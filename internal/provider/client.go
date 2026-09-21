package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	Endpoint string
	AdminKey string

	httpClient *http.Client
}

func NewClient(endpoint, adminKey string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{
		Endpoint:   endpoint,
		AdminKey:   adminKey,
		httpClient: httpClient,
	}
}

type APIError struct {
	StatusCode int
	Code       string
	Message    string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("datagecko API error %d (%s): %s", e.StatusCode, e.Code, e.Message)
}

func (e *APIError) IsNotFound() bool      { return e.StatusCode == http.StatusNotFound }
func (e *APIError) IsAlreadyExists() bool { return e.StatusCode == http.StatusConflict }

type errorBody struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func (c *Client) do(ctx context.Context, method, path string, body, out interface{}) error {
	return c.doOnce(ctx, method, path, body, out, false)
}

func (c *Client) doOnce(ctx context.Context, method, path string, body, out interface{}, retried bool) error {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encoding request body: %w", err)
		}
		reader = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.Endpoint, "/")+path, reader)
	if err != nil {
		return fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.AdminKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("calling datagecko: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests && !retried {
		wait, _ := strconv.Atoi(resp.Header.Get("Retry-After"))
		if wait < 0 {
			wait = 0
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(wait) * time.Second):
		}
		return c.doOnce(ctx, method, path, body, out, true)
	}

	if resp.StatusCode >= 400 {
		var decoded errorBody
		raw, _ := io.ReadAll(resp.Body)
		_ = json.Unmarshal(raw, &decoded)
		return &APIError{StatusCode: resp.StatusCode, Code: decoded.Error.Code, Message: decoded.Error.Message}
	}

	if out == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

type SeriesInfo struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Unit        string  `json:"unit"`
	LastPointAt *string `json:"last_point_at"`
}

type Stream struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	Description string       `json:"description"`
	FirstSeen   string       `json:"first_seen"`
	Series      []SeriesInfo `json:"series"`
}

func (c *Client) ListStreams(ctx context.Context) ([]Stream, error) {
	var streams []Stream
	if err := c.do(ctx, http.MethodGet, "/streams", nil, &streams); err != nil {
		return nil, err
	}
	return streams, nil
}

func (c *Client) FindStream(ctx context.Context, id string) (*Stream, error) {
	streams, err := c.ListStreams(ctx)
	if err != nil {
		return nil, err
	}
	for i := range streams {
		if streams[i].ID == id {
			return &streams[i], nil
		}
	}
	return nil, nil
}

func (c *Client) CreateStream(ctx context.Context, name, description string) (*Stream, error) {
	var stream Stream
	body := map[string]string{"name": name}
	if description != "" {
		body["description"] = description
	}
	if err := c.do(ctx, http.MethodPost, "/streams", body, &stream); err != nil {
		return nil, err
	}
	return &stream, nil
}

func (c *Client) UpdateStream(ctx context.Context, id string, name, description *string) (*Stream, error) {
	var stream Stream
	body := map[string]string{}
	if name != nil {
		body["name"] = *name
	}
	if description != nil {
		body["description"] = *description
	}
	if err := c.do(ctx, http.MethodPatch, "/streams/"+id, body, &stream); err != nil {
		return nil, err
	}
	return &stream, nil
}

func (c *Client) DeleteStream(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/streams/"+id, nil, nil)
}

func (c *Client) CreateSeries(ctx context.Context, streamID, name, unit string) (*SeriesInfo, error) {
	var series SeriesInfo
	body := map[string]string{"name": name}
	if unit != "" {
		body["unit"] = unit
	}
	if err := c.do(ctx, http.MethodPost, "/streams/"+streamID+"/series", body, &series); err != nil {
		return nil, err
	}
	return &series, nil
}

func (c *Client) UpdateSeries(ctx context.Context, streamID, seriesID string, name, unit *string) (*SeriesInfo, error) {
	var series SeriesInfo
	body := map[string]string{}
	if name != nil {
		body["name"] = *name
	}
	if unit != nil {
		body["unit"] = *unit
	}
	if err := c.do(ctx, http.MethodPatch, "/streams/"+streamID+"/series/"+seriesID, body, &series); err != nil {
		return nil, err
	}
	return &series, nil
}

func (c *Client) DeleteSeries(ctx context.Context, streamID, seriesID string) error {
	return c.do(ctx, http.MethodDelete, "/streams/"+streamID+"/series/"+seriesID, nil, nil)
}

type Key struct {
	Prefix     string  `json:"prefix"`
	Name       string  `json:"name"`
	Role       string  `json:"role"`
	Created    string  `json:"created"`
	Revoked    bool    `json:"revoked"`
	LastUsedAt *string `json:"last_used_at"`
}

type CreatedKey struct {
	Key
	Secret string `json:"secret"`
}

func (c *Client) ListKeys(ctx context.Context) ([]Key, error) {
	var keys []Key
	if err := c.do(ctx, http.MethodGet, "/keys", nil, &keys); err != nil {
		return nil, err
	}
	return keys, nil
}

func (c *Client) FindKey(ctx context.Context, prefix string) (*Key, error) {
	keys, err := c.ListKeys(ctx)
	if err != nil {
		return nil, err
	}
	for i := range keys {
		if keys[i].Prefix == prefix {
			return &keys[i], nil
		}
	}
	return nil, nil
}

func (c *Client) CreateIngestKey(ctx context.Context, name string) (*CreatedKey, error) {
	var key CreatedKey
	body := map[string]string{"name": name}
	if err := c.do(ctx, http.MethodPost, "/keys", body, &key); err != nil {
		return nil, err
	}
	return &key, nil
}

func (c *Client) RenameKey(ctx context.Context, prefix, name string) (*Key, error) {
	var key Key
	body := map[string]string{"name": name}
	if err := c.do(ctx, http.MethodPatch, "/keys/"+prefix, body, &key); err != nil {
		return nil, err
	}
	return &key, nil
}

func (c *Client) DeleteKey(ctx context.Context, prefix string) error {
	return c.do(ctx, http.MethodDelete, "/keys/"+prefix, nil, nil)
}

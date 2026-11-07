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

// maxRateLimitRetries is how many 429 windows a call waits out before it
// gives up: an admin key has 120 requests a minute, and a fully declared
// organization refreshes with more than that.
const maxRateLimitRetries = 3

func (c *Client) do(ctx context.Context, method, path string, body, out interface{}) error {
	var encoded []byte
	if body != nil {
		var err error
		encoded, err = json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encoding request body: %w", err)
		}
	}

	for attempt := 0; ; attempt++ {
		resp, err := c.send(ctx, method, path, encoded)
		if err != nil {
			return err
		}
		if resp.StatusCode == http.StatusTooManyRequests && attempt < maxRateLimitRetries {
			wait, _ := strconv.Atoi(resp.Header.Get("Retry-After"))
			resp.Body.Close()
			if wait < 0 {
				wait = 0
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(wait) * time.Second):
			}
			continue
		}
		defer resp.Body.Close()
		return decodeResponse(resp, out)
	}
}

func (c *Client) send(ctx context.Context, method, path string, encoded []byte) (*http.Response, error) {
	var reader io.Reader
	if encoded != nil {
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.Endpoint, "/")+path, reader)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.AdminKey)
	if encoded != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("calling datagecko: %w", err)
	}
	return resp, nil
}

func decodeResponse(resp *http.Response, out interface{}) error {
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
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	Unit         string  `json:"unit"`
	Kind         string  `json:"kind"`
	IntervalDays *int64  `json:"interval_days"`
	LastPointAt  *string `json:"last_point_at"`
}

type Stream struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Tags        map[string]string `json:"tags"`
	FirstSeen   string            `json:"first_seen"`
	Series      []SeriesInfo      `json:"series"`
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

func (c *Client) CreateStream(ctx context.Context, name, description string, tags map[string]string) (*Stream, error) {
	var stream Stream
	body := map[string]interface{}{"name": name}
	if description != "" {
		body["description"] = description
	}
	if tags != nil {
		body["tags"] = tags
	}
	if err := c.do(ctx, http.MethodPost, "/streams", body, &stream); err != nil {
		return nil, err
	}
	return &stream, nil
}

func (c *Client) UpdateStream(ctx context.Context, id string, name, description *string, tags map[string]string) (*Stream, error) {
	var stream Stream
	body := map[string]interface{}{}
	if name != nil {
		body["name"] = *name
	}
	if description != nil {
		body["description"] = *description
	}
	if tags != nil {
		body["tags"] = tags
	}
	if err := c.do(ctx, http.MethodPatch, "/streams/"+id, body, &stream); err != nil {
		return nil, err
	}
	return &stream, nil
}

func (c *Client) DeleteStream(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/streams/"+id, nil, nil)
}

func (c *Client) CreateSeries(ctx context.Context, streamID, name, unit, kind string, intervalDays *int64) (*SeriesInfo, error) {
	var series SeriesInfo
	body := map[string]interface{}{"name": name}
	if unit != "" {
		body["unit"] = unit
	}
	if kind != "" {
		body["kind"] = kind
	}
	if intervalDays != nil {
		body["interval_days"] = *intervalDays
	}
	if err := c.do(ctx, http.MethodPost, "/streams/"+streamID+"/series", body, &series); err != nil {
		return nil, err
	}
	return &series, nil
}

// UpdateSeries always sends interval_days: a nil intervalDays is JSON null,
// which clears the cadence. A series' kind never changes.
func (c *Client) UpdateSeries(ctx context.Context, streamID, seriesID string, name, unit *string, intervalDays *int64) (*SeriesInfo, error) {
	var series SeriesInfo
	body := map[string]interface{}{"interval_days": intervalDays}
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

type Metric struct {
	ID        string          `json:"id"`
	Type      string          `json:"type"`
	Name      string          `json:"name"`
	Params    json.RawMessage `json:"params"`
	Effective json.RawMessage `json:"effective"`
}

// metricBody carries params as raw bytes, embedded verbatim: never
// re-marshalled through float64, since the API's params are exact-typed.
type metricBody struct {
	Type   string          `json:"type,omitempty"`
	Name   string          `json:"name"`
	Params json.RawMessage `json:"params,omitempty"`
}

func (c *Client) CreateMetric(ctx context.Context, typ, name string, params json.RawMessage) (*Metric, error) {
	var metric Metric
	if err := c.do(ctx, http.MethodPost, "/metrics", metricBody{Type: typ, Name: name, Params: params}, &metric); err != nil {
		return nil, err
	}
	return &metric, nil
}

func (c *Client) GetMetric(ctx context.Context, id string) (*Metric, error) {
	var metric Metric
	if err := c.do(ctx, http.MethodGet, "/metrics/"+id, nil, &metric); err != nil {
		return nil, err
	}
	return &metric, nil
}

// UpdateMetric renames and replaces the params whole; a type never changes.
func (c *Client) UpdateMetric(ctx context.Context, id, name string, params json.RawMessage) (*Metric, error) {
	var metric Metric
	if err := c.do(ctx, http.MethodPatch, "/metrics/"+id, metricBody{Name: name, Params: params}, &metric); err != nil {
		return nil, err
	}
	return &metric, nil
}

func (c *Client) DeleteMetric(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/metrics/"+id, nil, nil)
}

type StreamMetric struct {
	Metric    string          `json:"metric"`
	Stream    string          `json:"stream"`
	Params    json.RawMessage `json:"params"`
	Effective json.RawMessage `json:"effective"`
}

func streamMetricPath(streamID, metricID string) string {
	return "/streams/" + streamID + "/metrics/" + metricID
}

// CreateStreamMetric connects a metric to a stream; a row already there is a 409.
func (c *Client) CreateStreamMetric(ctx context.Context, streamID, metricID string, params json.RawMessage) (*StreamMetric, error) {
	var row StreamMetric
	body := struct {
		MetricID string          `json:"metric_id"`
		Params   json.RawMessage `json:"params,omitempty"`
	}{metricID, params}
	if err := c.do(ctx, http.MethodPost, "/streams/"+streamID+"/metrics", body, &row); err != nil {
		return nil, err
	}
	return &row, nil
}

func (c *Client) GetStreamMetric(ctx context.Context, streamID, metricID string) (*StreamMetric, error) {
	var row StreamMetric
	if err := c.do(ctx, http.MethodGet, streamMetricPath(streamID, metricID), nil, &row); err != nil {
		return nil, err
	}
	return &row, nil
}

// ReplaceStreamMetric replaces a row's params whole; a missing row is a 404, never created.
func (c *Client) ReplaceStreamMetric(ctx context.Context, streamID, metricID string, params json.RawMessage) (*StreamMetric, error) {
	var row StreamMetric
	if params == nil {
		params = json.RawMessage(`{}`)
	}
	body := struct {
		Params json.RawMessage `json:"params"`
	}{params}
	if err := c.do(ctx, http.MethodPut, streamMetricPath(streamID, metricID), body, &row); err != nil {
		return nil, err
	}
	return &row, nil
}

func (c *Client) DeleteStreamMetric(ctx context.Context, streamID, metricID string) error {
	return c.do(ctx, http.MethodDelete, streamMetricPath(streamID, metricID), nil, nil)
}

type MetricType struct {
	Key          string          `json:"key"`
	Presentation string          `json:"presentation"`
	Defaults     json.RawMessage `json:"defaults"`
	Required     []string        `json:"required"`
	PerStream    []string        `json:"per_stream"`
}

func (c *Client) ListMetricTypes(ctx context.Context) ([]MetricType, error) {
	var types []MetricType
	if err := c.do(ctx, http.MethodGet, "/metric-types", nil, &types); err != nil {
		return nil, err
	}
	return types, nil
}

// ChartEntry is one line on a chart: a series id and only the style fields
// that were set — an unset field is absent on the wire and nil here.
type ChartEntry struct {
	ID          string   `json:"id"`
	Color       *string  `json:"color,omitempty"`
	Width       *float64 `json:"width,omitempty"`
	Dash        *string  `json:"dash,omitempty"`
	Fill        *bool    `json:"fill,omitempty"`
	PointRadius *float64 `json:"point_radius,omitempty"`
	PointStyle  *string  `json:"point_style,omitempty"`
}

type Chart struct {
	Title   string       `json:"title"`
	Series  []ChartEntry `json:"series"`
	Markers []ChartEntry `json:"markers"`
}

type chartsBody struct {
	Charts []Chart `json:"charts"`
}

func (c *Client) GetCharts(ctx context.Context, streamID string) ([]Chart, error) {
	var out chartsBody
	if err := c.do(ctx, http.MethodGet, "/streams/"+streamID+"/charts", nil, &out); err != nil {
		return nil, err
	}
	return out.Charts, nil
}

// ReplaceCharts replaces the stream's declared charts whole; an empty list
// returns the stream to its implicit chart. Lists are sent as [], never null.
func (c *Client) ReplaceCharts(ctx context.Context, streamID string, charts []Chart) ([]Chart, error) {
	sent := make([]Chart, len(charts))
	for i, chart := range charts {
		sent[i] = chart
		if sent[i].Series == nil {
			sent[i].Series = []ChartEntry{}
		}
		if sent[i].Markers == nil {
			sent[i].Markers = []ChartEntry{}
		}
	}
	var out chartsBody
	if err := c.do(ctx, http.MethodPut, "/streams/"+streamID+"/charts", chartsBody{Charts: sent}, &out); err != nil {
		return nil, err
	}
	return out.Charts, nil
}

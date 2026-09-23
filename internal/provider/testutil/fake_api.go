// Package testutil provides FakeAPI, an in-memory double of exactly the
// /api/v1/ routes terraform-provider-datagecko calls. No test in this
// module ever talks to a real datagecko instance.
package testutil

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
)

type fakeSeries struct {
	ID, Name, Unit string
}

type fakeStream struct {
	ID, Name, Description, FirstSeen string
	Tags                             map[string]string
	Series                           []*fakeSeries
}

// tagsEqual reports whether two tag sets carry the same pairs, regardless of
// how each map was built — the same equality the real API's tag_digest gives.
func tagsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if bv, ok := b[k]; !ok || bv != v {
			return false
		}
	}
	return true
}

type fakeKey struct {
	Prefix, Name, Role, Secret string
	Revoked                    bool
}

// FakeAPI is a minimal in-memory double of one organization's /api/v1/.
type FakeAPI struct {
	Server *httptest.Server

	mu               sync.Mutex
	streams          map[string]*fakeStream
	keys             map[string]*fakeKey
	nextID           int
	rateLimit        int
	requiredAdminKey string
}

func NewFakeAPI() *FakeAPI {
	f := &FakeAPI{streams: map[string]*fakeStream{}, keys: map[string]*fakeKey{}}
	f.Server = httptest.NewServer(http.HandlerFunc(f.route))
	return f
}

func (f *FakeAPI) Close() { f.Server.Close() }

// RateLimitNextRequests makes the next n requests answer 429 (Retry-After: 0)
// before the API resumes normal behavior — for testing the client's retry.
func (f *FakeAPI) RateLimitNextRequests(n int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rateLimit = n
}

// DeleteAllStreams removes every stream directly, bypassing DELETE —
// simulates a stream (and its series) being removed outside Terraform.
func (f *FakeAPI) DeleteAllStreams() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.streams = map[string]*fakeStream{}
}

// ClearAllSeries empties every stream's series list, keeping the streams —
// simulates a series being removed outside Terraform without touching its stream.
func (f *FakeAPI) ClearAllSeries() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, s := range f.streams {
		s.Series = nil
	}
}

// RequireAdminKey makes the fake reject any request whose Authorization
// header isn't exactly "Bearer <key>" — for testing that the client
// authenticates. Unset (the default) accepts any credential, which is what
// every other test in this module relies on.
func (f *FakeAPI) RequireAdminKey(key string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requiredAdminKey = key
}

// Revoke marks a key revoked directly, bypassing DELETE — simulates it
// being revoked outside Terraform.
func (f *FakeAPI) Revoke(prefix string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if k, ok := f.keys[prefix]; ok {
		k.Revoked = true
	}
}

func (f *FakeAPI) newID(prefix string) string {
	f.nextID++
	return fmt.Sprintf("%s-%012d", prefix, f.nextID)
}

func writeJSON(w http.ResponseWriter, status int, body interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]interface{}{
		"error": map[string]string{"code": code, "message": message},
	})
}

func decodeBody(r *http.Request, out interface{}) error {
	return json.NewDecoder(r.Body).Decode(out)
}

func (f *FakeAPI) route(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	if f.rateLimit > 0 {
		f.rateLimit--
		f.mu.Unlock()
		w.Header().Set("Retry-After", "0")
		writeError(w, http.StatusTooManyRequests, "rate_limited", "slow down")
		return
	}
	requiredAdminKey := f.requiredAdminKey
	f.mu.Unlock()

	if requiredAdminKey != "" && r.Header.Get("Authorization") != "Bearer "+requiredAdminKey {
		writeError(w, http.StatusUnauthorized, "invalid_key", "invalid API key")
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/")
	segments := strings.Split(path, "/")

	f.mu.Lock()
	defer f.mu.Unlock()

	switch {
	case r.Method == http.MethodGet && path == "streams":
		f.listStreams(w)
	case r.Method == http.MethodPost && path == "streams":
		f.createStream(w, r)
	case r.Method == http.MethodPatch && len(segments) == 2 && segments[0] == "streams":
		f.updateStream(w, r, segments[1])
	case r.Method == http.MethodDelete && len(segments) == 2 && segments[0] == "streams":
		f.deleteStream(w, segments[1])
	case r.Method == http.MethodPost && len(segments) == 3 && segments[0] == "streams" && segments[2] == "series":
		f.createSeries(w, r, segments[1])
	case r.Method == http.MethodPatch && len(segments) == 4 && segments[0] == "streams" && segments[2] == "series":
		f.updateSeries(w, r, segments[1], segments[3])
	case r.Method == http.MethodDelete && len(segments) == 4 && segments[0] == "streams" && segments[2] == "series":
		f.deleteSeries(w, segments[1], segments[3])
	case r.Method == http.MethodGet && path == "keys":
		f.listKeys(w)
	case r.Method == http.MethodPost && path == "keys":
		f.createKey(w, r)
	case r.Method == http.MethodPatch && len(segments) == 2 && segments[0] == "keys":
		f.renameKey(w, r, segments[1])
	case r.Method == http.MethodDelete && len(segments) == 2 && segments[0] == "keys":
		f.deleteKey(w, segments[1])
	default:
		writeError(w, http.StatusNotFound, "not_found", "no such route: "+r.Method+" "+path)
	}
}

func (f *FakeAPI) streamJSON(s *fakeStream) map[string]interface{} {
	series := make([]map[string]interface{}, len(s.Series))
	for i, sr := range s.Series {
		series[i] = map[string]interface{}{"id": sr.ID, "name": sr.Name, "unit": sr.Unit, "last_point_at": nil}
	}
	tags := s.Tags
	if tags == nil {
		tags = map[string]string{}
	}
	return map[string]interface{}{
		"id": s.ID, "name": s.Name, "description": s.Description, "tags": tags,
		"first_seen": s.FirstSeen, "series": series,
	}
}

func (f *FakeAPI) listStreams(w http.ResponseWriter) {
	out := make([]map[string]interface{}, 0, len(f.streams))
	for _, s := range f.streams {
		out = append(out, f.streamJSON(s))
	}
	writeJSON(w, http.StatusOK, out)
}

func (f *FakeAPI) createStream(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name        string
		Description string
		Tags        map[string]string
	}
	if err := decodeBody(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "payload_refused", err.Error())
		return
	}
	for _, s := range f.streams {
		if s.Name == body.Name && tagsEqual(s.Tags, body.Tags) {
			writeError(w, http.StatusConflict, "already_exists", "stream already exists")
			return
		}
	}
	s := &fakeStream{
		ID: f.newID("stream"), Name: body.Name, Description: body.Description,
		Tags: body.Tags, FirstSeen: "2026-09-21T00:00:00Z",
	}
	f.streams[s.ID] = s
	writeJSON(w, http.StatusCreated, f.streamJSON(s))
}

func (f *FakeAPI) updateStream(w http.ResponseWriter, r *http.Request, id string) {
	s, ok := f.streams[id]
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "no such stream")
		return
	}
	var body map[string]interface{}
	if err := decodeBody(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "payload_refused", err.Error())
		return
	}
	newName := s.Name
	if name, ok := body["name"].(string); ok {
		newName = name
	}
	newTags := s.Tags
	if rawTags, ok := body["tags"]; ok {
		newTags = map[string]string{}
		if m, ok := rawTags.(map[string]interface{}); ok {
			for k, v := range m {
				if sv, ok := v.(string); ok {
					newTags[k] = sv
				}
			}
		}
	}
	for otherID, other := range f.streams {
		if otherID != id && other.Name == newName && tagsEqual(other.Tags, newTags) {
			writeError(w, http.StatusConflict, "already_exists", "stream already exists")
			return
		}
	}
	s.Name = newName
	s.Tags = newTags
	if description, ok := body["description"].(string); ok {
		s.Description = description
	}
	writeJSON(w, http.StatusOK, f.streamJSON(s))
}

func (f *FakeAPI) deleteStream(w http.ResponseWriter, id string) {
	if _, ok := f.streams[id]; !ok {
		writeError(w, http.StatusNotFound, "not_found", "no such stream")
		return
	}
	delete(f.streams, id)
	w.WriteHeader(http.StatusNoContent)
}

func (f *FakeAPI) createSeries(w http.ResponseWriter, r *http.Request, streamID string) {
	s, ok := f.streams[streamID]
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "no such stream")
		return
	}
	var body struct{ Name, Unit string }
	if err := decodeBody(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "payload_refused", err.Error())
		return
	}
	for _, sr := range s.Series {
		if sr.Name == body.Name {
			writeError(w, http.StatusConflict, "already_exists", "series already exists")
			return
		}
	}
	series := &fakeSeries{ID: f.newID("series"), Name: body.Name, Unit: body.Unit}
	s.Series = append(s.Series, series)
	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"id": series.ID, "name": series.Name, "unit": series.Unit, "last_point_at": nil,
	})
}

func (f *FakeAPI) findSeries(streamID, seriesID string) (*fakeStream, *fakeSeries, bool) {
	s, ok := f.streams[streamID]
	if !ok {
		return nil, nil, false
	}
	for _, sr := range s.Series {
		if sr.ID == seriesID {
			return s, sr, true
		}
	}
	return nil, nil, false
}

func (f *FakeAPI) updateSeries(w http.ResponseWriter, r *http.Request, streamID, seriesID string) {
	s, series, ok := f.findSeries(streamID, seriesID)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "no such series")
		return
	}
	var body map[string]string
	if err := decodeBody(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "payload_refused", err.Error())
		return
	}
	if name, ok := body["name"]; ok {
		for _, other := range s.Series {
			if other.ID != seriesID && other.Name == name {
				writeError(w, http.StatusConflict, "already_exists", "series already exists")
				return
			}
		}
		series.Name = name
	}
	if unit, ok := body["unit"]; ok {
		series.Unit = unit
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"id": series.ID, "name": series.Name, "unit": series.Unit, "last_point_at": nil,
	})
}

func (f *FakeAPI) deleteSeries(w http.ResponseWriter, streamID, seriesID string) {
	s, _, ok := f.findSeries(streamID, seriesID)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "no such series")
		return
	}
	kept := s.Series[:0]
	for _, sr := range s.Series {
		if sr.ID != seriesID {
			kept = append(kept, sr)
		}
	}
	s.Series = kept
	w.WriteHeader(http.StatusNoContent)
}

func (f *FakeAPI) keyJSON(k *fakeKey) map[string]interface{} {
	return map[string]interface{}{
		"prefix": k.Prefix, "name": k.Name, "role": k.Role,
		"created": "2026-09-21T00:00:00Z", "revoked": k.Revoked, "last_used_at": nil,
	}
}

func (f *FakeAPI) listKeys(w http.ResponseWriter) {
	out := make([]map[string]interface{}, 0, len(f.keys))
	for _, k := range f.keys {
		if !k.Revoked {
			out = append(out, f.keyJSON(k))
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (f *FakeAPI) createKey(w http.ResponseWriter, r *http.Request) {
	var body struct{ Name string }
	if err := decodeBody(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "payload_refused", err.Error())
		return
	}
	prefix := f.newID("key")
	k := &fakeKey{Prefix: prefix, Name: body.Name, Role: "ingest", Secret: prefix + ".secret"}
	f.keys[prefix] = k
	out := f.keyJSON(k)
	out["secret"] = k.Secret
	writeJSON(w, http.StatusCreated, out)
}

func (f *FakeAPI) renameKey(w http.ResponseWriter, r *http.Request, prefix string) {
	k, ok := f.keys[prefix]
	if !ok || k.Revoked {
		writeError(w, http.StatusNotFound, "not_found", "no such key")
		return
	}
	var body struct{ Name string }
	if err := decodeBody(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "payload_refused", err.Error())
		return
	}
	k.Name = body.Name
	writeJSON(w, http.StatusOK, f.keyJSON(k))
}

func (f *FakeAPI) deleteKey(w http.ResponseWriter, prefix string) {
	k, ok := f.keys[prefix]
	if !ok || k.Revoked {
		writeError(w, http.StatusNotFound, "not_found", "no such key")
		return
	}
	k.Revoked = true
	w.WriteHeader(http.StatusNoContent)
}

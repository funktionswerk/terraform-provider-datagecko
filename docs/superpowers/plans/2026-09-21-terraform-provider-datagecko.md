# terraform-provider-datagecko Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A Terraform provider that manages datagecko streams, series, and ingest keys — `datagecko_stream`, `datagecko_series`, `datagecko_ingest_key` — against one organization's `/api/v1/`, authenticated by that organization's admin key.

**Architecture:** A Go module, `terraform-plugin-framework`-based. One HTTP client (`internal/provider/client.go`) is the only thing that talks to datagecko; three resource files each own one resource's schema and CRUD; a fake in-process HTTP double (`internal/provider/testutil/fake_api.go`) stands in for datagecko in every test — no test in this repo ever needs a running datagecko instance.

**Tech Stack:** Go, `terraform-plugin-framework`, `terraform-plugin-testing`, standard library `net/http`/`net/http/httptest`.

**Spec:** `docs/superpowers/specs/2026-09-21-terraform-provider-datagecko-design.md` (depends on `datagecko.ai`'s `docs/superpowers/specs/2026-09-21-stream-and-ingest-key-rename-design.md` — implement that plan first, or `name` on `datagecko_stream`/`datagecko_ingest_key` will 404/⁠500 against a datagecko that doesn't yet accept a `PATCH` rename).

## Global Constraints

- Go 1.22 or newer; `gofmt -l .` and `go vet ./...` clean before every commit.
- Terraform CLI must be on `PATH` — `terraform-plugin-testing`'s `resource.UnitTest` drives real plan/apply/destroy cycles against this provider's binary; only datagecko itself is faked.
- Every HTTP call goes through `internal/provider/client.go` — no resource file constructs a request or reads a response body directly.
- No `Co-Authored-By` or other attribution trailers on commits in this repo (`CLAUDE.md`).
- One provider instance manages one organization; `admin_key` resolves from the provider block, falling back to `DATAGECKO_ADMIN_KEY`.
- No test in this repo talks to a real datagecko instance — every test uses `internal/provider/testutil.FakeAPI`.

---

### Task 1: Module scaffold and provider skeleton

**Files:**
- Create: `go.mod`
- Create: `main.go`
- Create: `internal/provider/provider.go`
- Create: `internal/provider/provider_test.go`

**Interfaces:**
- Consumes: nothing (first task).
- Produces: `provider.New(version string) func() provider.Provider` — Task 2 onward's tests call this to build a provider instance; `*provider.Client` type (declared here as a forward reference the resources will use) is actually defined in Task 2 — this task's `Configure` stores whatever `req.ProviderData` receives.

- [ ] **Step 1: Initialize the Go module**

```bash
go mod init github.com/funktionswerk/terraform-provider-datagecko
go get github.com/hashicorp/terraform-plugin-framework@latest
go get github.com/hashicorp/terraform-plugin-go@latest
go get github.com/hashicorp/terraform-plugin-testing@latest
```

- [ ] **Step 2: Write `main.go`**

```go
package main

import (
	"context"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"

	"github.com/funktionswerk/terraform-provider-datagecko/internal/provider"
)

// version is overridden at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	err := providerserver.Serve(context.Background(), provider.New(version), providerserver.ServeOpts{
		Address: "registry.terraform.io/funktionswerk/datagecko",
	})
	if err != nil {
		log.Fatal(err.Error())
	}
}
```

- [ ] **Step 3: Write `internal/provider/provider.go`**

```go
package provider

import (
	"context"
	"os"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const defaultEndpoint = "https://datagecko.ai/api/v1/"

var (
	_ provider.Provider = &DatageckoProvider{}
)

type DatageckoProvider struct {
	version string
}

type providerModel struct {
	Endpoint types.String `tfsdk:"endpoint"`
	AdminKey types.String `tfsdk:"admin_key"`
}

func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &DatageckoProvider{version: version}
	}
}

func (p *DatageckoProvider) Metadata(ctx context.Context, req provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "datagecko"
	resp.Version = p.version
}

func (p *DatageckoProvider) Schema(ctx context.Context, req provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages datagecko streams, series, and ingest keys for one organization.",
		Attributes: map[string]schema.Attribute{
			"endpoint": schema.StringAttribute{
				Optional:    true,
				Description: "Base URL of the datagecko API. Defaults to https://datagecko.ai/api/v1/.",
			},
			"admin_key": schema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "The organization's admin key. Falls back to the DATAGECKO_ADMIN_KEY environment variable.",
			},
		},
	}
}

func (p *DatageckoProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var data providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	endpoint := defaultEndpoint
	if !data.Endpoint.IsNull() && data.Endpoint.ValueString() != "" {
		endpoint = data.Endpoint.ValueString()
	}

	adminKey := os.Getenv("DATAGECKO_ADMIN_KEY")
	if !data.AdminKey.IsNull() && data.AdminKey.ValueString() != "" {
		adminKey = data.AdminKey.ValueString()
	}
	if adminKey == "" {
		resp.Diagnostics.AddError(
			"Missing admin key",
			"Set the admin_key provider attribute or the DATAGECKO_ADMIN_KEY environment variable.",
		)
		return
	}

	client := NewClient(endpoint, adminKey, nil)
	resp.DataSourceData = client
	resp.ResourceData = client
}

func (p *DatageckoProvider) Resources(ctx context.Context) []func() resource.Resource {
	return nil
}

func (p *DatageckoProvider) DataSources(ctx context.Context) []func() datasource.DataSource {
	return nil
}
```

`NewClient` doesn't exist yet — that's Task 2. This file will not compile on its own until Task 2 lands; that's expected for Step 3. Do not run `go build` until Step 4 confirms the stub.

- [ ] **Step 4: Add a temporary client stub so this task builds and tests on its own**

Create `internal/provider/client_stub.go` — deleted in Task 2 when the real `client.go` replaces it:

```go
package provider

import "net/http"

// Client is replaced by the real implementation in Task 2. This stub exists
// only so Task 1 compiles and its own test can run in isolation.
type Client struct {
	Endpoint string
	AdminKey string
}

func NewClient(endpoint, adminKey string, httpClient *http.Client) *Client {
	return &Client{Endpoint: endpoint, AdminKey: adminKey}
}
```

- [ ] **Step 5: Write `internal/provider/provider_test.go`**

```go
package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func configureWith(t *testing.T, endpoint, adminKey string, env map[string]string) *provider.ConfigureResponse {
	t.Helper()
	for k, v := range env {
		t.Setenv(k, v)
	}

	p := New("test")()
	schemaResp := &provider.SchemaResponse{}
	p.Schema(context.Background(), provider.SchemaRequest{}, schemaResp)

	values := map[string]tftypes.Value{
		"endpoint":  tftypes.NewValue(tftypes.String, nilIfEmpty(endpoint)),
		"admin_key": tftypes.NewValue(tftypes.String, nilIfEmpty(adminKey)),
	}
	objType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{
		"endpoint": tftypes.String, "admin_key": tftypes.String,
	}}
	config := tfsdk.Config{
		Schema: schemaResp.Schema,
		Raw:    tftypes.NewValue(objType, values),
	}

	resp := &provider.ConfigureResponse{}
	p.Configure(context.Background(), provider.ConfigureRequest{Config: config}, resp)
	return resp
}

func nilIfEmpty(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

func TestConfigureUsesTheAdminKeyAttributeOverTheEnvVar(t *testing.T) {
	resp := configureWith(t, "http://example.test/api/v1/", "attr-key", map[string]string{"DATAGECKO_ADMIN_KEY": "env-key"})

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}
	client := resp.ResourceData.(*Client)
	if client.AdminKey != "attr-key" {
		t.Fatalf("expected the attribute's key to win, got %q", client.AdminKey)
	}
}

func TestConfigureFallsBackToTheEnvVar(t *testing.T) {
	resp := configureWith(t, "", "", map[string]string{"DATAGECKO_ADMIN_KEY": "env-key"})

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}
	client := resp.ResourceData.(*Client)
	if client.AdminKey != "env-key" {
		t.Fatalf("expected the env var's key, got %q", client.AdminKey)
	}
	if client.Endpoint != defaultEndpoint {
		t.Fatalf("expected the default endpoint, got %q", client.Endpoint)
	}
}

func TestConfigureFailsWithoutAnAdminKeyAnywhere(t *testing.T) {
	resp := configureWith(t, "", "", nil)

	if !resp.Diagnostics.HasError() {
		t.Fatal("expected a diagnostic error when no admin key is configured")
	}
}
```

- [ ] **Step 6: Run the tests and confirm they pass**

Run: `go test ./internal/provider/... -run TestConfigure -v`
Expected: all three PASS.

- [ ] **Step 7: Build and vet**

Run: `go build ./... && go vet ./... && gofmt -l .`
Expected: builds cleanly; `gofmt -l .` prints nothing.

- [ ] **Step 8: Commit**

```bash
git add go.mod go.sum main.go internal/provider/provider.go internal/provider/provider_test.go internal/provider/client_stub.go
git commit -m "feat: provider skeleton — schema, admin_key/endpoint configuration"
```

---

### Task 2: HTTP client and the fake datagecko double

**Files:**
- Create: `internal/provider/client.go` (replaces `client_stub.go`)
- Delete: `internal/provider/client_stub.go`
- Create: `internal/provider/client_test.go`
- Create: `internal/provider/testutil/fake_api.go`

**Interfaces:**
- Consumes: nothing beyond the standard library.
- Produces (used by every resource task from here on):
  - `NewClient(endpoint, adminKey string, httpClient *http.Client) *Client`
  - `type APIError struct{ StatusCode int; Code, Message string }`, `func (*APIError) Error() string`, `func (*APIError) IsNotFound() bool`, `func (*APIError) IsAlreadyExists() bool`
  - `type Stream struct{ ID, Name, Description, FirstSeen string; Series []SeriesInfo }`
  - `type SeriesInfo struct{ ID, Name, Unit string; LastPointAt *string }`
  - `type Key struct{ Prefix, Name, Role, Created string; Revoked bool; LastUsedAt *string }`, `type CreatedKey struct{ Key; Secret string }`
  - `(*Client) ListStreams(ctx) ([]Stream, error)`, `FindStream(ctx, id) (*Stream, error)`, `CreateStream(ctx, name, description) (*Stream, error)`, `UpdateStream(ctx, id string, name, description *string) (*Stream, error)`, `DeleteStream(ctx, id) error`
  - `(*Client) CreateSeries(ctx, streamID, name, unit string) (*SeriesInfo, error)`, `UpdateSeries(ctx, streamID, seriesID string, name, unit *string) (*SeriesInfo, error)`, `DeleteSeries(ctx, streamID, seriesID string) error`
  - `(*Client) ListKeys(ctx) ([]Key, error)`, `FindKey(ctx, prefix) (*Key, error)`, `CreateIngestKey(ctx, name string) (*CreatedKey, error)`, `RenameKey(ctx, prefix, name string) (*Key, error)`, `DeleteKey(ctx, prefix string) error`
  - `testutil.NewFakeAPI() *FakeAPI`, `(*FakeAPI) Server *httptest.Server` (field), `Close()`, `RateLimitNextRequests(n int)`, `DeleteAllStreams()`, `ClearAllSeries()`, `Revoke(prefix string)`

- [ ] **Step 1: Delete the Task 1 stub**

```bash
rm internal/provider/client_stub.go
```

- [ ] **Step 2: Write `internal/provider/testutil/fake_api.go`**

```go
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
	Series                           []*fakeSeries
}

type fakeKey struct {
	Prefix, Name, Role, Secret string
	Revoked                    bool
}

// FakeAPI is a minimal in-memory double of one organization's /api/v1/.
type FakeAPI struct {
	Server *httptest.Server

	mu        sync.Mutex
	streams   map[string]*fakeStream
	keys      map[string]*fakeKey
	nextID    int
	rateLimit int
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
	f.mu.Unlock()

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
	return map[string]interface{}{
		"id": s.ID, "name": s.Name, "description": s.Description,
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
	var body struct{ Name, Description string }
	if err := decodeBody(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "payload_refused", err.Error())
		return
	}
	for _, s := range f.streams {
		if s.Name == body.Name {
			writeError(w, http.StatusConflict, "already_exists", "stream already exists")
			return
		}
	}
	s := &fakeStream{ID: f.newID("stream"), Name: body.Name, Description: body.Description, FirstSeen: "2026-09-21T00:00:00Z"}
	f.streams[s.ID] = s
	writeJSON(w, http.StatusCreated, f.streamJSON(s))
}

func (f *FakeAPI) updateStream(w http.ResponseWriter, r *http.Request, id string) {
	s, ok := f.streams[id]
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "no such stream")
		return
	}
	var body map[string]string
	if err := decodeBody(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "payload_refused", err.Error())
		return
	}
	if name, ok := body["name"]; ok {
		for otherID, other := range f.streams {
			if otherID != id && other.Name == name {
				writeError(w, http.StatusConflict, "already_exists", "stream already exists")
				return
			}
		}
		s.Name = name
	}
	if description, ok := body["description"]; ok {
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
```

- [ ] **Step 3: Write `internal/provider/client.go`**

```go
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
		Endpoint:   strings.TrimRight(endpoint, "/"),
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

	req, err := http.NewRequestWithContext(ctx, method, c.Endpoint+path, reader)
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
		time.Sleep(time.Duration(wait) * time.Second)
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
```

- [ ] **Step 4: Write `internal/provider/client_test.go`**

```go
package provider

import (
	"context"
	"net/http"
	"testing"

	"github.com/funktionswerk/terraform-provider-datagecko/internal/provider/testutil"
)

func TestClientCreatesAndListsAStream(t *testing.T) {
	fake := testutil.NewFakeAPI()
	defer fake.Close()
	client := NewClient(fake.Server.URL, "test-admin-key", nil)
	ctx := context.Background()

	created, err := client.CreateStream(ctx, "revenue", "Daily revenue")
	if err != nil {
		t.Fatalf("CreateStream: %v", err)
	}

	streams, err := client.ListStreams(ctx)
	if err != nil {
		t.Fatalf("ListStreams: %v", err)
	}
	if len(streams) != 1 || streams[0].ID != created.ID {
		t.Fatalf("expected the created stream back, got %+v", streams)
	}
}

func TestCreateStreamAlreadyExists(t *testing.T) {
	fake := testutil.NewFakeAPI()
	defer fake.Close()
	client := NewClient(fake.Server.URL, "test", nil)
	ctx := context.Background()
	if _, err := client.CreateStream(ctx, "revenue", ""); err != nil {
		t.Fatalf("first create: %v", err)
	}

	_, err := client.CreateStream(ctx, "revenue", "")

	apiErr, ok := err.(*APIError)
	if !ok || !apiErr.IsAlreadyExists() {
		t.Fatalf("expected an already-exists APIError, got %v", err)
	}
}

func TestDeleteStreamNotFound(t *testing.T) {
	fake := testutil.NewFakeAPI()
	defer fake.Close()
	client := NewClient(fake.Server.URL, "test", nil)

	err := client.DeleteStream(context.Background(), "nonexistent")

	apiErr, ok := err.(*APIError)
	if !ok || !apiErr.IsNotFound() {
		t.Fatalf("expected a not-found APIError, got %v", err)
	}
}

func TestClientRetriesOnceOn429(t *testing.T) {
	fake := testutil.NewFakeAPI()
	defer fake.Close()
	fake.RateLimitNextRequests(1)
	client := NewClient(fake.Server.URL, "test", nil)

	_, err := client.CreateStream(context.Background(), "revenue", "")

	if err != nil {
		t.Fatalf("expected the retry to succeed, got %v", err)
	}
}

func TestClientGivesUpAfterOneRetry(t *testing.T) {
	fake := testutil.NewFakeAPI()
	defer fake.Close()
	fake.RateLimitNextRequests(2)
	client := NewClient(fake.Server.URL, "test", nil)

	_, err := client.CreateStream(context.Background(), "revenue", "")

	apiErr, ok := err.(*APIError)
	if !ok || apiErr.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected a 429 APIError after the one retry, got %v", err)
	}
}

func TestSeriesAndKeyLifecycleAgainstTheFake(t *testing.T) {
	fake := testutil.NewFakeAPI()
	defer fake.Close()
	client := NewClient(fake.Server.URL, "test", nil)
	ctx := context.Background()

	stream, _ := client.CreateStream(ctx, "revenue", "")
	series, err := client.CreateSeries(ctx, stream.ID, "delivery", "USD")
	if err != nil {
		t.Fatalf("CreateSeries: %v", err)
	}
	newName := "delivered"
	if _, err := client.UpdateSeries(ctx, stream.ID, series.ID, &newName, nil); err != nil {
		t.Fatalf("UpdateSeries: %v", err)
	}
	if err := client.DeleteSeries(ctx, stream.ID, series.ID); err != nil {
		t.Fatalf("DeleteSeries: %v", err)
	}

	key, err := client.CreateIngestKey(ctx, "feed")
	if err != nil {
		t.Fatalf("CreateIngestKey: %v", err)
	}
	if key.Secret == "" {
		t.Fatal("expected a secret on create")
	}
	if _, err := client.RenameKey(ctx, key.Prefix, "renamed-feed"); err != nil {
		t.Fatalf("RenameKey: %v", err)
	}
	if err := client.DeleteKey(ctx, key.Prefix); err != nil {
		t.Fatalf("DeleteKey: %v", err)
	}
	found, err := client.FindKey(ctx, key.Prefix)
	if err != nil {
		t.Fatalf("FindKey: %v", err)
	}
	if found != nil {
		t.Fatalf("expected a revoked key to be findable-none, got %+v", found)
	}
}
```

- [ ] **Step 5: Run the tests and confirm they pass**

Run: `go test ./internal/provider/... -v`
Expected: every test in `client_test.go` and `provider_test.go` PASSES. (`provider_test.go` from Task 1 must still pass — its `client_stub.go` type is now the real one, with the same field names, so it should need no changes.)

- [ ] **Step 6: Build and vet**

Run: `go build ./... && go vet ./... && gofmt -l .`
Expected: clean.

- [ ] **Step 7: Commit**

```bash
git add internal/provider/client.go internal/provider/client_test.go internal/provider/testutil/fake_api.go
git rm internal/provider/client_stub.go
git commit -m "feat: datagecko HTTP client and its in-memory test double"
```

---

### Task 3: `datagecko_stream` resource

**Files:**
- Create: `internal/provider/stream_resource.go`
- Create: `internal/provider/stream_resource_test.go`
- Modify: `internal/provider/provider.go` (register the resource)

**Interfaces:**
- Consumes: everything produced by Task 2's `Client`.
- Produces: `NewStreamResource() resource.Resource` — registered in `provider.go`'s `Resources()`.

- [ ] **Step 1: Write `internal/provider/stream_resource.go`**

```go
package provider

import (
	"context"
	"fmt"
	"regexp"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	streamNamePattern              = regexp.MustCompile(`^[a-z][a-z0-9_.]{0,127}$`)
	_                  resource.Resource                = &StreamResource{}
	_                  resource.ResourceWithConfigure    = &StreamResource{}
	_                  resource.ResourceWithImportState  = &StreamResource{}
)

type StreamResource struct {
	client *Client
}

func NewStreamResource() resource.Resource {
	return &StreamResource{}
}

type streamModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	FirstSeen   types.String `tfsdk:"first_seen"`
}

func (r *StreamResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_stream"
}

func (r *StreamResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A named grouping of related series.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "The stream's UUID.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Unique within the organization. Must match ^[a-z][a-z0-9_.]{0,127}$.",
				Validators: []validator.String{
					stringvalidator.RegexMatches(streamNamePattern, "must match ^[a-z][a-z0-9_.]{0,127}$"),
				},
			},
			"description": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString(""),
			},
			"first_seen": schema.StringAttribute{
				Computed:      true,
				Description:   "RFC 3339 timestamp of when this stream was created.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *StreamResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Resource Configure Type", fmt.Sprintf("Expected *Client, got: %T", req.ProviderData))
		return
	}
	r.client = client
}

func (r *StreamResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan streamModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	stream, err := r.client.CreateStream(ctx, plan.Name.ValueString(), plan.Description.ValueString())
	if apiErr, ok := err.(*APIError); ok && apiErr.IsAlreadyExists() {
		resp.Diagnostics.AddError(
			"Stream already exists",
			fmt.Sprintf("A stream named %q already exists in this organization. Import it instead: terraform import <this resource's address> <id>.", plan.Name.ValueString()),
		)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Error creating stream", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, toStreamModel(stream))...)
}

func (r *StreamResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state streamModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	stream, err := r.client.FindStream(ctx, state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error reading stream", err.Error())
		return
	}
	if stream == nil {
		resp.State.RemoveResource(ctx)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, toStreamModel(stream))...)
}

func (r *StreamResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan streamModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	name := plan.Name.ValueString()
	description := plan.Description.ValueString()
	stream, err := r.client.UpdateStream(ctx, plan.ID.ValueString(), &name, &description)
	if apiErr, ok := err.(*APIError); ok && apiErr.IsAlreadyExists() {
		resp.Diagnostics.AddError("Stream already exists", fmt.Sprintf("A stream named %q already exists in this organization.", name))
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Error updating stream", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, toStreamModel(stream))...)
}

func (r *StreamResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state streamModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteStream(ctx, state.ID.ValueString())
	if apiErr, ok := err.(*APIError); ok && apiErr.IsNotFound() {
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Error deleting stream", err.Error())
	}
}

func (r *StreamResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func toStreamModel(s *Stream) streamModel {
	return streamModel{
		ID:          types.StringValue(s.ID),
		Name:        types.StringValue(s.Name),
		Description: types.StringValue(s.Description),
		FirstSeen:   types.StringValue(s.FirstSeen),
	}
}
```

- [ ] **Step 2: Register it in `internal/provider/provider.go`**

Change:

```go
func (p *DatageckoProvider) Resources(ctx context.Context) []func() resource.Resource {
	return nil
}
```

to:

```go
func (p *DatageckoProvider) Resources(ctx context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewStreamResource,
	}
}
```

- [ ] **Step 3: Write `internal/provider/stream_resource_test.go`**

```go
package provider_test

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/funktionswerk/terraform-provider-datagecko/internal/provider"
	"github.com/funktionswerk/terraform-provider-datagecko/internal/provider/testutil"
)

func protoV6Factories() map[string]func() (tfprotov6.ProviderServer, error) {
	return map[string]func() (tfprotov6.ProviderServer, error){
		"datagecko": providerserver.NewProtocol6WithError(provider.New("test")()),
	}
}

func TestAccStreamResource_createUpdateImport(t *testing.T) {
	fake := testutil.NewFakeAPI()
	defer fake.Close()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories(),
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
provider "datagecko" {
  endpoint  = %q
  admin_key = "test"
}

resource "datagecko_stream" "revenue" {
  name        = "revenue"
  description = "Daily revenue"
}
`, fake.Server.URL),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("datagecko_stream.revenue", "name", "revenue"),
					resource.TestCheckResourceAttr("datagecko_stream.revenue", "description", "Daily revenue"),
					resource.TestCheckResourceAttrSet("datagecko_stream.revenue", "id"),
					resource.TestCheckResourceAttrSet("datagecko_stream.revenue", "first_seen"),
				),
			},
			{
				// A name-only change renames in place — no replace plan.
				Config: fmt.Sprintf(`
provider "datagecko" {
  endpoint  = %q
  admin_key = "test"
}

resource "datagecko_stream" "revenue" {
  name        = "sales"
  description = "Daily revenue"
}
`, fake.Server.URL),
				Check: resource.TestCheckResourceAttr("datagecko_stream.revenue", "name", "sales"),
			},
			{
				ResourceName:      "datagecko_stream.revenue",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccStreamResource_conflictingNameIsARealError(t *testing.T) {
	fake := testutil.NewFakeAPI()
	defer fake.Close()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories(),
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
provider "datagecko" {
  endpoint  = %q
  admin_key = "test"
}

resource "datagecko_stream" "a" {
  name = "revenue"
}

resource "datagecko_stream" "b" {
  name = "revenue"
}
`, fake.Server.URL),
				ExpectError: regexp.MustCompile("already exists"),
			},
		},
	})
}

func TestAccStreamResource_deletedOutOfBandIsRecreated(t *testing.T) {
	fake := testutil.NewFakeAPI()
	defer fake.Close()
	config := fmt.Sprintf(`
provider "datagecko" {
  endpoint  = %q
  admin_key = "test"
}

resource "datagecko_stream" "revenue" {
  name = "revenue"
}
`, fake.Server.URL)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories(),
		Steps: []resource.TestStep{
			{Config: config},
			{
				PreConfig:          func() { fake.DeleteAllStreams() },
				Config:             config,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}
```

- [ ] **Step 4: Run the tests and confirm they pass**

Run: `go test ./... -v -run TestAcc`
Expected: all three PASS. (Requires the Terraform CLI on `PATH` — see Global Constraints.)

- [ ] **Step 5: Build and vet**

Run: `go build ./... && go vet ./... && gofmt -l .`
Expected: clean.

- [ ] **Step 6: Commit**

```bash
git add internal/provider/stream_resource.go internal/provider/stream_resource_test.go internal/provider/provider.go
git commit -m "feat: datagecko_stream resource"
```

---

### Task 4: `datagecko_series` resource

**Files:**
- Create: `internal/provider/series_resource.go`
- Create: `internal/provider/series_resource_test.go`
- Modify: `internal/provider/provider.go` (register the resource)

**Interfaces:**
- Consumes: `Client.CreateSeries`/`UpdateSeries`/`DeleteSeries`/`FindStream` from Task 2.
- Produces: `NewSeriesResource() resource.Resource`.

- [ ] **Step 1: Write `internal/provider/series_resource.go`**

```go
package provider

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	seriesNamePattern             = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
	_                 resource.Resource               = &SeriesResource{}
	_                 resource.ResourceWithConfigure   = &SeriesResource{}
	_                 resource.ResourceWithImportState = &SeriesResource{}
)

type SeriesResource struct {
	client *Client
}

func NewSeriesResource() resource.Resource {
	return &SeriesResource{}
}

type seriesModel struct {
	ID          types.String `tfsdk:"id"`
	StreamID    types.String `tfsdk:"stream_id"`
	Name        types.String `tfsdk:"name"`
	Unit        types.String `tfsdk:"unit"`
	LastPointAt types.String `tfsdk:"last_point_at"`
}

func (r *SeriesResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_series"
}

func (r *SeriesResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "One line of points, belonging to a stream.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "The series' UUID.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"stream_id": schema.StringAttribute{
				Required:      true,
				Description:   "The stream this series belongs to. A series cannot move between streams.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Unique within the stream. Must match ^[a-z][a-z0-9_]{0,63}$.",
				Validators: []validator.String{
					stringvalidator.RegexMatches(seriesNamePattern, "must match ^[a-z][a-z0-9_]{0,63}$"),
				},
			},
			"unit": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString(""),
			},
			"last_point_at": schema.StringAttribute{
				Computed:      true,
				Description:   "RFC 3339, or empty if no point has ever landed.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *SeriesResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Resource Configure Type", fmt.Sprintf("Expected *Client, got: %T", req.ProviderData))
		return
	}
	r.client = client
}

func (r *SeriesResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan seriesModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	series, err := r.client.CreateSeries(ctx, plan.StreamID.ValueString(), plan.Name.ValueString(), plan.Unit.ValueString())
	if apiErr, ok := err.(*APIError); ok && apiErr.IsAlreadyExists() {
		resp.Diagnostics.AddError("Series already exists", fmt.Sprintf("A series named %q already exists on this stream. Import it instead.", plan.Name.ValueString()))
		return
	}
	if apiErr, ok := err.(*APIError); ok && apiErr.IsNotFound() {
		resp.Diagnostics.AddError("Stream not found", fmt.Sprintf("No stream %q in this organization.", plan.StreamID.ValueString()))
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Error creating series", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, toSeriesModel(plan.StreamID.ValueString(), series))...)
}

func (r *SeriesResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state seriesModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	stream, err := r.client.FindStream(ctx, state.StreamID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error reading series", err.Error())
		return
	}
	if stream == nil {
		resp.State.RemoveResource(ctx)
		return
	}
	for i := range stream.Series {
		if stream.Series[i].ID == state.ID.ValueString() {
			resp.Diagnostics.Append(resp.State.Set(ctx, toSeriesModel(stream.ID, &stream.Series[i]))...)
			return
		}
	}
	resp.State.RemoveResource(ctx)
}

func (r *SeriesResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan seriesModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	name := plan.Name.ValueString()
	unit := plan.Unit.ValueString()
	series, err := r.client.UpdateSeries(ctx, plan.StreamID.ValueString(), plan.ID.ValueString(), &name, &unit)
	if apiErr, ok := err.(*APIError); ok && apiErr.IsAlreadyExists() {
		resp.Diagnostics.AddError("Series already exists", fmt.Sprintf("A sibling series is already named %q.", name))
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Error updating series", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, toSeriesModel(plan.StreamID.ValueString(), series))...)
}

func (r *SeriesResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state seriesModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteSeries(ctx, state.StreamID.ValueString(), state.ID.ValueString())
	if apiErr, ok := err.(*APIError); ok && apiErr.IsNotFound() {
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Error deleting series", err.Error())
	}
}

func (r *SeriesResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.SplitN(req.ID, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		resp.Diagnostics.AddError(
			"Unexpected Import Identifier",
			fmt.Sprintf("Expected an identifier of the form <stream_id>/<series_id>, got: %q", req.ID),
		)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("stream_id"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), parts[1])...)
}

func toSeriesModel(streamID string, s *SeriesInfo) seriesModel {
	lastPointAt := ""
	if s.LastPointAt != nil {
		lastPointAt = *s.LastPointAt
	}
	return seriesModel{
		ID:          types.StringValue(s.ID),
		StreamID:    types.StringValue(streamID),
		Name:        types.StringValue(s.Name),
		Unit:        types.StringValue(s.Unit),
		LastPointAt: types.StringValue(lastPointAt),
	}
}
```

- [ ] **Step 2: Register it in `internal/provider/provider.go`**

```go
func (p *DatageckoProvider) Resources(ctx context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewStreamResource,
		NewSeriesResource,
	}
}
```

- [ ] **Step 3: Write `internal/provider/series_resource_test.go`**

```go
package provider_test

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/funktionswerk/terraform-provider-datagecko/internal/provider/testutil"
)

func TestAccSeriesResource_createUpdateImport(t *testing.T) {
	fake := testutil.NewFakeAPI()
	defer fake.Close()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories(),
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
provider "datagecko" {
  endpoint  = %q
  admin_key = "test"
}

resource "datagecko_stream" "revenue" {
  name = "revenue"
}

resource "datagecko_series" "delivery" {
  stream_id = datagecko_stream.revenue.id
  name      = "delivery"
  unit      = "USD"
}
`, fake.Server.URL),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("datagecko_series.delivery", "name", "delivery"),
					resource.TestCheckResourceAttr("datagecko_series.delivery", "unit", "USD"),
					resource.TestCheckResourceAttrSet("datagecko_series.delivery", "id"),
				),
			},
			{
				Config: fmt.Sprintf(`
provider "datagecko" {
  endpoint  = %q
  admin_key = "test"
}

resource "datagecko_stream" "revenue" {
  name = "revenue"
}

resource "datagecko_series" "delivery" {
  stream_id = datagecko_stream.revenue.id
  name      = "delivered"
  unit      = "EUR"
}
`, fake.Server.URL),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("datagecko_series.delivery", "name", "delivered"),
					resource.TestCheckResourceAttr("datagecko_series.delivery", "unit", "EUR"),
				),
			},
			{
				ResourceName:      "datagecko_series.delivery",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateIdFunc: func(s *terraform.State) (string, error) {
					rs, ok := s.RootModule().Resources["datagecko_series.delivery"]
					if !ok {
						return "", fmt.Errorf("resource not found in state")
					}
					return rs.Primary.Attributes["stream_id"] + "/" + rs.Primary.Attributes["id"], nil
				},
			},
		},
	})
}

func TestAccSeriesResource_deletedOutOfBandIsRecreated(t *testing.T) {
	fake := testutil.NewFakeAPI()
	defer fake.Close()
	config := fmt.Sprintf(`
provider "datagecko" {
  endpoint  = %q
  admin_key = "test"
}

resource "datagecko_stream" "revenue" {
  name = "revenue"
}

resource "datagecko_series" "delivery" {
  stream_id = datagecko_stream.revenue.id
  name      = "delivery"
}
`, fake.Server.URL)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories(),
		Steps: []resource.TestStep{
			{Config: config},
			{
				PreConfig:          func() { fake.ClearAllSeries() },
				Config:             config,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}
```

- [ ] **Step 4: Run the tests and confirm they pass**

Run: `go test ./... -v -run TestAccSeries`
Expected: both PASS.

- [ ] **Step 5: Build and vet**

Run: `go build ./... && go vet ./... && gofmt -l .`
Expected: clean.

- [ ] **Step 6: Commit**

```bash
git add internal/provider/series_resource.go internal/provider/series_resource_test.go internal/provider/provider.go
git commit -m "feat: datagecko_series resource"
```

---

### Task 5: `datagecko_ingest_key` resource

**Files:**
- Create: `internal/provider/ingest_key_resource.go`
- Create: `internal/provider/ingest_key_resource_test.go`
- Modify: `internal/provider/provider.go` (register the resource)

**Interfaces:**
- Consumes: `Client.CreateIngestKey`/`RenameKey`/`DeleteKey`/`FindKey` from Task 2.
- Produces: `NewIngestKeyResource() resource.Resource`.

- [ ] **Step 1: Write `internal/provider/ingest_key_resource.go`**

```go
package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource               = &IngestKeyResource{}
	_ resource.ResourceWithConfigure  = &IngestKeyResource{}
	_ resource.ResourceWithImportState = &IngestKeyResource{}
)

type IngestKeyResource struct {
	client *Client
}

func NewIngestKeyResource() resource.Resource {
	return &IngestKeyResource{}
}

type ingestKeyModel struct {
	ID         types.String `tfsdk:"id"`
	Name       types.String `tfsdk:"name"`
	Secret     types.String `tfsdk:"secret"`
	Created    types.String `tfsdk:"created"`
	LastUsedAt types.String `tfsdk:"last_used_at"`
	Revoked    types.Bool   `tfsdk:"revoked"`
}

func (r *IngestKeyResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_ingest_key"
}

func (r *IngestKeyResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "An ingest-role API key. Its secret is shown once, at creation, and cannot be refetched — importing an existing key leaves secret null.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "The key's prefix — how the API addresses it.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "A rename does not change the id, the secret, or what the key can do.",
			},
			"secret": schema.StringAttribute{
				Computed:      true,
				Sensitive:     true,
				Description:   "Shown once, at creation. Store it now — this state is the only place it's kept.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"created": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"last_used_at": schema.StringAttribute{
				Computed: true,
			},
			"revoked": schema.BoolAttribute{
				Computed: true,
			},
		},
	}
}

func (r *IngestKeyResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Resource Configure Type", fmt.Sprintf("Expected *Client, got: %T", req.ProviderData))
		return
	}
	r.client = client
}

func (r *IngestKeyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan ingestKeyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	key, err := r.client.CreateIngestKey(ctx, plan.Name.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error creating ingest key", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, ingestKeyModel{
		ID:         types.StringValue(key.Prefix),
		Name:       types.StringValue(key.Name),
		Secret:     types.StringValue(key.Secret),
		Created:    types.StringValue(key.Created),
		LastUsedAt: types.StringValue(optionalString(key.LastUsedAt)),
		Revoked:    types.BoolValue(key.Revoked),
	})...)
}

func (r *IngestKeyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state ingestKeyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	key, err := r.client.FindKey(ctx, state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error reading ingest key", err.Error())
		return
	}
	if key == nil || key.Revoked {
		resp.State.RemoveResource(ctx)
		return
	}

	state.Name = types.StringValue(key.Name)
	state.Created = types.StringValue(key.Created)
	state.LastUsedAt = types.StringValue(optionalString(key.LastUsedAt))
	state.Revoked = types.BoolValue(key.Revoked)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *IngestKeyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan ingestKeyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var state ingestKeyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	key, err := r.client.RenameKey(ctx, state.ID.ValueString(), plan.Name.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error renaming ingest key", err.Error())
		return
	}

	state.Name = types.StringValue(key.Name)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *IngestKeyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state ingestKeyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteKey(ctx, state.ID.ValueString())
	if apiErr, ok := err.(*APIError); ok && apiErr.IsNotFound() {
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Error revoking ingest key", err.Error())
	}
}

func (r *IngestKeyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func optionalString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
```

- [ ] **Step 2: Register it in `internal/provider/provider.go`**

```go
func (p *DatageckoProvider) Resources(ctx context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewStreamResource,
		NewSeriesResource,
		NewIngestKeyResource,
	}
}
```

- [ ] **Step 3: Write `internal/provider/ingest_key_resource_test.go`**

```go
package provider_test

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/funktionswerk/terraform-provider-datagecko/internal/provider/testutil"
)

func TestAccIngestKeyResource_createRenameRevokedDrift(t *testing.T) {
	fake := testutil.NewFakeAPI()
	defer fake.Close()
	var keyID string

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories(),
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
provider "datagecko" {
  endpoint  = %q
  admin_key = "test"
}

resource "datagecko_ingest_key" "feed" {
  name = "shop-feed"
}
`, fake.Server.URL),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("datagecko_ingest_key.feed", "name", "shop-feed"),
					resource.TestCheckResourceAttrSet("datagecko_ingest_key.feed", "secret"),
					resource.TestCheckResourceAttrWith("datagecko_ingest_key.feed", "id", func(value string) error {
						keyID = value
						return nil
					}),
				),
			},
			{
				// Renaming keeps the id and the secret — no replace plan.
				Config: fmt.Sprintf(`
provider "datagecko" {
  endpoint  = %q
  admin_key = "test"
}

resource "datagecko_ingest_key" "feed" {
  name = "renamed-feed"
}
`, fake.Server.URL),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("datagecko_ingest_key.feed", "name", "renamed-feed"),
					resource.TestCheckResourceAttrWith("datagecko_ingest_key.feed", "id", func(value string) error {
						if value != keyID {
							return fmt.Errorf("expected the id to stay %q, got %q", keyID, value)
						}
						return nil
					}),
				),
			},
			{
				// Revoked outside Terraform: expect a non-empty plan (recreate).
				PreConfig: func() { fake.Revoke(keyID) },
				Config: fmt.Sprintf(`
provider "datagecko" {
  endpoint  = %q
  admin_key = "test"
}

resource "datagecko_ingest_key" "feed" {
  name = "renamed-feed"
}
`, fake.Server.URL),
				ExpectNonEmptyPlan: true,
			},
		},
	})
}
```

- [ ] **Step 4: Run the tests and confirm they pass**

Run: `go test ./... -v -run TestAccIngestKey`
Expected: PASS.

- [ ] **Step 5: Build and vet**

Run: `go build ./... && go vet ./... && gofmt -l .`
Expected: clean.

- [ ] **Step 6: Run the full test suite**

Run: `go test ./...`
Expected: every package's tests pass — this is the first point all three resources exist together, so it also catches any cross-resource regression.

- [ ] **Step 7: Commit**

```bash
git add internal/provider/ingest_key_resource.go internal/provider/ingest_key_resource_test.go internal/provider/provider.go
git commit -m "feat: datagecko_ingest_key resource"
```

---

### Task 6: Examples and local install docs

**Files:**
- Create: `examples/provider/provider.tf`
- Create: `examples/resources/datagecko_stream/resource.tf`
- Create: `examples/resources/datagecko_series/resource.tf`
- Create: `examples/resources/datagecko_ingest_key/resource.tf`
- Create: `README.md`

**Interfaces:**
- Consumes: nothing new — this task only writes `.tf` and Markdown, no Go.
- Produces: nothing later depends on it; this is the plan's last deliverable.

- [ ] **Step 1: Write `examples/provider/provider.tf`**

```hcl
terraform {
  required_providers {
    datagecko = {
      source = "registry.terraform.io/funktionswerk/datagecko"
    }
  }
}

provider "datagecko" {
  # admin_key can also come from the DATAGECKO_ADMIN_KEY environment variable.
  admin_key = var.datagecko_admin_key
}

variable "datagecko_admin_key" {
  type      = string
  sensitive = true
  default   = null
}
```

- [ ] **Step 2: Write `examples/resources/datagecko_stream/resource.tf`**

```hcl
resource "datagecko_stream" "revenue" {
  name        = "revenue"
  description = "Daily revenue, by delivery status."

  # A stream's DELETE removes every series it holds and their points, and
  # cannot be undone — guard against an accidental destroy.
  lifecycle {
    prevent_destroy = true
  }
}
```

- [ ] **Step 3: Write `examples/resources/datagecko_series/resource.tf`**

```hcl
resource "datagecko_series" "delivery" {
  stream_id = datagecko_stream.revenue.id
  name      = "delivery"
  unit      = "USD"
}
```

- [ ] **Step 4: Write `examples/resources/datagecko_ingest_key/resource.tf`**

```hcl
resource "datagecko_ingest_key" "shop_feed" {
  name = "shop-feed"
}

output "shop_feed_secret" {
  value     = datagecko_ingest_key.shop_feed.secret
  sensitive = true
}
```

- [ ] **Step 5: Write `README.md`**

```markdown
# terraform-provider-datagecko

Manages [datagecko](https://datagecko.ai) streams, series, and ingest keys for one organization, authenticated by that organization's admin key.

## Building and installing locally

This provider isn't published to the Terraform Registry. Build it and point Terraform at the binary with a dev override:

    go build -o terraform-provider-datagecko

Add to `~/.terraformrc`:

    provider_installation {
      dev_overrides {
        "registry.terraform.io/funktionswerk/datagecko" = "/absolute/path/to/this/checkout"
      }
      direct {}
    }

With a dev override active, `terraform init` is skipped for this provider — just run `terraform plan`/`apply` directly.

## Usage

See `examples/`. Set `DATAGECKO_ADMIN_KEY` or pass `admin_key` in the `provider` block — either way, one provider instance (or one `alias`) manages exactly one organization, because an admin key *is* an organization's credential.

## Design and implementation plan

- `docs/superpowers/specs/2026-09-21-terraform-provider-datagecko-design.md`
- `docs/superpowers/plans/2026-09-21-terraform-provider-datagecko.md`
```

- [ ] **Step 6: Commit**

```bash
git add examples README.md
git commit -m "docs: usage examples and local install instructions"
```

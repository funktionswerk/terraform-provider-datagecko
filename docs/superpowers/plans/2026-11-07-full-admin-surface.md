# Full Admin Surface Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Every declaration an admin key can make under `/api/v1/` becomes a Terraform resource: series kind and cadence, metrics and their stream rows, charts, sections, query panels, actions, runner hooks, plus a metric-types data source.

**Architecture:** One resource file per resource under `internal/provider/`, each built on the one HTTP client (`client.go`) and tested through real plan/apply/destroy cycles against the in-memory fake (`testutil/fake_api.go`), exactly as the existing three resources are. Free-form JSON attributes are `jsontypes.Normalized`, forwarded to the API as raw bytes and compared by value, so `jsonb` key reordering never shows as a diff. The three replace-whole lists are one resource per stream whose Create refuses a non-empty list; the three create-never-adopt endpoints surface a 409 as "import it".

**Tech Stack:** Go 1.25 · `terraform-plugin-framework` v1.19 · `terraform-plugin-framework-jsontypes` v0.2.0 (new) · `terraform-plugin-testing` v1.16 · Terraform CLI 1.16 locally.

**Spec:** `docs/superpowers/specs/2026-11-07-full-admin-surface-design.md` (this repo). The API prerequisites it names (§4.7.1, §4.10) are merged in `datagecko.ai` `main` as of 2026-11-07 (PR #15); the API shapes below are read from that code.

**Branch:** `feature/full-admin-surface` off `main` (`2fd487d`). `docs/` is untracked today; the first commit adds the spec and this plan.

## Global Constraints

- One HTTP client: every call lands in `internal/provider/client.go`; no resource builds a request by hand (`CLAUDE.md`).
- One fake: every new endpoint lands in `internal/provider/testutil/fake_api.go`; every resource is tested with `resource.UnitTest` against it; no test talks to a real datagecko.
- JSON attributes (`params`, `effective`, `parameters`, `parameters_schema`, `defaults`) are `jsontypes.Normalized`; no `types.Dynamic`, no nested-object schemas for params (spec §5).
- The client forwards JSON attributes as raw bytes (`json.RawMessage`), never re-marshalled through `float64` (spec §5).
- The provider never normalises, defaults in, or reorders on the way through; bounds and vocabularies are the server's (spec §2 anti-goals). The only provider-side validator is the existing series-name regex.
- A secret shown once is `Computed` + `Sensitive`, never read back, `Unknown` in the plan whenever it rotates (spec §3).
- Resources read through a list (`GET /streams`, `GET /actions`, `GET /actions/runners`) find their row by key and remove themselves from state when absent (spec §3).
- Error messages name the remedy: an existing row is "declared outside Terraform; import it".
- `gofmt -l .` empty, `go vet ./...` clean, `go test ./...` green before every commit. No attribution trailers in commits (`CLAUDE.md`).
- Every resource carries a `Computed` `id` attribute holding its import identifier (the spec says the stream-metric pair "has no id of its own": `terraform-plugin-testing`'s `ImportStateVerify` matches the imported resource by `id`, so each resource exposes one; it is documented as the import id and nothing more).

## Review Focus

1. A JSON attribute written with keys in another order than the API returns them (`jsonb` sorts) must plan clean after apply — Task 3 `TestAccMetricResource_jsonKeyOrderIsNotADiff`.
2. A chart style field the user never wrote must not read back as a value: absent is null, and a plan after apply is empty — Task 6 `TestAccStreamChartsResource_unsetStyleStaysNull`.
3. A section body with a trailing newline (a heredoc) must round-trip byte-exact — Task 7 `TestAccStreamSectionsResource_bodyRoundTripsExactly`.
4. Creating a charts/sections/panels resource on a stream that already has a declared list must refuse, not overwrite — Task 6 `TestAccStreamChartsResource_existingListIsRefusedOnCreate`.
5. Changing a hook's `url` must plan `secret` as unknown and apply a new one; an unchanged `url` keeps it — Task 10 `TestAccRunnerHookResource_urlChangeRotatesTheSecret`.

---

### Task 0: Branch and documents

**Files:**
- Add: `docs/superpowers/specs/2026-11-07-full-admin-surface-design.md`, `docs/superpowers/plans/2026-11-07-full-admin-surface.md`

- [ ] **Step 1: Branch and commit the documents**

```bash
git checkout -b feature/full-admin-surface main
git add docs/
git commit -m "docs: full admin surface design and plan"
```

---

### Task 1: Rate-limit retry loop and the jsontypes dependency

**Files:**
- Modify: `go.mod`, `go.sum`
- Modify: `internal/provider/client.go:53-106`
- Test: `internal/provider/client_test.go:124-149`

**Interfaces:**
- Produces: `do(ctx, method, path, body, out)` retries a 429 up to `maxRateLimitRetries = 3` times, sleeping `Retry-After` seconds each time (0 when the header is absent or malformed), and returns the 429 as `*APIError` after the third retry. `doOnce`'s `retried bool` parameter goes away.
- Produces: `github.com/hashicorp/terraform-plugin-framework-jsontypes v0.2.0` in `go.mod`, imported first in Task 3.

- [ ] **Step 1: Replace the two retry tests**

Replace `TestClientRetriesOnceOn429` and `TestClientGivesUpAfterOneRetry` with:

```go
func TestClientRetriesUpToThreeTimesOn429(t *testing.T) {
	fake := testutil.NewFakeAPI()
	defer fake.Close()
	fake.RateLimitNextRequests(3)
	client := NewClient(fake.Server.URL, "test", nil)

	_, err := client.CreateStream(context.Background(), "revenue", "", nil)

	if err != nil {
		t.Fatalf("expected the third retry to succeed, got %v", err)
	}
}

func TestClientGivesUpAfterThreeRetries(t *testing.T) {
	fake := testutil.NewFakeAPI()
	defer fake.Close()
	fake.RateLimitNextRequests(4)
	client := NewClient(fake.Server.URL, "test", nil)

	_, err := client.CreateStream(context.Background(), "revenue", "", nil)

	apiErr, ok := err.(*APIError)
	if !ok || apiErr.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected a 429 APIError after three retries, got %v", err)
	}
}
```

- [ ] **Step 2: Run them**

Run: `go test ./internal/provider/ -run 'TestClient.*429|TestClientGivesUp' -v`
Expected: `TestClientRetriesUpToThreeTimesOn429` FAILS (the second 429 is returned); `TestClientGivesUpAfterThreeRetries` passes already (it would also pass with one retry — that is fine, it pins the ceiling once the loop exists).

- [ ] **Step 3: Implement the loop in `client.go`**

`const maxRateLimitRetries = 3`. `do` becomes a `for attempt := 0; ; attempt++` loop around the request; on a 429 with `attempt < maxRateLimitRetries`, wait `Retry-After` (honouring `ctx.Done()`) and continue; otherwise fall through to the error decoding. Build a fresh `bytes.NewReader(encoded)` per attempt (the body is consumed). Delete `doOnce`.

- [ ] **Step 4: Run the client tests**

Run: `go test ./internal/provider/ -run TestClient -v`
Expected: PASS

- [ ] **Step 5: Add the dependency**

```bash
go get github.com/hashicorp/terraform-plugin-framework-jsontypes@v0.2.0
go mod tidy
go build ./...
```

Expected: `go.mod` lists `terraform-plugin-framework-jsontypes v0.2.0` as `// indirect` until Task 3 imports it; the build is clean.

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum internal/provider/client.go internal/provider/client_test.go
git commit -m "feat(client): retry a 429 up to three windows, honouring Retry-After; add jsontypes"
```

---

### Task 2: `datagecko_series` — `kind` and `interval_days`

**Files:**
- Modify: `internal/provider/client.go` (`SeriesInfo`, `CreateSeries`, `UpdateSeries`)
- Modify: `internal/provider/testutil/fake_api.go` (`fakeSeries`, `createSeries`, `updateSeries`, `streamJSON`)
- Modify: `internal/provider/series_resource.go`
- Create: `internal/provider/helpers_test.go` (`providerBlock`)
- Modify: `internal/provider/series_resource_test.go`, `internal/provider/client_test.go`
- Modify: `examples/resources/datagecko_series/resource.tf`

**Interfaces:**
- Consumes: Task 1's client.
- Produces:
  ```go
  type SeriesInfo struct {
  	ID           string  `json:"id"`
  	Name         string  `json:"name"`
  	Unit         string  `json:"unit"`
  	Kind         string  `json:"kind"`
  	IntervalDays *int64  `json:"interval_days"`
  	LastPointAt  *string `json:"last_point_at"`
  }
  func (c *Client) CreateSeries(ctx context.Context, streamID, name, unit, kind string, intervalDays *int64) (*SeriesInfo, error)
  // Sends kind only when non-empty and interval_days only when non-nil.
  func (c *Client) UpdateSeries(ctx context.Context, streamID, seriesID string, name, unit *string, intervalDays *int64) (*SeriesInfo, error)
  // Always sends "interval_days": the value, or JSON null when intervalDays is nil (PATCH null clears).
  ```
- Produces (test helper, `package provider_test`):
  ```go
  // providerBlock is the provider stanza every acceptance config starts with.
  func providerBlock(endpointURL string) string
  ```

- [ ] **Step 1: Write the failing client test** in `client_test.go`

```go
func TestSeriesKindAndIntervalRoundTrip(t *testing.T) {
	fake := testutil.NewFakeAPI()
	defer fake.Close()
	client := NewClient(fake.Server.URL, "test", nil)
	ctx := context.Background()
	stream, _ := client.CreateStream(ctx, "revenue", "", nil)
	seven := int64(7)

	series, err := client.CreateSeries(ctx, stream.ID, "deployed", "", "marker", &seven)
	if err != nil {
		t.Fatalf("CreateSeries: %v", err)
	}
	if series.Kind != "marker" || series.IntervalDays == nil || *series.IntervalDays != 7 {
		t.Fatalf("expected kind marker and interval 7 back, got %+v", series)
	}

	cleared, err := client.UpdateSeries(ctx, stream.ID, series.ID, nil, nil, nil)
	if err != nil {
		t.Fatalf("UpdateSeries: %v", err)
	}
	if cleared.IntervalDays != nil {
		t.Fatalf("expected a null interval_days to clear the cadence, got %v", *cleared.IntervalDays)
	}
	if cleared.Kind != "marker" {
		t.Fatalf("expected the kind to be untouched by a PATCH, got %q", cleared.Kind)
	}
}
```

Update the existing `CreateSeries`/`UpdateSeries` call sites in `client_test.go` (`TestSeriesAndKeyLifecycleAgainstTheFake`) to the new signatures: `CreateSeries(ctx, stream.ID, "delivery", "USD", "", nil)` and `UpdateSeries(ctx, stream.ID, series.ID, &newName, nil, nil)`.

- [ ] **Step 2: Run it**

Run: `go test ./internal/provider/ -run TestSeriesKindAndIntervalRoundTrip -v`
Expected: compile error on the new signature. Add the two parameters to the client signatures without using them, re-run, and expect FAIL on `expected kind marker and interval 7 back` (the fake does not know the fields yet).

- [ ] **Step 3: Implement client and fake**

Client: the structs and bodies above (`map[string]interface{}`; `interval_days: nil` marshals to `null`).
Fake: `fakeSeries` gains `Kind string` and `IntervalDays *int64`; `createSeries` decodes `kind` (default `"measure"` when empty) and `interval_days` (`*int64`); `updateSeries` decodes into `map[string]json.RawMessage` so it can tell an absent key from `null`: `interval_days` present and `null` clears, present and a number sets, absent keeps; `kind` in a PATCH body is ignored (the API's serializer has no such field). `streamJSON` and the series responses carry `"kind"` and `"interval_days"` (`nil` → `null`).

- [ ] **Step 4: Run the client tests**

Run: `go test ./internal/provider/ -run 'TestSeries|TestClient' -v`
Expected: PASS

- [ ] **Step 5: Write the failing acceptance test** in `series_resource_test.go`

First add `helpers_test.go`:

```go
package provider_test

import "fmt"

func providerBlock(endpointURL string) string {
	return fmt.Sprintf(`
provider "datagecko" {
  endpoint  = %q
  admin_key = "test"
}
`, endpointURL)
}
```

Then:

```go
func TestAccSeriesResource_kindAndInterval(t *testing.T) {
	fake := testutil.NewFakeAPI()
	defer fake.Close()
	withInterval := providerBlock(fake.Server.URL) + `
resource "datagecko_stream" "revenue" { name = "revenue" }

resource "datagecko_series" "deployed" {
  stream_id     = datagecko_stream.revenue.id
  name          = "deployed"
  kind          = "marker"
  interval_days = 7
}
`
	withoutInterval := providerBlock(fake.Server.URL) + `
resource "datagecko_stream" "revenue" { name = "revenue" }

resource "datagecko_series" "deployed" {
  stream_id = datagecko_stream.revenue.id
  name      = "deployed"
  kind      = "marker"
}
`
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories(),
		Steps: []resource.TestStep{
			{
				Config: withInterval,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("datagecko_series.deployed", "kind", "marker"),
					resource.TestCheckResourceAttr("datagecko_series.deployed", "interval_days", "7"),
				),
			},
			{
				// Dropping the cadence is an in-place PATCH {"interval_days": null}, not a replace.
				Config: withoutInterval,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("datagecko_series.deployed", plancheck.ResourceActionUpdate),
				}},
				Check: resource.TestCheckNoResourceAttr("datagecko_series.deployed", "interval_days"),
			},
			{
				ResourceName:      "datagecko_series.deployed",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateIdFunc: seriesImportID("datagecko_series.deployed"),
			},
		},
	})
}

func TestAccSeriesResource_kindDefaultsToMeasureAndAChangeReplaces(t *testing.T) {
	fake := testutil.NewFakeAPI()
	defer fake.Close()
	measure := providerBlock(fake.Server.URL) + `
resource "datagecko_stream" "revenue" { name = "revenue" }
resource "datagecko_series" "delivery" {
  stream_id = datagecko_stream.revenue.id
  name      = "delivery"
}
`
	marker := providerBlock(fake.Server.URL) + `
resource "datagecko_stream" "revenue" { name = "revenue" }
resource "datagecko_series" "delivery" {
  stream_id = datagecko_stream.revenue.id
  name      = "delivery"
  kind      = "marker"
}
`
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories(),
		Steps: []resource.TestStep{
			{Config: measure, Check: resource.TestCheckResourceAttr("datagecko_series.delivery", "kind", "measure")},
			{
				Config: marker,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("datagecko_series.delivery", plancheck.ResourceActionReplace),
				}},
			},
		},
	})
}
```

Extract the existing inline `ImportStateIdFunc` in `TestAccSeriesResource_createUpdateImport` into `seriesImportID(name string) resource.ImportStateIdFunc` in `helpers_test.go` and use it in both tests.

- [ ] **Step 6: Run them**

Run: `go test ./internal/provider/ -run TestAccSeriesResource -v`
Expected: the two new tests FAIL on an unsupported attribute `kind`.

- [ ] **Step 7: Implement the schema and CRUD in `series_resource.go`**

Schema: `kind` `StringAttribute{Optional, Computed, Default: stringdefault.StaticString("measure"), PlanModifiers: RequiresReplace}`; `interval_days` `Int64Attribute{Optional}` (no Computed: null is a real value, "no cadence"). Model gains `Kind types.String` and `IntervalDays types.Int64`. Create passes `plan.Kind.ValueString()` and `plan.IntervalDays.ValueInt64Pointer()`. Update passes `plan.IntervalDays.ValueInt64Pointer()` (nil when null → the client sends `null`). `toSeriesModel` maps `Kind` and `IntervalDays` (`nil` → `types.Int64Null()`).

- [ ] **Step 8: Run the series tests, then the suite**

Run: `go test ./internal/provider/ -run TestAccSeriesResource -v` then `go test ./...`
Expected: PASS, PASS

- [ ] **Step 9: Example and commit**

Append to `examples/resources/datagecko_series/resource.tf`:

```hcl
resource "datagecko_series" "deployed" {
  stream_id     = datagecko_stream.revenue.id
  name          = "deployed"
  kind          = "marker" # shown as a value, never plotted; cannot change after creation
  interval_days = 7        # declared cadence; freshness measures days behind net of it
}
```

```bash
gofmt -l . && go vet ./...
git add -A internal/provider examples
git commit -m "feat(series): kind (replace on change) and interval_days (cleared by null)"
```

---

### Task 3: `datagecko_metric`

**Files:**
- Create: `internal/provider/json_attrs.go`
- Modify: `internal/provider/client.go`
- Modify: `internal/provider/testutil/fake_api.go`
- Create: `internal/provider/metric_resource.go`, `internal/provider/metric_resource_test.go`
- Modify: `internal/provider/provider.go` (register)
- Create: `examples/resources/datagecko_metric/resource.tf`

**Interfaces:**
- Produces (`json_attrs.go`):
  ```go
  // normalized wraps the API's bytes as a JSON attribute value; nil bytes become "{}".
  func normalized(raw json.RawMessage) jsontypes.Normalized
  // rawJSON is the attribute's bytes exactly as the user wrote them; null or unknown → nil.
  func rawJSON(v jsontypes.Normalized) json.RawMessage
  // jsonAttribute is the schema for an Optional JSON attribute defaulting to {}.
  func jsonAttribute(description string) schema.StringAttribute
  // computedJSONAttribute is the schema for a read-only JSON attribute.
  func computedJSONAttribute(description string) schema.StringAttribute
  ```
  `jsonAttribute`: `CustomType: jsontypes.NormalizedType{}`, `Optional`, `Computed`, `Default: stringdefault.StaticString("{}")`. `computedJSONAttribute`: `CustomType`, `Computed`, no plan modifier (its value changes whenever params change; `UseStateForUnknown` would make the apply "inconsistent").
- Produces (client):
  ```go
  type Metric struct {
  	ID        string          `json:"id"`
  	Type      string          `json:"type"`
  	Name      string          `json:"name"`
  	Params    json.RawMessage `json:"params"`
  	Effective json.RawMessage `json:"effective"`
  }
  func (c *Client) CreateMetric(ctx context.Context, typ, name string, params json.RawMessage) (*Metric, error) // POST /metrics
  func (c *Client) GetMetric(ctx context.Context, id string) (*Metric, error)                                  // GET /metrics/{id}; a 404 is an *APIError
  func (c *Client) UpdateMetric(ctx context.Context, id, name string, params json.RawMessage) (*Metric, error) // PATCH /metrics/{id} {"name","params"}
  func (c *Client) DeleteMetric(ctx context.Context, id string) error
  ```
  Request bodies holding `json.RawMessage` are structs with `json:"params,omitempty"` so `json.Marshal` embeds the bytes verbatim.
- Produces (fake): `metrics map[string]*fakeMetric{ID, Type, Name string; Params map[string]interface{}}`; a type table `fakeMetricTypes` with
  `freshness` defaults `{"warning_after": 2, "critical_after": 7}`, required `[]`, per_stream `[]`, presentation `"badge"`;
  `coverage` defaults `{"markers": [], "target": "", "window_days": 30, "warning_after": 1, "critical_after": 3}`, required `[]`, per_stream `["markers", "target"]`, presentation `"badge"`.
  `effective` = defaults shallow-merged with params. `POST /metrics`: 400 `payload_refused` for an unknown type; 409 `already_exists` when a metric's name matches case-insensitively. `PATCH`: 404 when missing, 409 on a taken name, `params` replaces whole. `DELETE`: 404 when missing. `DeleteAllMetrics()` test hook. The fake stores params as decoded JSON and re-encodes them, so its read-back is key-sorted — the same reordering `jsonb` does.

- [ ] **Step 1: Write the failing client test** in `client_test.go`

```go
func TestMetricLifecycleAgainstTheFake(t *testing.T) {
	fake := testutil.NewFakeAPI()
	defer fake.Close()
	client := NewClient(fake.Server.URL, "test", nil)
	ctx := context.Background()

	created, err := client.CreateMetric(ctx, "freshness", "Freshness", json.RawMessage(`{"warning_after": 3}`))
	if err != nil {
		t.Fatalf("CreateMetric: %v", err)
	}
	if string(created.Effective) != `{"critical_after":7,"warning_after":3}` {
		t.Fatalf("expected defaults under params, got %s", created.Effective)
	}

	_, err = client.CreateMetric(ctx, "freshness", "FRESHNESS", nil)
	if apiErr, ok := err.(*APIError); !ok || !apiErr.IsAlreadyExists() {
		t.Fatalf("expected a case-insensitive name conflict, got %v", err)
	}

	updated, err := client.UpdateMetric(ctx, created.ID, "Fresh", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("UpdateMetric: %v", err)
	}
	if updated.Name != "Fresh" || string(updated.Params) != `{}` {
		t.Fatalf("expected the rename and params replaced whole, got %+v", updated)
	}
	if err := client.DeleteMetric(ctx, created.ID); err != nil {
		t.Fatalf("DeleteMetric: %v", err)
	}
	_, err = client.GetMetric(ctx, created.ID)
	if apiErr, ok := err.(*APIError); !ok || !apiErr.IsNotFound() {
		t.Fatalf("expected not found after delete, got %v", err)
	}
}
```

- [ ] **Step 2: Run it**

Run: `go test ./internal/provider/ -run TestMetricLifecycleAgainstTheFake -v`
Expected: compile error (`CreateMetric` undefined). Declare the four methods returning `nil, errors.New("not implemented")`, re-run, expect FAIL at `CreateMetric: not implemented`.

- [ ] **Step 3: Implement client and fake** as specified in Interfaces. Fake routes: `GET/POST metrics`, `GET/PATCH/DELETE metrics/{id}` (the `metrics` segment check must come before the existing `streams` cases only in the sense that both are exact-match; no overlap).

- [ ] **Step 4: Run it**

Run: `go test ./internal/provider/ -run TestMetricLifecycleAgainstTheFake -v`
Expected: PASS

- [ ] **Step 5: Write the failing acceptance tests** in `metric_resource_test.go`

```go
func TestAccMetricResource_createUpdateImport(t *testing.T) {
	fake := testutil.NewFakeAPI()
	defer fake.Close()
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories(),
		Steps: []resource.TestStep{
			{
				Config: providerBlock(fake.Server.URL) + `
resource "datagecko_metric" "freshness" {
  type   = "freshness"
  name   = "Freshness"
  params = jsonencode({ warning_after = 3 })
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("datagecko_metric.freshness", "id"),
					resource.TestCheckResourceAttr("datagecko_metric.freshness", "params", `{"warning_after":3}`),
					resource.TestCheckResourceAttr("datagecko_metric.freshness", "effective", `{"critical_after":7,"warning_after":3}`),
				),
			},
			{
				// A rename and new params update in place.
				Config: providerBlock(fake.Server.URL) + `
resource "datagecko_metric" "freshness" {
  type   = "freshness"
  name   = "Fresh"
  params = jsonencode({ warning_after = 4, critical_after = 9 })
}
`,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("datagecko_metric.freshness", plancheck.ResourceActionUpdate),
				}},
				Check: resource.TestCheckResourceAttr("datagecko_metric.freshness", "effective", `{"critical_after":9,"warning_after":4}`),
			},
			{ResourceName: "datagecko_metric.freshness", ImportState: true, ImportStateVerify: true},
		},
	})
}

func TestAccMetricResource_paramsDefaultToEmptyObject(t *testing.T) {
	// name only: params is {} in state and effective is the type's defaults.
	// Check: params == "{}" and effective == `{"critical_after":7,"warning_after":2}`.
}

func TestAccMetricResource_typeChangeReplaces(t *testing.T) {
	// freshness → coverage: plancheck.ResourceActionReplace.
}

func TestAccMetricResource_jsonKeyOrderIsNotADiff(t *testing.T) {
	// params = "{\"warning_after\": 3, \"critical_after\": 9}" (a raw string, keys out of
	// order, spaces) — the fake returns `{"critical_after":9,"warning_after":3}`. One
	// step, no ExpectNonEmptyPlan: the harness's post-apply plan must be empty, and
	// state keeps the user's bytes:
	// Check: params == "{\"warning_after\": 3, \"critical_after\": 9}".
}

func TestAccMetricResource_takenNameIsARealError(t *testing.T) {
	// two metrics named "Freshness" and "freshness": ExpectError "already exists".
}

func TestAccMetricResource_deletedOutOfBandIsRecreated(t *testing.T) {
	// PreConfig: fake.DeleteAllMetrics(); PlanOnly; ExpectNonEmptyPlan — as the series test.
}
```

Write the four sketched tests out in full in the same style as the first.

- [ ] **Step 6: Run them**

Run: `go test ./internal/provider/ -run TestAccMetricResource -v`
Expected: FAIL — `datagecko_metric` is not a resource type the provider offers.

- [ ] **Step 7: Implement `json_attrs.go` and `metric_resource.go`; register `NewMetricResource` in `provider.go`**

Model:
```go
type metricModel struct {
	ID        types.String         `tfsdk:"id"`
	Type      types.String         `tfsdk:"type"`
	Name      types.String         `tfsdk:"name"`
	Params    jsontypes.Normalized `tfsdk:"params"`
	Effective jsontypes.Normalized `tfsdk:"effective"`
}
```
Schema: `id` Computed + `UseStateForUnknown`; `type` Required + `RequiresReplace`; `name` Required; `params` `jsonAttribute(...)`; `effective` `computedJSONAttribute(...)`. Create: 409 → `"Metric already exists"` / `"A metric named %q is declared outside Terraform; import it."`. Read: `GetMetric`; 404 → `RemoveResource`. Update: `UpdateMetric(id, plan.Name, rawJSON(plan.Params))`; 409 → the same "already exists" wording. Delete: 404 ignored. Import: passthrough on `id`. `toMetricModel(m *Metric) metricModel` maps `Params`/`Effective` through `normalized`.

- [ ] **Step 8: Run the metric tests, then the suite**

Run: `go test ./internal/provider/ -run TestAccMetricResource -v`; then `go test ./...`
Expected: PASS. If `TestAccMetricResource_jsonKeyOrderIsNotADiff` reports a non-empty plan, the semantic-equality path is not engaged: confirm the model field type is `jsontypes.Normalized` (not `types.String`) and that `CustomType` is set on the attribute.

- [ ] **Step 9: Example, docs line, commit**

`examples/resources/datagecko_metric/resource.tf`:
```hcl
resource "datagecko_metric" "freshness" {
  type   = "freshness"
  name   = "Freshness"
  params = jsonencode({ warning_after = 2, critical_after = 7 })
}
```

```bash
gofmt -l . && go vet ./...
git add -A internal/provider examples
git commit -m "feat: datagecko_metric — an instance of a metric type, params compared as JSON"
```

---

### Task 4: `datagecko_stream_metric`

**Files:**
- Modify: `internal/provider/client.go`, `internal/provider/testutil/fake_api.go`
- Create: `internal/provider/stream_metric_resource.go`, `internal/provider/stream_metric_resource_test.go`
- Modify: `internal/provider/provider.go`
- Create: `examples/resources/datagecko_stream_metric/resource.tf`

**Interfaces:**
- Consumes: Task 3's `normalized`, `rawJSON`, `jsonAttribute`, `computedJSONAttribute`, fake metrics table.
- Produces (client):
  ```go
  type StreamMetric struct {
  	Metric    string          `json:"metric"`
  	Stream    string          `json:"stream"`
  	Params    json.RawMessage `json:"params"`
  	Effective json.RawMessage `json:"effective"`
  }
  func (c *Client) CreateStreamMetric(ctx context.Context, streamID, metricID string, params json.RawMessage) (*StreamMetric, error)  // POST /streams/{id}/metrics {"metric_id","params"}
  func (c *Client) GetStreamMetric(ctx context.Context, streamID, metricID string) (*StreamMetric, error)                             // GET
  func (c *Client) ReplaceStreamMetric(ctx context.Context, streamID, metricID string, params json.RawMessage) (*StreamMetric, error) // PUT {"params"}
  func (c *Client) DeleteStreamMetric(ctx context.Context, streamID, metricID string) error
  ```
- Produces (fake): `fakeStream.Metrics map[metricID]map[string]interface{}` (params per row). `POST`: 404 when the stream or metric is missing, 409 `already_exists` when the row exists, 201 otherwise. `GET`/`PUT`/`DELETE`: 404 when the stream, the metric or the row is missing. `effective` = type defaults, then the instance's params, then the row's. `PUT` replaces params whole. Route order: `streams/{id}/metrics` (3 segments) and `streams/{id}/metrics/{mid}` (4 segments) sit beside the existing `series` cases.

- [ ] **Step 1: Write the failing client test** — `TestStreamMetricLifecycleAgainstTheFake`: create a stream, a coverage metric; `CreateStreamMetric` with `{"target": "<series id>"}` → `Effective` equals `{"critical_after":3,"markers":[],"target":"<id>","warning_after":1,"window_days":30}`; a second create → `IsAlreadyExists`; `ReplaceStreamMetric` with `{}` → `Params == "{}"`; `ReplaceStreamMetric` on an unknown metric id → `IsNotFound`; delete → `GetStreamMetric` `IsNotFound`.

- [ ] **Step 2: Run it** — Expected: compile error → declare stubs → FAIL `not implemented`.

- [ ] **Step 3: Implement client and fake.**

- [ ] **Step 4: Run it** — Expected: PASS.

- [ ] **Step 5: Write the failing acceptance tests** in `stream_metric_resource_test.go`

```go
func TestAccStreamMetricResource_createUpdateImport(t *testing.T) {
	// stream "revenue", series "deployed" (kind marker), metric coverage "Coverage",
	// stream_metric { stream_id, metric_id, params = jsonencode({ markers = [datagecko_series.deployed.id] }) }
	// Check: id == stream_id + "/" + metric_id (TestCheckResourceAttrPair-free: use a
	// func(s *terraform.State) error reading the three attributes); effective contains "window_days":30.
	// Step 2: params = jsonencode({}) → ResourceActionUpdate, params == "{}".
	// Step 3: ImportState with ImportStateIdFunc returning stream_id + "/" + metric_id, ImportStateVerify.
}

func TestAccStreamMetricResource_existingRowIsRefusedOnCreate(t *testing.T) {
	// PreConfig-free: two datagecko_stream_metric resources with the same stream and metric
	// → ExpectError "declared outside Terraform; import it".
}

func TestAccStreamMetricResource_streamOrMetricChangeReplaces(t *testing.T) {
	// a second stream; switching stream_id → plancheck.ResourceActionReplace.
}
```

- [ ] **Step 6: Run them** — Expected: FAIL, unknown resource type.

- [ ] **Step 7: Implement `stream_metric_resource.go`; register it**

Model: `ID`, `StreamID`, `MetricID types.String`; `Params`, `Effective jsontypes.Normalized`. `id` Computed `UseStateForUnknown`, set to `stream_id + "/" + metric_id`. `stream_id` and `metric_id` Required + `RequiresReplace`. Create: 409 → `"Stream metric already exists"` / `"Metric %s is already set on stream %s — declared outside Terraform; import it as %s/%s."`; 404 → `"Stream or metric not found"`. Read: 404 → `RemoveResource`. Update: `ReplaceStreamMetric`; 404 → error `"Stream metric is gone"` (a replace never creates; the next plan will). Delete: 404 ignored. Import: split on the first `/` as `series_resource.go` does, set `stream_id`, `metric_id` and `id`.

- [ ] **Step 8: Run the tests, then the suite** — Expected: PASS, PASS.

- [ ] **Step 9: Example and commit**

`examples/resources/datagecko_stream_metric/resource.tf`:
```hcl
resource "datagecko_stream_metric" "revenue_coverage" {
  stream_id = datagecko_stream.revenue.id
  metric_id = datagecko_metric.coverage.id
  params = jsonencode({
    markers = [datagecko_series.deployed.id]
    target  = datagecko_series.delivery.id
  })
}
```

```bash
gofmt -l . && go vet ./... && git add -A internal/provider examples
git commit -m "feat: datagecko_stream_metric — a metric's row on a stream; POST creates, PUT replaces"
```

---

### Task 5: Data source `datagecko_metric_types`

**Files:**
- Modify: `internal/provider/client.go`, `internal/provider/testutil/fake_api.go`
- Create: `internal/provider/metric_types_data_source.go`, `internal/provider/metric_types_data_source_test.go`
- Modify: `internal/provider/provider.go` (`DataSources`)
- Create: `examples/data-sources/datagecko_metric_types/data-source.tf`

**Interfaces:**
- Produces (client):
  ```go
  type MetricType struct {
  	Key          string          `json:"key"`
  	Presentation string          `json:"presentation"`
  	Defaults     json.RawMessage `json:"defaults"`
  	Required     []string        `json:"required"`
  	PerStream    []string        `json:"per_stream"`
  }
  func (c *Client) ListMetricTypes(ctx context.Context) ([]MetricType, error) // GET /metric-types
  ```
- Produces (fake): `GET metric-types` serves `fakeMetricTypes` from Task 3, `freshness` first.

- [ ] **Step 1: Write the failing acceptance test**

```go
func TestAccMetricTypesDataSource_listsEveryType(t *testing.T) {
	fake := testutil.NewFakeAPI()
	defer fake.Close()
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories(),
		Steps: []resource.TestStep{{
			Config: providerBlock(fake.Server.URL) + `data "datagecko_metric_types" "all" {}`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("data.datagecko_metric_types.all", "types.#", "2"),
				resource.TestCheckResourceAttr("data.datagecko_metric_types.all", "types.0.key", "freshness"),
				resource.TestCheckResourceAttr("data.datagecko_metric_types.all", "types.0.defaults", `{"critical_after":7,"warning_after":2}`),
				resource.TestCheckResourceAttr("data.datagecko_metric_types.all", "types.1.per_stream.0", "markers"),
			),
		}},
	})
}
```

- [ ] **Step 2: Run it** — Expected: FAIL, unknown data source.

- [ ] **Step 3: Implement**

Client method and fake route. Data source: schema `types` `ListNestedAttribute` Computed with `key`, `presentation` (String), `defaults` (`computedJSONAttribute`), `required`, `per_stream` (`ListAttribute{ElementType: types.StringType}`); `Read` lists and sets. Register in `DataSources`.

- [ ] **Step 4: Run it, then the suite** — Expected: PASS, PASS.

- [ ] **Step 5: Example and commit**

```hcl
data "datagecko_metric_types" "all" {}

output "freshness_defaults" {
  value = jsondecode([for t in data.datagecko_metric_types.all.types : t.defaults if t.key == "freshness"][0])
}
```

```bash
gofmt -l . && go vet ./... && git add -A internal/provider examples
git commit -m "feat: datagecko_metric_types data source"
```

---

### Task 6: `datagecko_stream_charts`

**Files:**
- Modify: `internal/provider/client.go`, `internal/provider/testutil/fake_api.go`
- Create: `internal/provider/stream_charts_resource.go`, `internal/provider/stream_charts_resource_test.go`
- Modify: `internal/provider/provider.go`
- Create: `examples/resources/datagecko_stream_charts/resource.tf`

**Interfaces:**
- Produces (client):
  ```go
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
  func (c *Client) GetCharts(ctx context.Context, streamID string) ([]Chart, error)                   // GET; decodes {"charts": [...]}, ignores each chart's id
  func (c *Client) ReplaceCharts(ctx context.Context, streamID string, charts []Chart) ([]Chart, error) // PUT {"charts": [...]}
  ```
  `ReplaceCharts` sends `[]` (never `null`) for an empty list and for empty `Series`/`Markers`.
- Produces (fake): `fakeStream.Charts []fakeChart{ID, Title string; Series, Markers []map[string]interface{}}`. `PUT`: 404 for an unknown stream; 400 `payload_refused` `"no series <id> on this stream"` when any entry id is not one of the stream's series; stores entries exactly as given; assigns fresh ids on every `PUT`. `GET`: `{"charts": [...]}` with each chart's `id`; `[]` when none. `DeclareCharts(streamID string, charts []map[string]interface{})` test hook writes a list directly.

- [ ] **Step 1: Write the failing client test** — `TestChartsRoundTripOnlySetStyles`: a stream with two series; `ReplaceCharts` with one chart (`Series`: first with `Width` 2.5 and `Color` `#f59e0b`, second bare; `Markers`: none) → `GetCharts` returns one chart whose first entry has `*Width == 2.5` and second entry has `Width == nil && Color == nil`; `ReplaceCharts` naming a series id of another stream → `*APIError` with status 400 and message containing `no series`.

- [ ] **Step 2: Run it** — Expected: compile error → stubs → FAIL.

- [ ] **Step 3: Implement client and fake.**

- [ ] **Step 4: Run it** — Expected: PASS.

- [ ] **Step 5: Write the failing acceptance tests**

```go
func TestAccStreamChartsResource_createUpdateImport(t *testing.T) {
	// stream revenue; series delivery, baseline (measure), deployed (marker).
	// chart { title = "Delivery vs baseline"
	//   series { id = delivery.id  color = "#f59e0b" width = 2.5 point_radius = 3 }
	//   series { id = baseline.id  dash = "dashed" }
	//   marker { id = deployed.id } }
	// Check: chart.#=1, chart.0.title, chart.0.series.0.width == "2.5", chart.0.series.1.dash == "dashed",
	//        chart.0.marker.0.id set, id == stream_id.
	// Step 2: add a second chart { series { id = baseline.id } } → ResourceActionUpdate, chart.# == "2".
	// Step 3: ImportState by stream_id (ImportStateIdFunc returns attributes["stream_id"]), ImportStateVerify.
}

func TestAccStreamChartsResource_unsetStyleStaysNull(t *testing.T) {
	// one chart, one bare series { id }. One step, no ExpectNonEmptyPlan (post-apply plan must be empty).
	// Check: TestCheckNoResourceAttr for chart.0.series.0.color, .width, .dash, .fill, .point_radius, .point_style.
}

func TestAccStreamChartsResource_existingListIsRefusedOnCreate(t *testing.T) {
	// Step 1: stream + series only. Step 2: PreConfig: fake.DeclareCharts(<stream id from step 1 via a
	// closure over terraform.State captured in step 1's Check>, one chart); config adds the charts
	// resource → ExpectError "declared outside Terraform; import it".
}

func TestAccStreamChartsResource_foreignSeriesIsTheServersError(t *testing.T) {
	// two streams; chart on stream a names stream b's series → ExpectError "no series".
}

func TestAccStreamChartsResource_destroyLeavesTheImplicitChart(t *testing.T) {
	// Step 1: charts declared. Step 2: config without the charts resource → Check via a func that
	// calls fake-independent client? No: assert through the fake's exported Charts(streamID) []... → len 0.
	// Add `func (f *FakeAPI) ChartCount(streamID string) int` for this.
}
```

- [ ] **Step 6: Run them** — Expected: FAIL, unknown resource type.

- [ ] **Step 7: Implement `stream_charts_resource.go`; register it**

Blocks, to match the spec's HCL: `chart` `ListNestedBlock` with attribute `title` (`Optional`, `Computed`, `Default ""`) and nested blocks `series` and `marker` (`ListNestedBlock`). `series` attributes: `id` Required; `color`, `dash`, `point_style` `StringAttribute{Optional}`; `width`, `point_radius` `Float64Attribute{Optional}`; `fill` `BoolAttribute{Optional}` — no defaults, no validators. `marker`: `id` Required, `color` Optional. Top level: `id` Computed `UseStateForUnknown` (= `stream_id`), `stream_id` Required `RequiresReplace`.

Models: `chartsModel{ID, StreamID types.String; Charts []chartModel `tfsdk:"chart"`}`, `chartModel{Title types.String; Series []chartEntryModel `tfsdk:"series"`; Markers []markerModel `tfsdk:"marker"`}`, `chartEntryModel{ID, Color, Dash, PointStyle types.String; Width, PointRadius types.Float64; Fill types.Bool}`, `markerModel{ID, Color types.String}`.

Conversions: `toChartInputs([]chartModel) []Chart` (null → nil pointer; `ValueStringPointer()`/`ValueFloat64Pointer()`/`ValueBoolPointer()`), `toChartModels([]Chart) []chartModel` (nil → `types.StringNull()` etc.). Always produce a non-nil slice for `Charts`, `Series`, `Markers` so an empty list is `[]`, never null (a block list is never null in config).

Create: `GetCharts` first; non-empty → `"Charts already declared"` / `"Stream %s already has %d declared chart(s) — declared outside Terraform; import this resource with the stream id."`; then `ReplaceCharts`. A 404 on the GET → `"Stream not found"`. Read: `GetCharts`; 404 → `RemoveResource`. Update: `ReplaceCharts`. Delete: `ReplaceCharts(nil)`; 404 ignored. Import: passthrough into `stream_id`, and set `id` to the same value.

- [ ] **Step 8: Run the tests, then the suite** — Expected: PASS, PASS.

- [ ] **Step 9: Example and commit**

`examples/resources/datagecko_stream_charts/resource.tf`: the spec §4.4 HCL verbatim, with a comment: "One datagecko_stream_charts per stream. Two on one stream overwrite each other on every apply."

```bash
gofmt -l . && go vet ./... && git add -A internal/provider examples
git commit -m "feat: datagecko_stream_charts — a stream's declared charts, replaced whole"
```

---

### Task 7: `datagecko_stream_sections`

**Files:**
- Modify: `internal/provider/client.go`, `internal/provider/testutil/fake_api.go`
- Create: `internal/provider/stream_sections_resource.go`, `internal/provider/stream_sections_resource_test.go`
- Modify: `internal/provider/provider.go`
- Create: `examples/resources/datagecko_stream_sections/resource.tf`

**Interfaces:**
- Produces (client):
  ```go
  type Section struct {
  	Title string `json:"title"`
  	Body  string `json:"body"`
  }
  func (c *Client) GetSections(ctx context.Context, streamID string) ([]Section, error)                      // {"sections": [...]}
  func (c *Client) ReplaceSections(ctx context.Context, streamID string, sections []Section) ([]Section, error) // PUT {"sections": [...]}, [] never null
  ```
- Produces (fake): `fakeStream.Sections []fakeSection{ID, Title, Body string}`; bodies stored exactly as given; ids assigned on `PUT`; 404 unknown stream; `DeclareSections(streamID string, sections []Section-like maps)` and `SectionCount(streamID) int` hooks.

- [ ] **Step 1: Write the failing client test** — `TestSectionsKeepTheBodyExactly`: `ReplaceSections` with `Body: "line one\n\nline two\n"` → `GetSections()[0].Body` equal byte-for-byte.

- [ ] **Step 2–4:** run (stubs → FAIL), implement, run (PASS).

- [ ] **Step 5: Write the failing acceptance tests**

```go
func TestAccStreamSectionsResource_createUpdateImport(t *testing.T) {
	// section { title = "What this is" body = "Daily revenue by channel." }
	// section { title = "Owner" }           # body defaults to ""
	// Check: section.# == 2, section.1.body == "". Step 2: reorder the two sections → Update, section.0.title == "Owner".
	// Step 3: import by stream_id, verify.
}

func TestAccStreamSectionsResource_bodyRoundTripsExactly(t *testing.T) {
	// body = <<-EOT ... EOT with a trailing newline; one step, empty post-apply plan;
	// Check: section.0.body == "Line one\n\nLine two\n".
}

func TestAccStreamSectionsResource_existingListIsRefusedOnCreate(t *testing.T) { /* as charts */ }
```

- [ ] **Step 6–8:** run (FAIL unknown type), implement `stream_sections_resource.go` (`section` `ListNestedBlock`: `title` Required, `body` Optional + Computed + Default `""`; Create GET-then-refuse, Read, Update, Delete-as-empty, Import by stream id — the same shape as Task 6), register, run tests and suite (PASS).

- [ ] **Step 9: Example and commit**

```hcl
resource "datagecko_stream_sections" "revenue" {
  stream_id = datagecko_stream.revenue.id

  section {
    title = "What this is"
    body  = <<-EOT
      Daily revenue per sales channel, pushed nightly from the warehouse.

      Contact: data team.
    EOT
  }
}
```

```bash
git commit -am "feat: datagecko_stream_sections — a stream's About tab, bodies stored exactly"
```

---

### Task 8: `datagecko_stream_query_panels`

**Files:**
- Modify: `internal/provider/client.go`, `internal/provider/testutil/fake_api.go`
- Create: `internal/provider/stream_query_panels_resource.go`, `internal/provider/stream_query_panels_resource_test.go`
- Modify: `internal/provider/provider.go`
- Create: `examples/resources/datagecko_stream_query_panels/resource.tf`

**Interfaces:**
- Produces (client):
  ```go
  type QueryPanel struct {
  	Title      string          `json:"title"`
  	Action     string          `json:"action"`
  	Parameters json.RawMessage `json:"parameters"`
  }
  func (c *Client) GetQueryPanels(ctx context.Context, streamID string) ([]QueryPanel, error)                        // {"panels": [...]}
  func (c *Client) ReplaceQueryPanels(ctx context.Context, streamID string, panels []QueryPanel) ([]QueryPanel, error) // PUT {"panels": [...]}
  ```
- Produces (fake): `fakeStream.Panels []fakePanel{ID, Title, Action string; Parameters map[string]interface{}}`; `PUT` refuses 400 `payload_refused` `"panel parameters may not carry dt"` when any `parameters` has a `dt` key; ids assigned on `PUT`; 404 unknown stream; `DeclarePanels`, `PanelCount` hooks.

- [ ] **Step 1: Write the failing client test** — `TestQueryPanelsRefuseDt`: a panel with `Parameters: {"dt": "x"}` → `*APIError` 400 whose message contains `dt`; a panel with `{"rows": 10}` round-trips.

- [ ] **Step 2–4:** stubs → FAIL; implement; PASS.

- [ ] **Step 5: Write the failing acceptance tests**

```go
func TestAccStreamQueryPanelsResource_createUpdateImport(t *testing.T) {
	// panel { title = "Top rows" action = "rows" parameters = jsonencode({ limit = 10 }) }
	// panel { title = "Totals"   action = "totals" }   # parameters defaults to {}
	// Check: panel.1.parameters == "{}". Step 2: change limit → Update. Step 3: import by stream_id, verify.
}

func TestAccStreamQueryPanelsResource_dtIsTheServersError(t *testing.T) {
	// parameters = jsonencode({ dt = "2026-01-01" }) → ExpectError "dt".
}

func TestAccStreamQueryPanelsResource_existingListIsRefusedOnCreate(t *testing.T) { /* as charts */ }
```

- [ ] **Step 6–8:** run (FAIL), implement (`panel` block: `title`, `action` Required; `parameters` `jsonAttribute`), register, PASS + suite PASS.

- [ ] **Step 9: Example and commit**

```hcl
resource "datagecko_stream_query_panels" "revenue" {
  stream_id = datagecko_stream.revenue.id

  panel {
    title      = "Top rows"
    action     = datagecko_action.rows.name
    parameters = jsonencode({ limit = 10 }) # `dt` is datagecko's to fill
  }
}
```

```bash
git commit -am "feat: datagecko_stream_query_panels — a stream's panels, each a pointer to an action"
```

---

### Task 9: `datagecko_action`

**Files:**
- Modify: `internal/provider/client.go`, `internal/provider/testutil/fake_api.go`
- Create: `internal/provider/action_resource.go`, `internal/provider/action_resource_test.go`
- Modify: `internal/provider/provider.go`
- Create: `examples/resources/datagecko_action/resource.tf`

**Interfaces:**
- Produces (client):
  ```go
  type Action struct {
  	ID               string          `json:"id"`
  	Name             string          `json:"name"`
  	Description      string          `json:"description"`
  	ParametersSchema json.RawMessage `json:"parameters_schema"`
  	ExpiresAfter     int64           `json:"expires_after"`
  	ResultMaxBytes   int64           `json:"result_max_bytes"`
  	RequestRole      string          `json:"request_role"`
  }
  // ActionInput: a nil pointer or nil bytes means "not sent" — the API then stores its default.
  type ActionInput struct {
  	Description      *string
  	ParametersSchema json.RawMessage
  	ExpiresAfter     *int64
  	ResultMaxBytes   *int64
  	RequestRole      *string
  }
  func (c *Client) ListActions(ctx context.Context) ([]Action, error)                                   // GET /actions → {"actions": [...]}
  func (c *Client) FindAction(ctx context.Context, name string) (*Action, error)                        // nil, nil when absent
  func (c *Client) CreateAction(ctx context.Context, name string, in ActionInput) (*Action, error)      // POST /actions
  func (c *Client) ReplaceAction(ctx context.Context, name string, in ActionInput) (*Action, error)     // PUT /actions/{name}
  func (c *Client) DeleteAction(ctx context.Context, name string) error                                 // DELETE; 409 while a run is in flight
  ```
- Produces (fake): `actions map[name]*fakeAction{ID, Name, Description string; ParametersSchema map[string]interface{}; ExpiresAfter, ResultMaxBytes int64; RequestRole string; RunInFlight bool}`. Defaults when a field is absent: `description ""`, `parameters_schema {}`, `expires_after 10`, `result_max_bytes 1048576`, `request_role "member"`. `POST`: 409 `already_exists` when the name exists; 400 `payload_refused` `"expires_after must be at most 3600"` above the cap (the one server rule the tests surface). `PUT`: 404 when missing; every omitted field returns to its default. `DELETE`: 404 when missing; 409 `conflict` `"the action has runs in flight; cancel them or wait"` when `RunInFlight`. Route order: `actions/runners` (Task 10) is matched before `actions/{name}`. Hooks: `SetRunInFlight(name string)`, `DeleteAllActions()`.

- [ ] **Step 1: Write the failing client test** — `TestActionLifecycleAgainstTheFake`: `CreateAction("rows", ActionInput{ParametersSchema: {"properties":{"dt":{"type":"string"}}}})` → `ExpiresAfter == 10`, `RequestRole == "member"`; a second create → `IsAlreadyExists`; `ReplaceAction("rows", ActionInput{ExpiresAfter: &120})` → `ExpiresAfter == 120` and `ParametersSchema` equals `{}` (omitted field reset); `ReplaceAction("nope", …)` → `IsNotFound`; `CreateAction("slow", {ExpiresAfter: &7200})` → `*APIError` 400 whose message contains `3600`; `fake.SetRunInFlight("rows")`; `DeleteAction("rows")` → `IsAlreadyExists()` (409).

- [ ] **Step 2–4:** stubs → FAIL; implement; PASS.

- [ ] **Step 5: Write the failing acceptance tests**

```go
func TestAccActionResource_createUpdateImport(t *testing.T) {
	// resource "datagecko_action" "rows" {
	//   name              = "rows"
	//   description       = "Top rows of the day"
	//   parameters_schema = jsonencode({ properties = { dt = { type = "string" }, limit = { type = "integer" } } })
	//   expires_after     = 120
	// }
	// Check: id set; expires_after "120"; result_max_bytes "1048576" (server default read back); request_role "member".
	// Step 2: description changed, expires_after removed from config → ResourceActionUpdate; expires_after
	//         stays "120" (Optional+Computed keeps the prior value when the line is dropped — the spec's
	//         "reads back without a diff"; the PUT still sends it, from the plan).
	// Step 3: import by name (ImportStateId "rows"), ImportStateVerify.
}

func TestAccActionResource_serverDefaultsReadBackWithoutADiff(t *testing.T) {
	// name only; one step, empty post-apply plan; Check: expires_after "10", result_max_bytes "1048576",
	// request_role "member", parameters_schema "{}", description "".
}

func TestAccActionResource_renameReplaces(t *testing.T) {
	// name rows → top_rows: plancheck.ResourceActionReplace; after apply fake has no "rows".
}

func TestAccActionResource_overCapIsTheServersError(t *testing.T) {
	// expires_after = 7200 → ExpectError "3600".
}

func TestAccActionResource_existingNameIsRefusedOnCreate(t *testing.T) {
	// two resources named "rows" → ExpectError "declared outside Terraform; import it".
}

func TestAccActionResource_deleteWhileARunIsInFlightSurfacesTheConflict(t *testing.T) {
	// Step 1: create. Step 2: PreConfig fake.SetRunInFlight("rows"); Config without the resource;
	// ExpectError "runs in flight".
}
```

- [ ] **Step 6: Run them** — Expected: FAIL, unknown resource type.

- [ ] **Step 7: Implement `action_resource.go`; register it**

Model: `ID, Name, Description, RequestRole types.String; ParametersSchema jsontypes.Normalized; ExpiresAfter, ResultMaxBytes types.Int64`. Schema: `id` Computed `UseStateForUnknown`; `name` Required `RequiresReplace`; `description` Optional + Computed + Default `""`; `parameters_schema` `jsonAttribute`; `expires_after`, `result_max_bytes` `Int64Attribute{Optional, Computed, PlanModifiers: UseStateForUnknown}`; `request_role` `StringAttribute{Optional, Computed, PlanModifiers: UseStateForUnknown}`.

`inputOf(plan actionModel) ActionInput`: description and schema always; each of the three Optional+Computed fields only when `!IsNull() && !IsUnknown()`. Create: `CreateAction(plan.Name, inputOf(plan))`; 409 → `"Action already exists"` / `"An action named %q is declared outside Terraform; import it."`. Read: `FindAction(state.Name)`; nil → `RemoveResource`. Update: `ReplaceAction(state.Name, inputOf(plan))`; 404 → error `"Action is gone"`. Delete: `DeleteAction`; 404 ignored; 409 → `"Action has runs in flight"` with the server's message. Import: passthrough into `name`. `toActionModel(a *Action) actionModel`.

- [ ] **Step 8: Run the tests, then the suite** — Expected: PASS, PASS.

- [ ] **Step 9: Example and commit**

```hcl
resource "datagecko_action" "rows" {
  name        = "rows"
  description = "Top rows of the day"
  parameters_schema = jsonencode({
    properties = {
      dt    = { type = "string" }
      limit = { type = "integer" }
    }
  })
  expires_after = 120 # seconds a run may wait for a runner; at most 3600
}
```

```bash
git commit -am "feat: datagecko_action — POST creates, PUT replaces whole, a rename is a new action"
```

---

### Task 10: `datagecko_runner_hook`

**Files:**
- Modify: `internal/provider/client.go`, `internal/provider/testutil/fake_api.go`
- Create: `internal/provider/runner_hook_resource.go`, `internal/provider/runner_hook_resource_test.go`
- Modify: `internal/provider/provider.go`
- Create: `examples/resources/datagecko_runner_hook/resource.tf`

**Interfaces:**
- Produces (client):
  ```go
  type RunnerHook struct {
  	URL            string  `json:"url"`
  	LastRingAt     *string `json:"last_ring_at"`
  	LastRingStatus string  `json:"last_ring_status"`
  }
  type Runner struct {
  	Prefix string      `json:"prefix"`
  	Name   string      `json:"name"`
  	Hook   *RunnerHook `json:"hook"`
  }
  type HookSecret struct {
  	URL    string `json:"url"`
  	Secret string `json:"secret"`
  }
  func (c *Client) ListRunners(ctx context.Context) ([]Runner, error)                              // GET /actions/runners → {"runners": [...]}
  func (c *Client) FindRunner(ctx context.Context, prefix string) (*Runner, error)                 // nil, nil when absent
  func (c *Client) CreateHook(ctx context.Context, prefix, url string) (*HookSecret, error)        // POST /keys/{prefix}/hook
  func (c *Client) ReplaceHook(ctx context.Context, prefix, url string) (*HookSecret, error)       // PUT — rotates the secret
  func (c *Client) DeleteHook(ctx context.Context, prefix string) error                            // DELETE
  ```
- Produces (fake): `fakeKey.Hook *fakeHook{URL, Secret string}`. `POST`: 404 when the key is missing or revoked; 400 `payload_refused` `"hook url must be https"` unless the URL starts with `https://`; 409 `already_exists` when a hook exists; 201 `{url, secret}` with a fresh secret (`prefix + ".hook." + counter`). `PUT`: 404 when no hook; 200 with a new secret. `DELETE`: 404 when no hook. `GET actions/runners`: every live ingest key as `{prefix, name, last_claim_at: null, in_flight: 0, hook: null | {url, last_ring_at: null, last_ring_status: ""}}`.

- [ ] **Step 1: Write the failing client test** — `TestHookLifecycleAgainstTheFake`: create an ingest key; `CreateHook(prefix, "https://runner.example/ring")` → non-empty `Secret`; a second `CreateHook` → `IsAlreadyExists`; `ReplaceHook` → a different `Secret`; `FindRunner(prefix).Hook.URL` equals the new URL; `CreateHook(prefix2, "http://…")` on another key → 400; `DeleteHook` then `FindRunner(prefix).Hook == nil`.

- [ ] **Step 2–4:** stubs → FAIL; implement; PASS.

- [ ] **Step 5: Write the failing acceptance tests**

```go
func TestAccRunnerHookResource_createImport(t *testing.T) {
	// datagecko_ingest_key "runner" { name = "runner" }
	// datagecko_runner_hook "runner" { key_prefix = datagecko_ingest_key.runner.id  url = "https://runner.example/ring" }
	// Check: secret set, url, last_ring_status "", id == key_prefix.
	// Step 2: import by key prefix, ImportStateVerify, ImportStateVerifyIgnore: []string{"secret"} (never read back).
}

func TestAccRunnerHookResource_urlChangeRotatesTheSecret(t *testing.T) {
	// Step 1: create; capture secret in a closure. Step 2: url changed →
	//   ConfigPlanChecks PreApply: plancheck.ExpectUnknownValue("datagecko_runner_hook.runner", tfjsonpath.New("secret")),
	//   ResourceActionUpdate; Check: secret != captured.
	// Step 3: same config again → empty plan (the harness checks), secret unchanged.
}

func TestAccRunnerHookResource_existingHookIsRefusedOnCreate(t *testing.T) {
	// two hooks on one key → ExpectError "declared outside Terraform; import it".
}

func TestAccRunnerHookResource_httpIsTheServersError(t *testing.T) {
	// url = "http://…" → ExpectError "https".
}
```

- [ ] **Step 6: Run them** — Expected: FAIL, unknown resource type.

- [ ] **Step 7: Implement `runner_hook_resource.go`; register it**

Model: `ID, KeyPrefix, URL, Secret, LastRingAt, LastRingStatus types.String`. Schema: `id` Computed `UseStateForUnknown` (= `key_prefix`); `key_prefix` Required `RequiresReplace`; `url` Required; `secret` Computed + Sensitive with the plan modifier below; `last_ring_at`, `last_ring_status` Computed.

```go
// secretRotatesWithURL keeps the stored secret in the plan while url is unchanged and
// leaves it unknown when url changes: a PUT always issues a new one.
type secretRotatesWithURL struct{}

func (secretRotatesWithURL) PlanModifyString(ctx context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	if req.State.Raw.IsNull() {
		return // create: the secret is unknown until the API shows it
	}
	var planURL, stateURL types.String
	req.Plan.GetAttribute(ctx, path.Root("url"), &planURL)
	req.State.GetAttribute(ctx, path.Root("url"), &stateURL)
	if planURL.Equal(stateURL) {
		resp.PlanValue = req.StateValue
	}
}
```
(`Description`/`MarkdownDescription` methods as the interface requires.)

Create: `CreateHook`; 409 → `"Hook already set"` / `"Key %s already has a hook — declared outside Terraform; import it with the key prefix (the secret cannot be recovered)."`; 404 → `"Ingest key not found"`. Then `FindRunner` for `last_ring_*` (both empty right after a create — set `""` without the call; one request fewer). Read: `FindRunner`; nil or `Hook == nil` → `RemoveResource`; else set `url`, `last_ring_at` (`optionalString`), `last_ring_status`; keep `secret` from state. Update: `ReplaceHook`; set `secret` from the response. Delete: 404 ignored. Import: passthrough into `key_prefix`, `id` the same; `secret` stays null and the attribute description says so.

- [ ] **Step 8: Run the tests, then the suite** — Expected: PASS, PASS.

- [ ] **Step 9: Example and commit**

```hcl
resource "datagecko_ingest_key" "runner" {
  name = "runner"
}

resource "datagecko_runner_hook" "runner" {
  key_prefix = datagecko_ingest_key.runner.id
  url        = "https://runner.example.com/datagecko/ring" # https only; never put a secret in it, it is logged
}

# datagecko_runner_hook.runner.secret signs every ring; changing url issues a new one.
```

```bash
git commit -am "feat: datagecko_runner_hook — a runner's hook; the secret rotates with the url"
```

---

### Task 11: Provider description, README, CLAUDE.md, final checks

**Files:**
- Modify: `internal/provider/provider.go:42`, `README.md:1-3`, `CLAUDE.md` (Architecture block)

- [ ] **Step 1: Wording**

`provider.go` description: `"Manages datagecko streams, series, metrics, charts, sections, query panels, actions, runner hooks and ingest keys for one organization."`

`README.md` first paragraph: `Manages [datagecko](https://datagecko.ai) streams, series, metrics and their stream rows, charts, sections, query panels, actions, runner hooks and ingest keys for one organization, authenticated by that organization's admin key. One ` + "`datagecko_stream_charts`" + `, ` + "`_sections`" + ` or ` + "`_query_panels`" + ` resource per stream: two on one stream overwrite each other on every apply.` Add a line: `JSON attributes (params, parameters, parameters_schema) are compared as JSON values, so key order and whitespace never show as a diff; write them with jsonencode().`

`CLAUDE.md` Architecture block: list `json_attrs.go`, `metric_resource.go`, `stream_metric_resource.go`, `metric_types_data_source.go`, `stream_charts_resource.go`, `stream_sections_resource.go`, `stream_query_panels_resource.go`, `action_resource.go`, `runner_hook_resource.go`, and `helpers_test.go`; one line under the client rule: "JSON attributes travel as `json.RawMessage` and live in state as `jsontypes.Normalized` — never re-marshalled through `float64`, never compared as strings."

- [ ] **Step 2: Final checks**

Run: `gofmt -l . ; go vet ./... ; go build -o terraform-provider-datagecko . ; go test ./... -count=1`
Expected: empty, clean, built, PASS.

- [ ] **Step 3: Commit**

```bash
git add -A
git commit -m "docs: the provider covers the full admin surface"
```

---

## Self-review

**Spec coverage.** §4.1 → Task 2. §4.2 → Task 3. §4.3 → Task 4. §4.4 → Task 6. §4.5 → Task 7. §4.6 → Task 8. §4.7 → Task 9 (§4.7.1 is merged in the API). §4.8 → Task 10. §4.9 → Task 5. §4.10 → the 409/404 branches in Tasks 4, 9, 10. §5 retry loop → Task 1; raw-bytes JSON → `json_attrs.go` (Task 3) and every `json.RawMessage` field; float vs int → Task 6's Float64 attributes; examples, README, CLAUDE.md → per task and Task 11. §2 "one HTTP client, one fake" → every task's client and fake steps. Non-goals: nothing in the plan touches a listed endpoint. Gap found and closed: the spec's order puts 4.9 after 4.3 — kept (Task 5).

**Deviations, stated.** Every resource has a Computed `id` (Global Constraints, last bullet); the spec's stream-metric "no id of its own" becomes "an id that is the import identifier". `effective` carries no `UseStateForUnknown` so an update plan shows it as unknown — the alternative is an "inconsistent result after apply" error whenever params change. Dropping `expires_after` from an action's config keeps the stored value rather than resetting it (Optional + Computed); the spec chose Optional + Computed for exactly the read-back-without-diff property, and the PUT still sends every known planned field.

**Type consistency.** `providerBlock`, `seriesImportID`, `normalized`, `rawJSON`, `jsonAttribute`, `computedJSONAttribute`, `optionalString` (existing) are each defined once and used by name afterwards; client method names match between Interfaces blocks and test steps.

**Proportion.** Tasks 7, 8 and the later acceptance tests are sketched by assertion rather than written out; the first full test in each file is the template. The plan is about the spec's length.

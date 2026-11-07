# terraform-provider-datagecko: the full admin surface

**Date:** 2026-11-07
**Status:** design, awaiting approval
**Builds on:** the provider as of `2fd487d` (streams, series, ingest keys)

## 1. Intent

Everything an admin key can declare on datagecko becomes declarable in
Terraform, so one `terraform apply` sets up an organization end to end:
streams and their series, the metrics that judge them, how each stream is
drawn and described, the actions a runner answers, the panels that call
them, and the runner's hook. Today the provider stops at streams, series
and ingest keys; the API has since grown metrics, charts, sections,
actions, query panels, runner hooks, and two series fields, and the Grips
connector cannot declare any of it.

Nothing is in production. Breaking changes to the existing three resources
are allowed; none is needed.

## 2. Goals, non-goals, anti-goals

**Goals**

- Every `PUT`/`POST`/`PATCH`/`DELETE` an admin key may make under `/api/v1/`
  has a resource, and every resource reads itself back on refresh.
- A plan after an apply is empty. The API stores what it is given and reads
  back only what was set, so the provider must never normalise, default in,
  or reorder on the way through.
- One HTTP client, one fake: every new call lands in `client.go`, every new
  endpoint in `testutil/fake_api.go`, and every resource is tested through
  real plan/apply/destroy cycles against the fake, as the existing three are.

**Non-goals** — the API surface Terraform will not cover, decided
2026-11-07. The rule: a declaration is a resource; anything a program
does at runtime, or a person does once, is not.

| Endpoint | Caller | Why not |
|---|---|---|
| `POST /points` | ingest key | pushing data |
| `POST /actions/claim`, `POST /actions/runs/{id}/claim` | runner | claiming work |
| `POST /actions/runs/{id}/heartbeat`, `.../result` | runner | lease, answer |
| `POST /actions/{name}/runs` | admin key | *requesting* a run is an event |
| `GET /actions/runs`, `GET /actions/runs/{id}`, `POST .../cancel` | admin key | runs are events, not state |
| `POST /actions/runners/{prefix}/ring` | admin key | a one-off test |
| `GET /actions/runners` | admin key | operational view; `datagecko_runner_hook` reads it, nothing declares it |
| `GET /status`, `GET /tags` | admin key | operational and lookup views; tags are written on streams |
| `GET /streams/{id}/series?from&to` | admin key | reading points |
| admin keys | — | the API cannot create one; an owner does, in the SPA or the CLI |
| `/schema/`, `/docs/` | anyone | documentation |

Also out: organization-level things the API does not expose (quota,
members).

**Anti-goals**

- A resource whose refresh shows a diff the user did not write. If a shape
  cannot round-trip cleanly, it is modelled differently, not papered over
  with `ignore_changes` advice.
- Hiding a server-side rule. If the API refuses something (a foreign series
  on a chart, `dt` in panel parameters), the provider surfaces that error
  verbatim rather than pre-validating a copy of the rule that will drift.

## 3. Constraints

- `terraform-plugin-framework`; free-form JSON attributes use
  `jsontypes.Normalized` from `terraform-plugin-framework-jsontypes`, so a
  plan compares JSON semantically (key order and whitespace never diff) and
  the user writes `jsonencode({...})`.
- The API reads some things back only through a list (`GET /actions`,
  `GET /actions/runners`, `GET /keys`, `GET /streams`). A resource read that
  way finds its row by key and removes itself from state when it is absent,
  as `datagecko_stream` does today.
- Replace-whole lists (charts, sections, panels) are one resource per
  stream. Two of them on one stream would overwrite each other on every
  apply; that is a user error the docs name, not something the provider
  detects.
- A secret the API shows once (`hook` secret, like the ingest key's) is
  `Computed` + `Sensitive`, set on create, never read back, and `Unknown`
  on any update that rotates it.

## 4. The resources

### 4.1 `datagecko_series` — two new attributes

| attribute | | |
|---|---|---|
| `kind` | Optional, default `"measure"`, `measure` or `marker` | **RequiresReplace** — the API never changes a kind |
| `interval_days` | Optional Int64, 1–366, null = no cadence | in-place; `PATCH {"interval_days": null}` clears it |

Read-back: the `GET /streams` list, which `datagecko_series.Read` already
uses, carries both on each series.

### 4.2 `datagecko_metric`

An instance of a metric type: `POST /metrics`, `GET`/`PATCH`/`DELETE
/metrics/{id}`.

| attribute | | |
|---|---|---|
| `id` | Computed | |
| `type` | Required | **RequiresReplace** (`PATCH` refuses a type change) |
| `name` | Required, ≤ 64 | in-place; unique per organization, case-insensitive (409 surfaced) |
| `params` | Optional JSON (`Normalized`), default `{}` | in-place; `PATCH` replaces whole — exactly the resource semantics |
| `effective` | Computed JSON | defaults under params, read-only |

Import by id.

### 4.3 `datagecko_stream_metric`

A stream's row for an instance: `POST /streams/{stream_id}/metrics` to
create, `GET`/`PUT`/`DELETE /streams/{stream_id}/metrics/{metric_id}` after
(the `POST` is new — §4.10).

| attribute | | |
|---|---|---|
| `stream_id` | Required | RequiresReplace |
| `metric_id` | Required | RequiresReplace |
| `params` | Optional JSON, default `{}` | in-place via `PUT`; holds the per-stream params (coverage's `markers`, a list of series ids, and `target`) |
| `effective` | Computed JSON | |

No `id` of its own; the pair is the identity. Import as `stream_id/metric_id`.

Create is `POST {"metric_id": ..., "params": {...}}`; a row that already
exists is a 409, which the provider surfaces as "declared outside
Terraform; import it". Update is `PUT`, a 404 when the row is gone.

### 4.4 `datagecko_stream_charts`

The stream's declared charts, whole: `GET`/`PUT /streams/{id}/charts`.

```hcl
resource "datagecko_stream_charts" "revenue" {
  stream_id = datagecko_stream.revenue.id

  chart {
    title = "Delivery vs baseline"
    series { id = datagecko_series.delivery.id  color = "#f59e0b" width = 2.5 point_radius = 3 }
    series { id = datagecko_series.baseline.id  color = "#64748b" dash = "dashed" }
    marker { id = datagecko_series.deployed.id }
  }
}
```

| | |
|---|---|
| `stream_id` | Required, RequiresReplace |
| `chart` | list of nested blocks, ordered, ≤ 20 (the API's `MAX_CHARTS_PER_STREAM`) |
| `chart.title` | Optional string, default `""` |
| `chart.series` | ordered list ≥ 1: `id` Required; `color`, `width` (Float64 0–8), `dash` (`solid`/`dashed`/`dotted`), `fill` (Bool), `point_radius` (0–8), `point_style` (`circle`/`rect`/`triangle`/`cross`/`star`), all Optional, **no defaults** |
| `chart.marker` | ordered list: `id` Required, `color` Optional |

Style fields carry no default because the API reads back only what was
set: a field the user never wrote comes back absent, and absent ⇔ null is
the clean round-trip. Validation of bounds and vocabularies stays
server-side (anti-goal 2); the provider declares types only.

The API assigns chart ids afresh on every `PUT`; they are not exposed —
a computed value that changes every apply is noise.

Delete is `PUT {"charts": []}`: the stream falls back to its implicit chart.
Import by `stream_id`.

The list always exists, empty or not, so the API has no "already exists"
to give. The provider's Create therefore `GET`s first and refuses when the
list is not empty ("declared outside Terraform; import it") — one extra
request on create only, never on refresh. The same holds for 4.5 and 4.6.

### 4.5 `datagecko_stream_sections`

Same shape, `GET`/`PUT /streams/{id}/sections`:

| | |
|---|---|
| `stream_id` | Required, RequiresReplace |
| `section` | ordered list ≤ 20: `title` Required ≤ 128, `body` Optional string (Markdown, ≤ 20,000), default `""` |

Bodies are stored exactly as given — no trimming, so a heredoc's trailing
newline is part of the body and round-trips as such. Delete is an empty
list. Import by `stream_id`.

### 4.6 `datagecko_stream_query_panels`

`GET`/`PUT /streams/{id}/query-panels`:

| | |
|---|---|
| `stream_id` | Required, RequiresReplace |
| `panel` | ordered list ≤ 8: `title` Required, `action` Required (a name; existence not checked by the API, and not by the provider either — a panel is a pointer), `parameters` Optional JSON, default `{}` |

Delete is an empty list. Import by `stream_id`.

### 4.7 `datagecko_action`

`POST /actions` to create, `PUT /actions/{name}` to replace, `GET /actions`
(list), `DELETE /actions/{name}` (the `POST` and the keyed `PUT` are new —
§4.10; today's `PUT /actions` upsert goes away).

| attribute | | |
|---|---|---|
| `id` | Computed | |
| `name` | Required | **RequiresReplace** — the name is the key; a rename is a new action, and the old one is deleted |
| `description` | Optional, default `""` | |
| `parameters_schema` | Optional JSON, default `{}` | stored exactly as given — after the API change in §4.7.1 |
| `expires_after` | Optional Int64 seconds | server default when omitted; Computed+Optional so the default reads back without a diff |
| `result_max_bytes` | Optional Int64 | same |
| `request_role` | Optional string | same |

`PUT /actions/{name}` is a full replace: an omitted field resets to its
default, so Update always sends the planned value of all five fields.

Create is one `POST`; an existing name is a 409, surfaced as "import it".
Import by name. `DELETE /actions/{name}` is a 409 while a run is in flight;
a rename (replace) surfaces that verbatim.

#### 4.7.1 Prerequisite in the API: refuse, never rewrite

(One half of the API prerequisite; the other half, create-never-adopt, is
§4.10. One `datagecko.ai` branch and PR covers both.)

Today `actions/services/catalog.py` rewrites three inputs instead of
refusing them: `parameters_schema` is stored with `type: "object"` and
`additionalProperties: false` injected (overriding an explicit `true`), and
`expires_after` above 3600 s and `result_max_bytes` above 4 MiB are capped
with `min()`. Each is a perpetual plan diff: the user writes `7200`, reads
back `3600`, and every plan from then on "changes" it back. No provider
rule can fix that without copying the server's numbers.

Decided (2026-11-07): the API changes, before the provider work, so the
provider needs nothing special here.

- `expires_after` and `result_max_bytes` above their ceilings are a 400
  naming the ceiling, like every other bound in the API.
- `parameters_schema` is stored exactly as given. The closed-schema rule
  (`type: object`, `additionalProperties: false`) is applied when a run's
  parameters are validated against it, not baked into the stored copy; an
  explicit `additionalProperties: true` at the top level is a 400, since the
  rule is that an action's schema is closed. Read-back is the same JSON value
  as the write — not the same bytes: the column is `jsonb`, which reorders
  object keys — so the provider compares JSON attributes by value
  (`jsontypes.Normalized`, §5), never as strings.
- The API's own tests: a `PUT` with an over-cap value is refused; a schema
  reads back as the same JSON value; a run against a verbatim schema still refuses an
  undeclared parameter. `docs/integration.md` §10 says the schema is closed
  *when checked* rather than *as stored*.

This is a `datagecko.ai` change, test-first, its own branch and PR.

### 4.8 `datagecko_runner_hook`

A hook on an ingest key: `POST /keys/{prefix}/hook` to set one, `PUT` to
change it, `DELETE` to clear it (the `POST` is new — §4.10), read from
`GET /actions/runners`.

| attribute | | |
|---|---|---|
| `key_prefix` | Required | RequiresReplace |
| `url` | Required, ≤ 500, `https` | in-place `PUT` — which **rotates the secret** |
| `secret` | Computed, Sensitive | set on create and on every `url` change; never read back; `Unknown` in the plan whenever `url` changes |
| `last_ring_at`, `last_ring_status` | Computed | from the runners list |

Why its own resource and not `hook_url` on `datagecko_ingest_key`: the hook
has its own delete, its own secret with its own rotation, and reads from a
different endpoint than the key. On `datagecko_ingest_key` it would make
that resource's refresh a two-call join and would put a second
show-once secret on a resource that already has one. One ingest key, one
hook, so `key_prefix` is the identity; import by prefix recovers everything
but the secret, which the import says.

Create is `POST`; a key that already has a hook is a 409, surfaced as
"import it". This is the case where silent adoption would hurt most: a
`PUT` on a key with a working hook replaces its URL and rotates its secret,
and the runner that held the old secret stops being able to verify rings.

### 4.9 Data source `datagecko_metric_types`

`GET /metric-types`, read-only: a list of `{key, presentation, defaults
(JSON), required (list), per_stream (list)}`. Lets a module look up a
type's defaults instead of copying them. No `tags` data source: tags are
written on streams, and there is nothing to look up.

### 4.10 Prerequisite in the API: create, never adopt

Three endpoints are upserts: `PUT /actions`, `PUT
/streams/{id}/metrics/{metric_id}` and `PUT /keys/{prefix}/hook`. A
Terraform Create against an upsert silently takes over whatever was there —
for a hook, rotating a working runner's secret. The only provider-side
remedy is a read before every create, which races with itself and copies
the API's notion of "exists".

Decided (2026-11-07): each gets a `POST` that creates and a keyed `PUT`
that replaces, the shape streams, series, keys and metrics already have.
Nothing is in production; the upserts go away.

| | create | replace |
|---|---|---|
| actions | `POST /actions` → 201; 409 when the name exists | `PUT /actions/{name}` → 200; 404 when it does not |
| stream metric rows | `POST /streams/{id}/metrics` `{"metric_id", "params"}` → 201; 409 when the row exists | `PUT /streams/{id}/metrics/{metric_id}` → 200; 404 when it does not |
| hooks | `POST /keys/{prefix}/hook` `{"url"}` → 201 with the secret; 409 when the key has one | `PUT /keys/{prefix}/hook` → 200 with a new secret; 404 when it has none |

Each 409 and 404 is the API's one error shape, and a foreign stream or key
stays the same 404 as a missing one. The API's own tests cover both
branches per endpoint; `docs/integration.md` §§9–10 and 12 change
accordingly.

The three replace-whole lists (4.4–4.6) are not upserts in this sense — an
empty list is a valid state, not an absence — so they keep `PUT` and the
provider refuses a non-empty list on create.

## 5. Cross-cutting

- **Client.** One method per endpoint in `client.go`, same error-shape
  decoding as the existing calls. Retry-on-429 changes from one retry to a
  loop that honours `Retry-After` (the API answers with the whole window,
  60 s), up to three windows: an admin key has 120 requests a minute, there
  is no `GET /streams/{id}`, and a fully declared organisation of a few
  dozen streams refreshes with well over 120 calls at Terraform's default
  parallelism. No memo of list responses across a plan: a stale read inside
  an apply is worse than a slow one. The client forwards JSON attributes as
  raw bytes, never re-marshalled through `float64`, because the server's
  param validation is exact-typed (`2.0` is refused where an int is
  expected). The fake grows an
  in-memory table per endpoint, with the server's rules that matter to a
  plan: 409 on a duplicate metric name, 404 on a foreign stream, ids
  assigned on `PUT` for charts/sections/panels.
- **JSON attributes** (`params`, `effective`, `parameters`,
  `parameters_schema`, `defaults`): `jsontypes.Normalized` everywhere. No
  `types.Dynamic`; nested-object schemas for `params` would need one schema
  per metric type and would drift.
- **Float vs int.** Chart `width` and `point_radius` are Float64 in the
  schema; the API accepts either. `2` and `2.0` compare equal in a Float64
  attribute, so a user writing `width = 2` sees no diff.
- **Existing resources** are untouched except 4.1.
- **Examples and docs.** One example per new resource under
  `examples/resources/`; `README.md`'s first line lists the resources;
  `CLAUDE.md`'s architecture block lists the new files.
- **Order of work** (so each step is shippable): 4.7.1 + 4.10 in the API
  first, one PR →
  4.1 → 4.2 + 4.3 + 4.9 → 4.4 → 4.5 → 4.6 → 4.7 → 4.8. Metrics first because they are what the Grips
  overview needs; charts next because the Grips streams have two series
  each and the implicit chart already draws them, so it can wait.

## 6. Left to the builder

- File layout: one `<resource>_resource.go` + test per resource, matching
  the existing three.
- Whether a nested-block list is `ListNestedBlock` or `ListNestedAttribute`;
  the framework's current guidance (nested attributes) is fine unless the
  HCL in 4.4 reads worse for it.
- Fake-API fidelity beyond what a plan needs.

## 7. Out of scope, recorded

- The Grips connector's own Terraform gains a `datagecko_metric` (freshness
  on `delivery`) and a `datagecko_stream_metric` per stream once this ships;
  that is its change, not the provider's.
- Publishing to the Registry stays a separate decision.

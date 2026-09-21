# terraform-provider-datagecko — design

**Date:** 2026-09-21
**Depends on:** `2026-09-21-stream-and-ingest-key-rename-design.md` in `datagecko.ai` — `PATCH /streams/{id}` accepting `name` and the new `PATCH /keys/{prefix}` are both assumed to exist; without them, `datagecko_stream.name` and `datagecko_ingest_key.name` would be forced to `RequiresReplace` instead of updating in place.

## 1. Motivation

datagecko's `/api/v1/` is admin-key-authenticated and lets a person or a script declare streams, their series, and ingest keys — exactly the shape of infrastructure a team wants to keep in version control rather than click or curl by hand. Terraform is that version control for infrastructure; this project is the provider that lets a `.tf` file be the source of truth for an organization's streams, series, and ingest keys.

## 2. Decisions

| Decision | Choice | Rejected |
|---|---|---|
| Organization lifecycle | **Out of scope entirely.** An admin key already belongs to one organization — there is no REST endpoint to create one, and creating one is deliberately CLI/owner-gated (`manage.py create_org`, run via `heroku run`, not HTTP) | A data source to reference an existing org; shelling out to `heroku run` from Terraform to create one — both add scope the API's own design refuses to expose over HTTP |
| Provider vs. module | **A real Terraform provider** (Go, `terraform-plugin-framework`), giving typed resources (`datagecko_stream`, `datagecko_series`, `datagecko_ingest_key`) | A `.tf` module wrapping the community `terraform-provider-restapi` over raw JSON — weak typing, no native plan/diff for nested resources, no import |
| Provider scope | **One provider instance manages one organization** — an admin key *is* an org's credential. Multiple orgs in one config use `provider "datagecko" { alias = ... }` per admin key | A provider that takes an org identifier and juggles multiple admin keys internally — the API gives no way to address "an org" except by holding its key |
| Credentials | **Provider block attribute, falling back to `DATAGECKO_ADMIN_KEY` env var** | Env var only — loses the ability to alias multiple orgs cleanly in one config |
| Repo location | **Sibling to `datagecko.ai`**, own Go module, own git history, own release lifecycle | Living inside `datagecko.ai` — this is a different language, a different toolchain, and a different consumer (someone writing `.tf`, not Django/Vue) |
| Testing | **Mocked HTTP only** (`httptest.Server` fake + `terraform-plugin-testing`'s `resource.UnitTest`) | Acceptance tests against a real local datagecko (docker compose + `manage.py runserver`) — would exercise the real contract but adds a second stack's worth of setup to this project |
| Distribution | **Local only** — `go build` + `~/.terraformrc` `dev_overrides`, no registry | Publishing to the Terraform Registry — needs GPG signing, GoReleaser, and a release pipeline this project doesn't need yet |

## 3. Repo layout

```
terraform-provider-datagecko/
  go.mod
  main.go                      # provider server entrypoint
  internal/
    provider/
      provider.go               # schema, configure (endpoint + admin_key)
      client.go                 # thin HTTP client: auth header, error-shape decoding, retry-on-429
      stream_resource.go
      series_resource.go
      ingest_key_resource.go
      *_test.go                 # unit tests against the httptest fake per resource
      testutil/
        fake_api.go              # the httptest.Server double implementing the /api/v1/ subset below
  examples/
    provider/
      provider.tf
    resources/
      datagecko_stream/
      datagecko_series/
      datagecko_ingest_key/
  docs/                         # tfplugindocs-generated, committed like schema.graphql is in datagecko.ai
```

## 4. Provider configuration

```hcl
provider "datagecko" {
  endpoint  = "https://datagecko.ai/api/v1/"   # optional; this is the default
  admin_key = var.datagecko_admin_key           # optional; falls back to DATAGECKO_ADMIN_KEY
}
```

`Configure()` resolves `admin_key` from the attribute first, then the env var, and fails with a diagnostic naming both if neither is set — never a bare "unauthenticated." `endpoint` lets the same provider target a local dev server (`http://localhost:8000/api/v1/`) or a review app.

The HTTP client (`internal/provider/client.go`) is the one place that sends `Authorization: Bearer <admin_key>` and the one place that parses `{"error": {"code", "message"}}`. Every resource calls through it; none constructs a request by hand — the same "one wrapper, nobody else touches the wire" rule `datagecko.ai`'s own `integrations/` packages follow, applied here to the client of that API instead of a provider of one.

## 5. Resources

### 5.1 `datagecko_stream`

| Attribute | | |
|---|---|---|
| `id` | computed | stream UUID |
| `name` | required, updates in place | `^[a-z][a-z0-9_.]{0,127}$`, validated client-side before the round trip; `PATCH /streams/{id}` now accepts it |
| `description` | optional, updates in place | `PATCH /streams/{id}` |
| `first_seen` | computed | read-only timestamp |

Create → `POST /streams`. Read → `GET /streams` (the only listing endpoint; there is no `GET /streams/{id}`) filtered client-side by `id`; absent ⇒ removed from state. Update → `PATCH /streams/{id}` with whichever of `name`/`description` changed. Delete → `DELETE /streams/{id}` — irreversible and server-audited; the example config and generated docs carry a `lifecycle { prevent_destroy = true }` recommendation.

### 5.2 `datagecko_series`

| Attribute | | |
|---|---|---|
| `id` | computed | series UUID |
| `stream_id` | required, **RequiresReplace** | a series cannot move between streams — nothing in the API supports that |
| `name` | required, updates in place | `^[a-z][a-z0-9_]{0,63}$`; `PATCH .../series/{id}` |
| `unit` | optional, updates in place | same `PATCH` |
| `last_point_at` | computed | read-only, nullable |

Create → `POST /streams/{stream_id}/series`. Read → the same `GET /streams` call, walking into the matching stream's nested `series[]`. Update → `PATCH /streams/{stream_id}/series/{series_id}`. Delete → `DELETE /streams/{stream_id}/series/{series_id}`.

### 5.3 `datagecko_ingest_key`

Role is not an attribute — it is hard-coded to `ingest`. The provider has no business minting an admin key, and the API would refuse it from an admin key anyway.

| Attribute | | |
|---|---|---|
| `id` | computed | the key's `prefix` — how `/keys/{prefix}` addresses it |
| `name` | required, updates in place | `PATCH /keys/{prefix}` (ingest-role only; an admin-role prefix would 403, but the provider never creates one, so that path is unreachable in practice) |
| `secret` | computed, **sensitive** | only ever returned by `POST /keys`; stored in state, never refetchable — the same write-once shape as `aws_iam_access_key`'s secret |
| `created`, `last_used_at`, `revoked` | computed | refreshed from `GET /keys` |

Create → `POST /keys`. Read → `GET /keys`, matched by prefix; `revoked: true` or absent ⇒ removed from state (revoked outside Terraform). Update → `PATCH /keys/{prefix}` for `name`. Delete → `DELETE /keys/{prefix}`.

## 6. Error handling and drift

- Client-side regex validation for stream and series names at plan time, mirroring `core/names.py`, so a bad name fails before any request is sent.
- `409 already_exists` on create ⇒ a diagnostic pointing at `terraform import` rather than retrying create.
- `404` / absent on refresh ⇒ silently dropped from state (the standard "deleted out of band" behavior).
- `429` ⇒ one bounded retry honoring `Retry-After` before failing the apply.
- Import: `terraform import datagecko_stream.x <uuid>`, `datagecko_series.x <stream_uuid>/<series_uuid>`, `datagecko_ingest_key.x <prefix>` — falls out of Read needing no extra API surface.

## 7. Testing

`internal/provider/testutil/fake_api.go` is an `httptest.Server` implementing exactly the endpoints this provider calls (list/create/patch/delete for streams and series, create/patch/delete for keys) and the documented error shape, including `already_exists`, `name_error`, `not_found`, and `429` with `Retry-After`. Each resource's `_test.go` drives `terraform-plugin-testing`'s `resource.UnitTest` (no `TF_ACC`, no real datagecko instance) against that fake through create → update → import → drift (fake returns 404 or `revoked: true`) → delete, for every attribute named above.

## 8. Build and distribution

`go build` produces a local binary. The docs (and `examples/`) include the `~/.terraformrc` `dev_overrides` block that points `registry.terraform.io/funktionswerk/datagecko` at that binary — no signing, no registry, no CI release pipeline for this iteration.

## 9. Out of scope

- Organization creation/lifecycle (§2).
- Admin key creation, rotation, or rename — an admin key is what authenticates the provider; the provider does not manage its own credential.
- Points/data (`POST /api/v1/points`) — that's runtime data, not infrastructure, and doesn't belong in Terraform state.
- Terraform Registry publishing, GPG signing, release automation.
- Multi-region/multi-endpoint failover — `endpoint` is a single URL per provider instance.

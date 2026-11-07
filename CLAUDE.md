# terraform-provider-datagecko — developer notes

## Stack

Go · `terraform-plugin-framework` · `terraform-plugin-testing` (unit tests against a fake HTTP double, no real datagecko instance) · Terraform CLI (needed locally to run the resource tests, which drive real plan/apply/destroy cycles against the provider binary).

## Commands

```bash
go build ./...
go test ./...
go vet ./...
gofmt -l .        # empty output means clean
```

## Architecture

```
main.go                          # provider server entrypoint
internal/provider/
  provider.go                     # schema, Configure (endpoint + admin_key)
  client.go                       # the one HTTP client: auth header, error-shape decoding, retry on 429 (three windows)
  json_attrs.go                   # JSON attributes: jsontypes.Normalized in state, raw bytes on the wire
  framework.go                    # what every resource shares: the embedded client holder, error predicates, the refuse/delete helpers
  declared_list_resource.go       # the generic replace-whole list resource; charts, sections and panels are shapes of it
  stream_resource.go
  series_resource.go
  ingest_key_resource.go
  metric_resource.go
  stream_metric_resource.go
  metric_types_data_source.go
  stream_charts_resource.go       # \
  stream_sections_resource.go     #  } replace-whole lists: one per stream, Create refuses a non-empty list
  stream_query_panels_resource.go # /
  action_resource.go
  runner_hook_resource.go
  helpers_test.go                 # providerBlock and import-id helpers for the acceptance tests
  testutil/fake_api.go            # in-memory double of datagecko's /api/v1/, test-only
examples/                         # provider + one example per resource
```

Every HTTP call to datagecko goes through `internal/provider/client.go` — no resource file builds a request by hand, the same "one wrapper" rule `datagecko.ai`'s own `integrations/` packages follow.

JSON attributes travel as `json.RawMessage` and live in state as `jsontypes.Normalized` — never re-marshalled through `float64` (the API's params are exact-typed), never compared as strings (its `jsonb` columns reorder keys).

A row that already exists on create is an error naming the remedy — "declared outside Terraform; import it" — never adopted. Bounds and vocabularies are the API's; the provider surfaces its refusal verbatim instead of copying the rule.

## Git commits

Do not add `Co-Authored-By` lines or any other attribution trailers to commit messages or PR descriptions in this repo.

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
  client.go                       # the one HTTP client: auth header, error-shape decoding, retry-on-429
  stream_resource.go
  series_resource.go
  ingest_key_resource.go
  testutil/fake_api.go            # in-memory double of datagecko's /api/v1/, test-only
examples/                         # provider + one example per resource
```

Every HTTP call to datagecko goes through `internal/provider/client.go` — no resource file builds a request by hand, the same "one wrapper" rule `datagecko.ai`'s own `integrations/` packages follow.

## Git commits

Do not add `Co-Authored-By` lines or any other attribution trailers to commit messages or PR descriptions in this repo.

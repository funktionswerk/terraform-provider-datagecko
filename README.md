# terraform-provider-datagecko

Manages [datagecko](https://datagecko.ai) streams, series, metrics and their stream rows, charts, sections, query panels, actions, runner hooks and ingest keys for one organization, authenticated by that organization's admin key.

Declare one `datagecko_stream_charts`, `datagecko_stream_sections` or `datagecko_stream_query_panels` per stream: each replaces its list whole, so two on one stream overwrite each other on every apply.

JSON attributes (`params`, `parameters`, `parameters_schema`) are compared as JSON values, so key order and whitespace never show as a diff; write them with `jsonencode()`.

## Installing

The provider is published to the Terraform Registry as `funktionswerk/datagecko`:

    terraform {
      required_providers {
        datagecko = {
          source  = "funktionswerk/datagecko"
          version = "~> 0.1"
        }
      }
    }

Reference docs for every resource are on the Registry page; they are generated from the schema descriptions and `examples/` (`go generate ./...`).

## Developing against a local build

Build it and point Terraform at the binary with a dev override:

    go build -o terraform-provider-datagecko

Add to `~/.terraformrc`:

    provider_installation {
      dev_overrides {
        "registry.terraform.io/funktionswerk/datagecko" = "/absolute/path/to/this/checkout"
      }
      direct {}
    }

With a dev override active, `terraform init` is skipped for this provider — just run `terraform plan`/`apply` directly.

## Releasing

Push a tag `vX.Y.Z` on `main`. The release workflow builds every platform with GoReleaser, signs the checksums with the registered GPG key (repository secrets `GPG_PRIVATE_KEY` and `PASSPHRASE`) and publishes a GitHub release; the Registry imports it through its webhook. Regenerate `docs/` before tagging whenever a schema description changed.

## Usage

See `examples/`. Set `DATAGECKO_ADMIN_KEY` or pass `admin_key` in the `provider` block — either way, one provider instance (or one `alias`) manages exactly one organization, because an admin key *is* an organization's credential.

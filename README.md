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

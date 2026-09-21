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
				// PlanOnly: applying this step's plan would silently
				// reconcile (recreate) the resource, leaving the next
				// refresh's plan empty. Stopping at `terraform plan`
				// captures the actual pending-recreate plan that proves
				// Read() dropped the revoked key from state.
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
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

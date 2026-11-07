package provider_test

import (
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"

	"github.com/funktionswerk/terraform-provider-datagecko/internal/provider/testutil"
)

func TestAccActionResource_createUpdateImport(t *testing.T) {
	fake := testutil.NewFakeAPI()
	defer fake.Close()
	const name = "datagecko_action.rows"
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories(),
		Steps: []resource.TestStep{
			{
				Config: providerBlock(fake.Server.URL) + `
resource "datagecko_action" "rows" {
  name              = "rows"
  description       = "Top rows of the day"
  parameters_schema = jsonencode({ properties = { dt = { type = "string" }, limit = { type = "integer" } } })
  expires_after     = 120
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(name, "id"),
					resource.TestCheckResourceAttr(name, "expires_after", "120"),
					resource.TestCheckResourceAttr(name, "result_max_bytes", "1048576"),
					resource.TestCheckResourceAttr(name, "request_role", "member"),
				),
			},
			{
				// Dropping expires_after keeps the stored value (Optional + Computed);
				// the replace still sends it, from the plan.
				Config: providerBlock(fake.Server.URL) + `
resource "datagecko_action" "rows" {
  name              = "rows"
  description       = "Top rows"
  parameters_schema = jsonencode({ properties = { dt = { type = "string" }, limit = { type = "integer" } } })
}
`,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(name, plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(name, "description", "Top rows"),
					resource.TestCheckResourceAttr(name, "expires_after", "120"),
				),
			},
			{
				ResourceName:                         name,
				ImportState:                          true,
				ImportStateVerify:                    true,
				ImportStateId:                        "rows",
				ImportStateVerifyIdentifierAttribute: "name",
			},
		},
	})
}

func TestAccActionResource_serverDefaultsReadBackWithoutADiff(t *testing.T) {
	fake := testutil.NewFakeAPI()
	defer fake.Close()
	const name = "datagecko_action.rows"
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories(),
		Steps: []resource.TestStep{{
			Config: providerBlock(fake.Server.URL) + `
resource "datagecko_action" "rows" {
  name = "rows"
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr(name, "expires_after", "10"),
				resource.TestCheckResourceAttr(name, "result_max_bytes", "1048576"),
				resource.TestCheckResourceAttr(name, "request_role", "member"),
				resource.TestCheckResourceAttr(name, "parameters_schema", "{}"),
				resource.TestCheckResourceAttr(name, "description", ""),
			),
		}},
	})
}

func TestAccActionResource_renameReplaces(t *testing.T) {
	fake := testutil.NewFakeAPI()
	defer fake.Close()
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories(),
		Steps: []resource.TestStep{
			{Config: providerBlock(fake.Server.URL) + `
resource "datagecko_action" "rows" { name = "rows" }
`},
			{
				Config: providerBlock(fake.Server.URL) + `
resource "datagecko_action" "rows" { name = "top_rows" }
`,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("datagecko_action.rows", plancheck.ResourceActionReplace),
				}},
				Check: resource.TestCheckResourceAttr("datagecko_action.rows", "name", "top_rows"),
			},
		},
	})
}

func TestAccActionResource_overCapIsTheServersError(t *testing.T) {
	fake := testutil.NewFakeAPI()
	defer fake.Close()
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories(),
		Steps: []resource.TestStep{{
			Config: providerBlock(fake.Server.URL) + `
resource "datagecko_action" "slow" {
  name          = "slow"
  expires_after = 7200
}
`,
			ExpectError: regexp.MustCompile("3600"),
		}},
	})
}

func TestAccActionResource_existingNameIsRefusedOnCreate(t *testing.T) {
	fake := testutil.NewFakeAPI()
	defer fake.Close()
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories(),
		Steps: []resource.TestStep{{
			Config: providerBlock(fake.Server.URL) + `
resource "datagecko_action" "a" { name = "rows" }
resource "datagecko_action" "b" { name = "rows" }
`,
			ExpectError: regexp.MustCompile(`declared outside\s+Terraform;\s+import`),
		}},
	})
}

func TestAccActionResource_deleteWhileARunIsInFlightSurfacesTheConflict(t *testing.T) {
	fake := testutil.NewFakeAPI()
	defer fake.Close()
	declared := providerBlock(fake.Server.URL) + `
resource "datagecko_action" "rows" { name = "rows" }
`
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories(),
		Steps: []resource.TestStep{
			{Config: declared},
			{
				PreConfig:   func() { fake.SetRunInFlight("rows") },
				Config:      providerBlock(fake.Server.URL),
				ExpectError: regexp.MustCompile(`runs in\s+flight`),
			},
			{
				// The run finishes; the delete then goes through.
				PreConfig: func() { fake.ClearRunsInFlight() },
				Config:    providerBlock(fake.Server.URL),
			},
		},
	})
}

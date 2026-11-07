package provider_test

import (
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"

	"github.com/funktionswerk/terraform-provider-datagecko/internal/provider/testutil"
)

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
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("datagecko_metric.freshness", "name", "Fresh"),
					resource.TestCheckResourceAttr("datagecko_metric.freshness", "effective", `{"critical_after":9,"warning_after":4}`),
				),
			},
			{ResourceName: "datagecko_metric.freshness", ImportState: true, ImportStateVerify: true},
		},
	})
}

func TestAccMetricResource_paramsDefaultToEmptyObject(t *testing.T) {
	fake := testutil.NewFakeAPI()
	defer fake.Close()
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories(),
		Steps: []resource.TestStep{{
			Config: providerBlock(fake.Server.URL) + `
resource "datagecko_metric" "freshness" {
  type = "freshness"
  name = "Freshness"
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("datagecko_metric.freshness", "params", "{}"),
				resource.TestCheckResourceAttr("datagecko_metric.freshness", "effective", `{"critical_after":7,"warning_after":2}`),
			),
		}},
	})
}

func TestAccMetricResource_typeChangeReplaces(t *testing.T) {
	fake := testutil.NewFakeAPI()
	defer fake.Close()
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories(),
		Steps: []resource.TestStep{
			{Config: providerBlock(fake.Server.URL) + `
resource "datagecko_metric" "m" {
  type = "freshness"
  name = "Watch"
}
`},
			{
				Config: providerBlock(fake.Server.URL) + `
resource "datagecko_metric" "m" {
  type = "coverage"
  name = "Watch"
}
`,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("datagecko_metric.m", plancheck.ResourceActionReplace),
				}},
				Check: resource.TestCheckResourceAttr("datagecko_metric.m", "type", "coverage"),
			},
		},
	})
}

func TestAccMetricResource_jsonKeyOrderIsNotADiff(t *testing.T) {
	// The fake reads params back key-sorted, as jsonb does. The harness's
	// post-apply plan must still be empty, and state keeps the user's bytes.
	fake := testutil.NewFakeAPI()
	defer fake.Close()
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories(),
		Steps: []resource.TestStep{{
			Config: providerBlock(fake.Server.URL) + `
resource "datagecko_metric" "freshness" {
  type   = "freshness"
  name   = "Freshness"
  params = "{\"warning_after\": 3, \"critical_after\": 9}"
}
`,
			Check: resource.TestCheckResourceAttr("datagecko_metric.freshness", "params", `{"warning_after": 3, "critical_after": 9}`),
		}},
	})
}

func TestAccMetricResource_takenNameIsARealError(t *testing.T) {
	fake := testutil.NewFakeAPI()
	defer fake.Close()
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories(),
		Steps: []resource.TestStep{{
			Config: providerBlock(fake.Server.URL) + `
resource "datagecko_metric" "a" {
  type = "freshness"
  name = "Freshness"
}

resource "datagecko_metric" "b" {
  type = "freshness"
  name = "freshness"
}
`,
			ExpectError: regexp.MustCompile("already exists"),
		}},
	})
}

func TestAccMetricResource_deletedOutOfBandIsRecreated(t *testing.T) {
	fake := testutil.NewFakeAPI()
	defer fake.Close()
	config := providerBlock(fake.Server.URL) + `
resource "datagecko_metric" "freshness" {
  type = "freshness"
  name = "Freshness"
}
`
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories(),
		Steps: []resource.TestStep{
			{Config: config},
			{
				PreConfig:          func() { fake.DeleteAllMetrics() },
				Config:             config,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

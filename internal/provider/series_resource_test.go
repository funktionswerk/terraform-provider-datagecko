package provider_test

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"

	"github.com/funktionswerk/terraform-provider-datagecko/internal/provider/testutil"
)

func TestAccSeriesResource_createUpdateImport(t *testing.T) {
	fake := testutil.NewFakeAPI()
	defer fake.Close()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories(),
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
provider "datagecko" {
  endpoint  = %q
  admin_key = "test"
}

resource "datagecko_stream" "revenue" {
  name = "revenue"
}

resource "datagecko_series" "delivery" {
  stream_id = datagecko_stream.revenue.id
  name      = "delivery"
  unit      = "USD"
}
`, fake.Server.URL),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("datagecko_series.delivery", "name", "delivery"),
					resource.TestCheckResourceAttr("datagecko_series.delivery", "unit", "USD"),
					resource.TestCheckResourceAttrSet("datagecko_series.delivery", "id"),
				),
			},
			{
				Config: fmt.Sprintf(`
provider "datagecko" {
  endpoint  = %q
  admin_key = "test"
}

resource "datagecko_stream" "revenue" {
  name = "revenue"
}

resource "datagecko_series" "delivery" {
  stream_id = datagecko_stream.revenue.id
  name      = "delivered"
  unit      = "EUR"
}
`, fake.Server.URL),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("datagecko_series.delivery", "name", "delivered"),
					resource.TestCheckResourceAttr("datagecko_series.delivery", "unit", "EUR"),
				),
			},
			{
				ResourceName:      "datagecko_series.delivery",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateIdFunc: seriesImportID("datagecko_series.delivery"),
			},
		},
	})
}

func TestAccSeriesResource_deletedOutOfBandIsRecreated(t *testing.T) {
	fake := testutil.NewFakeAPI()
	defer fake.Close()
	config := fmt.Sprintf(`
provider "datagecko" {
  endpoint  = %q
  admin_key = "test"
}

resource "datagecko_stream" "revenue" {
  name = "revenue"
}

resource "datagecko_series" "delivery" {
  stream_id = datagecko_stream.revenue.id
  name      = "delivery"
}
`, fake.Server.URL)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories(),
		Steps: []resource.TestStep{
			{Config: config},
			{
				// PlanOnly: applying this step's plan would silently
				// re-converge (Terraform would just create a replacement
				// series), leaving nothing to assert. Stopping at plan lets
				// us confirm the drift itself is detected: Read() notices
				// the series is gone and drops it from state, so the plan
				// against unchanged config comes back non-empty (a create)
				// instead of empty.
				PreConfig:          func() { fake.ClearAllSeries() },
				Config:             config,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

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

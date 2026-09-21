package provider_test

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

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
				ImportStateIdFunc: func(s *terraform.State) (string, error) {
					rs, ok := s.RootModule().Resources["datagecko_series.delivery"]
					if !ok {
						return "", fmt.Errorf("resource not found in state")
					}
					return rs.Primary.Attributes["stream_id"] + "/" + rs.Primary.Attributes["id"], nil
				},
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

package provider_test

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/funktionswerk/terraform-provider-datagecko/internal/provider/testutil"
)

const streamMetricBase = `
resource "datagecko_stream" "revenue" { name = "revenue" }

resource "datagecko_series" "deployed" {
  stream_id = datagecko_stream.revenue.id
  name      = "deployed"
  kind      = "marker"
}

resource "datagecko_metric" "coverage" {
  type = "coverage"
  name = "Coverage"
}
`

func TestAccStreamMetricResource_createUpdateImport(t *testing.T) {
	fake := testutil.NewFakeAPI()
	defer fake.Close()
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories(),
		Steps: []resource.TestStep{
			{
				Config: providerBlock(fake.Server.URL) + streamMetricBase + `
resource "datagecko_stream_metric" "revenue_coverage" {
  stream_id = datagecko_stream.revenue.id
  metric_id = datagecko_metric.coverage.id
  params    = jsonencode({ markers = [datagecko_series.deployed.id] })
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					func(s *terraform.State) error {
						a := s.RootModule().Resources["datagecko_stream_metric.revenue_coverage"].Primary.Attributes
						if a["id"] != a["stream_id"]+"/"+a["metric_id"] {
							return fmt.Errorf("expected id %s/%s, got %s", a["stream_id"], a["metric_id"], a["id"])
						}
						if !strings.Contains(a["effective"], `"window_days":30`) {
							return fmt.Errorf("expected the type's defaults in effective, got %s", a["effective"])
						}
						return nil
					},
				),
			},
			{
				Config: providerBlock(fake.Server.URL) + streamMetricBase + `
resource "datagecko_stream_metric" "revenue_coverage" {
  stream_id = datagecko_stream.revenue.id
  metric_id = datagecko_metric.coverage.id
  params    = jsonencode({})
}
`,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("datagecko_stream_metric.revenue_coverage", plancheck.ResourceActionUpdate),
				}},
				Check: resource.TestCheckResourceAttr("datagecko_stream_metric.revenue_coverage", "params", "{}"),
			},
			{
				ResourceName:      "datagecko_stream_metric.revenue_coverage",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateIdFunc: attrImportID("datagecko_stream_metric.revenue_coverage", "id"),
			},
		},
	})
}

func TestAccStreamMetricResource_existingRowIsRefusedOnCreate(t *testing.T) {
	fake := testutil.NewFakeAPI()
	defer fake.Close()
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories(),
		Steps: []resource.TestStep{{
			Config: providerBlock(fake.Server.URL) + streamMetricBase + `
resource "datagecko_stream_metric" "a" {
  stream_id = datagecko_stream.revenue.id
  metric_id = datagecko_metric.coverage.id
}

resource "datagecko_stream_metric" "b" {
  stream_id = datagecko_stream.revenue.id
  metric_id = datagecko_metric.coverage.id
}
`,
			ExpectError: regexp.MustCompile(`declared outside\s+Terraform;\s+import`),
		}},
	})
}

func TestAccStreamMetricResource_streamChangeReplaces(t *testing.T) {
	fake := testutil.NewFakeAPI()
	defer fake.Close()
	other := `
resource "datagecko_stream" "orders" { name = "orders" }
`
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories(),
		Steps: []resource.TestStep{
			{Config: providerBlock(fake.Server.URL) + streamMetricBase + other + `
resource "datagecko_stream_metric" "m" {
  stream_id = datagecko_stream.revenue.id
  metric_id = datagecko_metric.coverage.id
}
`},
			{
				Config: providerBlock(fake.Server.URL) + streamMetricBase + other + `
resource "datagecko_stream_metric" "m" {
  stream_id = datagecko_stream.orders.id
  metric_id = datagecko_metric.coverage.id
}
`,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("datagecko_stream_metric.m", plancheck.ResourceActionReplace),
				}},
			},
		},
	})
}

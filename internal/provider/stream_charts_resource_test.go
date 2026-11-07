package provider_test

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/funktionswerk/terraform-provider-datagecko/internal/provider/testutil"
)

const chartsBase = `
resource "datagecko_stream" "revenue" { name = "revenue" }

resource "datagecko_series" "delivery" {
  stream_id = datagecko_stream.revenue.id
  name      = "delivery"
}

resource "datagecko_series" "baseline" {
  stream_id = datagecko_stream.revenue.id
  name      = "baseline"
}

resource "datagecko_series" "deployed" {
  stream_id = datagecko_stream.revenue.id
  name      = "deployed"
  kind      = "marker"
}
`

const heroChart = `
  chart {
    title = "Delivery vs baseline"
    series {
      id           = datagecko_series.delivery.id
      color        = "#f59e0b"
      width        = 2.5
      point_radius = 3
    }
    series {
      id   = datagecko_series.baseline.id
      dash = "dashed"
    }
    marker { id = datagecko_series.deployed.id }
  }
`

// captureID stores a resource's id from state, for a later step's PreConfig or Check.
func captureID(name string, into *string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[name]
		if !ok {
			return fmt.Errorf("resource %s not found in state", name)
		}
		*into = rs.Primary.ID
		return nil
	}
}

func TestAccStreamChartsResource_createUpdateImport(t *testing.T) {
	fake := testutil.NewFakeAPI()
	defer fake.Close()
	const name = "datagecko_stream_charts.revenue"
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories(),
		Steps: []resource.TestStep{
			{
				Config: providerBlock(fake.Server.URL) + chartsBase + `
resource "datagecko_stream_charts" "revenue" {
  stream_id = datagecko_stream.revenue.id
` + heroChart + `}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(name, "chart.#", "1"),
					resource.TestCheckResourceAttr(name, "chart.0.title", "Delivery vs baseline"),
					resource.TestCheckResourceAttr(name, "chart.0.series.0.width", "2.5"),
					resource.TestCheckResourceAttr(name, "chart.0.series.1.dash", "dashed"),
					resource.TestCheckResourceAttrSet(name, "chart.0.marker.0.id"),
					resource.TestCheckResourceAttrPair(name, "id", name, "stream_id"),
				),
			},
			{
				Config: providerBlock(fake.Server.URL) + chartsBase + `
resource "datagecko_stream_charts" "revenue" {
  stream_id = datagecko_stream.revenue.id
` + heroChart + `
  chart {
    series { id = datagecko_series.baseline.id }
  }
}
`,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(name, plancheck.ResourceActionUpdate),
				}},
				Check: resource.TestCheckResourceAttr(name, "chart.#", "2"),
			},
			{
				ResourceName:      name,
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateIdFunc: attrImportID(name, "stream_id"),
			},
		},
	})
}

func TestAccStreamChartsResource_unsetStyleStaysNull(t *testing.T) {
	fake := testutil.NewFakeAPI()
	defer fake.Close()
	const name = "datagecko_stream_charts.revenue"
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories(),
		Steps: []resource.TestStep{{
			Config: providerBlock(fake.Server.URL) + chartsBase + `
resource "datagecko_stream_charts" "revenue" {
  stream_id = datagecko_stream.revenue.id
  chart {
    series { id = datagecko_series.delivery.id }
  }
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckNoResourceAttr(name, "chart.0.series.0.color"),
				resource.TestCheckNoResourceAttr(name, "chart.0.series.0.width"),
				resource.TestCheckNoResourceAttr(name, "chart.0.series.0.dash"),
				resource.TestCheckNoResourceAttr(name, "chart.0.series.0.fill"),
				resource.TestCheckNoResourceAttr(name, "chart.0.series.0.point_radius"),
				resource.TestCheckNoResourceAttr(name, "chart.0.series.0.point_style"),
			),
		}},
	})
}

func TestAccStreamChartsResource_existingListIsRefusedOnCreate(t *testing.T) {
	fake := testutil.NewFakeAPI()
	defer fake.Close()
	var streamID string
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories(),
		Steps: []resource.TestStep{
			{
				Config: providerBlock(fake.Server.URL) + chartsBase,
				Check:  captureID("datagecko_stream.revenue", &streamID),
			},
			{
				PreConfig: func() {
					fake.DeclareCharts(streamID, []map[string]interface{}{
						{"title": "Made in the UI", "series": []interface{}{}, "markers": []interface{}{}},
					})
				},
				Config: providerBlock(fake.Server.URL) + chartsBase + `
resource "datagecko_stream_charts" "revenue" {
  stream_id = datagecko_stream.revenue.id
  chart {
    series { id = datagecko_series.delivery.id }
  }
}
`,
				ExpectError: regexp.MustCompile(`declared outside\s+Terraform;\s+import`),
			},
		},
	})
}

func TestAccStreamChartsResource_foreignSeriesIsTheServersError(t *testing.T) {
	fake := testutil.NewFakeAPI()
	defer fake.Close()
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories(),
		Steps: []resource.TestStep{{
			Config: providerBlock(fake.Server.URL) + chartsBase + `
resource "datagecko_stream" "orders" { name = "orders" }

resource "datagecko_series" "count" {
  stream_id = datagecko_stream.orders.id
  name      = "count"
}

resource "datagecko_stream_charts" "revenue" {
  stream_id = datagecko_stream.revenue.id
  chart {
    series { id = datagecko_series.count.id }
  }
}
`,
			ExpectError: regexp.MustCompile("no series"),
		}},
	})
}

func TestAccStreamChartsResource_destroyLeavesTheImplicitChart(t *testing.T) {
	fake := testutil.NewFakeAPI()
	defer fake.Close()
	var streamID string
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories(),
		Steps: []resource.TestStep{
			{
				Config: providerBlock(fake.Server.URL) + chartsBase + `
resource "datagecko_stream_charts" "revenue" {
  stream_id = datagecko_stream.revenue.id
` + heroChart + `}
`,
				Check: captureID("datagecko_stream.revenue", &streamID),
			},
			{
				Config: providerBlock(fake.Server.URL) + chartsBase,
				Check: func(*terraform.State) error {
					if n := fake.ChartCount(streamID); n != 0 {
						return fmt.Errorf("expected the declared charts cleared, got %d", n)
					}
					return nil
				},
			},
		},
	})
}

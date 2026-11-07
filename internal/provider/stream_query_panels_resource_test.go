package provider_test

import (
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"

	"github.com/funktionswerk/terraform-provider-datagecko/internal/provider/testutil"
)

func TestAccStreamQueryPanelsResource_createUpdateImport(t *testing.T) {
	fake := testutil.NewFakeAPI()
	defer fake.Close()
	const name = "datagecko_stream_query_panels.revenue"
	panels := func(limit string) string {
		return providerBlock(fake.Server.URL) + sectionsBase + `
resource "datagecko_stream_query_panels" "revenue" {
  stream_id = datagecko_stream.revenue.id
  panel {
    title      = "Top rows"
    action     = "rows"
    parameters = jsonencode({ limit = ` + limit + ` })
  }
  panel {
    title  = "Totals"
    action = "totals"
  }
}
`
	}
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories(),
		Steps: []resource.TestStep{
			{
				Config: panels("10"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(name, "panel.#", "2"),
					resource.TestCheckResourceAttr(name, "panel.0.parameters", `{"limit":10}`),
					resource.TestCheckResourceAttr(name, "panel.1.parameters", "{}"),
				),
			},
			{
				Config: panels("25"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(name, plancheck.ResourceActionUpdate),
				}},
				Check: resource.TestCheckResourceAttr(name, "panel.0.parameters", `{"limit":25}`),
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

func TestAccStreamQueryPanelsResource_dtIsTheServersError(t *testing.T) {
	fake := testutil.NewFakeAPI()
	defer fake.Close()
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories(),
		Steps: []resource.TestStep{{
			Config: providerBlock(fake.Server.URL) + sectionsBase + `
resource "datagecko_stream_query_panels" "revenue" {
  stream_id = datagecko_stream.revenue.id
  panel {
    title      = "Day"
    action     = "rows"
    parameters = jsonencode({ dt = "2026-01-01" })
  }
}
`,
			ExpectError: regexp.MustCompile(`may not carry\s+dt`),
		}},
	})
}

func TestAccStreamQueryPanelsResource_existingListIsRefusedOnCreate(t *testing.T) {
	fake := testutil.NewFakeAPI()
	defer fake.Close()
	var streamID string
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories(),
		Steps: []resource.TestStep{
			{
				Config: providerBlock(fake.Server.URL) + sectionsBase,
				Check:  captureID("datagecko_stream.revenue", &streamID),
			},
			{
				PreConfig: func() {
					fake.DeclarePanels(streamID, []map[string]interface{}{
						{"title": "Made in the UI", "action": "rows", "parameters": map[string]interface{}{}},
					})
				},
				Config: providerBlock(fake.Server.URL) + sectionsBase + `
resource "datagecko_stream_query_panels" "revenue" {
  stream_id = datagecko_stream.revenue.id
  panel {
    title  = "Totals"
    action = "totals"
  }
}
`,
				ExpectError: regexp.MustCompile(`declared outside\s+Terraform;\s+import`),
			},
		},
	})
}

package provider_test

import (
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"

	"github.com/funktionswerk/terraform-provider-datagecko/internal/provider/testutil"
)

const sectionsBase = `
resource "datagecko_stream" "revenue" { name = "revenue" }
`

func TestAccStreamSectionsResource_createUpdateImport(t *testing.T) {
	fake := testutil.NewFakeAPI()
	defer fake.Close()
	const name = "datagecko_stream_sections.revenue"
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories(),
		Steps: []resource.TestStep{
			{
				Config: providerBlock(fake.Server.URL) + sectionsBase + `
resource "datagecko_stream_sections" "revenue" {
  stream_id = datagecko_stream.revenue.id
  section {
    title = "What this is"
    body  = "Daily revenue by channel."
  }
  section { title = "Owner" }
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(name, "section.#", "2"),
					resource.TestCheckResourceAttr(name, "section.0.body", "Daily revenue by channel."),
					resource.TestCheckResourceAttr(name, "section.1.body", ""),
				),
			},
			{
				// Reordering is a change: the list is ordered.
				Config: providerBlock(fake.Server.URL) + sectionsBase + `
resource "datagecko_stream_sections" "revenue" {
  stream_id = datagecko_stream.revenue.id
  section { title = "Owner" }
  section {
    title = "What this is"
    body  = "Daily revenue by channel."
  }
}
`,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(name, plancheck.ResourceActionUpdate),
				}},
				Check: resource.TestCheckResourceAttr(name, "section.0.title", "Owner"),
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

func TestAccStreamSectionsResource_bodyRoundTripsExactly(t *testing.T) {
	fake := testutil.NewFakeAPI()
	defer fake.Close()
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories(),
		Steps: []resource.TestStep{{
			Config: providerBlock(fake.Server.URL) + sectionsBase + `
resource "datagecko_stream_sections" "revenue" {
  stream_id = datagecko_stream.revenue.id
  section {
    title = "About"
    body  = <<-EOT
      Line one

      Line two
    EOT
  }
}
`,
			Check: resource.TestCheckResourceAttr("datagecko_stream_sections.revenue", "section.0.body", "Line one\n\nLine two\n"),
		}},
	})
}

func TestAccStreamSectionsResource_existingListIsRefusedOnCreate(t *testing.T) {
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
					fake.DeclareSections(streamID, []map[string]interface{}{{"title": "Made in the UI", "body": ""}})
				},
				Config: providerBlock(fake.Server.URL) + sectionsBase + `
resource "datagecko_stream_sections" "revenue" {
  stream_id = datagecko_stream.revenue.id
  section { title = "Owner" }
}
`,
				ExpectError: regexp.MustCompile(`declared outside\s+Terraform;\s+import`),
			},
		},
	})
}

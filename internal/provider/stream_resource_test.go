package provider_test

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/funktionswerk/terraform-provider-datagecko/internal/provider"
	"github.com/funktionswerk/terraform-provider-datagecko/internal/provider/testutil"
)

func protoV6Factories() map[string]func() (tfprotov6.ProviderServer, error) {
	return map[string]func() (tfprotov6.ProviderServer, error){
		"datagecko": providerserver.NewProtocol6WithError(provider.New("test")()),
	}
}

func TestAccStreamResource_createUpdateImport(t *testing.T) {
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
  name        = "revenue"
  description = "Daily revenue"
}
`, fake.Server.URL),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("datagecko_stream.revenue", "name", "revenue"),
					resource.TestCheckResourceAttr("datagecko_stream.revenue", "description", "Daily revenue"),
					resource.TestCheckResourceAttrSet("datagecko_stream.revenue", "id"),
					resource.TestCheckResourceAttrSet("datagecko_stream.revenue", "first_seen"),
				),
			},
			{
				// A name-only change renames in place — no replace plan.
				Config: fmt.Sprintf(`
provider "datagecko" {
  endpoint  = %q
  admin_key = "test"
}

resource "datagecko_stream" "revenue" {
  name        = "sales"
  description = "Daily revenue"
}
`, fake.Server.URL),
				Check: resource.TestCheckResourceAttr("datagecko_stream.revenue", "name", "sales"),
			},
			{
				ResourceName:      "datagecko_stream.revenue",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccStreamResource_conflictingNameIsARealError(t *testing.T) {
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

resource "datagecko_stream" "a" {
  name = "revenue"
}

resource "datagecko_stream" "b" {
  name = "revenue"
}
`, fake.Server.URL),
				ExpectError: regexp.MustCompile("already exists"),
			},
		},
	})
}

func TestAccStreamResource_sameNameDifferentTagsIsTwoStreams(t *testing.T) {
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

resource "datagecko_stream" "online" {
  name = "acme.com"
  tags = { sales_channel = "online" }
}

resource "datagecko_stream" "offline" {
  name = "acme.com"
  tags = { sales_channel = "offline" }
}
`, fake.Server.URL),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("datagecko_stream.online", "tags.sales_channel", "online"),
					resource.TestCheckResourceAttr("datagecko_stream.offline", "tags.sales_channel", "offline"),
					func(s *terraform.State) error {
						a := s.RootModule().Resources["datagecko_stream.online"].Primary.ID
						b := s.RootModule().Resources["datagecko_stream.offline"].Primary.ID
						if a == b {
							return fmt.Errorf("expected two distinct streams, got the same id %q", a)
						}
						return nil
					},
				),
			},
		},
	})
}

func TestAccStreamResource_tagsDefaultToEmptyAndUpdateInPlace(t *testing.T) {
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
`, fake.Server.URL),
				Check: resource.TestCheckResourceAttr("datagecko_stream.revenue", "tags.%", "0"),
			},
			{
				// A tag-only change retags in place — no replace plan.
				Config: fmt.Sprintf(`
provider "datagecko" {
  endpoint  = %q
  admin_key = "test"
}

resource "datagecko_stream" "revenue" {
  name = "revenue"
  tags = { sales_channel = "online" }
}
`, fake.Server.URL),
				Check: resource.TestCheckResourceAttr("datagecko_stream.revenue", "tags.sales_channel", "online"),
			},
		},
	})
}

func TestAccStreamResource_retaggingOntoATakenIdentityIsARealError(t *testing.T) {
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

resource "datagecko_stream" "online" {
  name = "acme.com"
  tags = { sales_channel = "online" }
}

resource "datagecko_stream" "offline" {
  name = "acme.com"
  tags = { sales_channel = "offline" }
}
`, fake.Server.URL),
			},
			{
				Config: fmt.Sprintf(`
provider "datagecko" {
  endpoint  = %q
  admin_key = "test"
}

resource "datagecko_stream" "online" {
  name = "acme.com"
  tags = { sales_channel = "online" }
}

resource "datagecko_stream" "offline" {
  name = "acme.com"
  tags = { sales_channel = "online" }
}
`, fake.Server.URL),
				ExpectError: regexp.MustCompile("already exists"),
			},
		},
	})
}

func TestAccStreamResource_deletedOutOfBandIsRecreated(t *testing.T) {
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
`, fake.Server.URL)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories(),
		Steps: []resource.TestStep{
			{Config: config},
			{
				// PlanOnly: applying this step's plan would silently
				// re-converge (Terraform would just create a replacement
				// stream), leaving nothing to assert. Stopping at plan lets
				// us confirm the drift itself is detected: Read() notices
				// the stream is gone and drops it from state, so the plan
				// against unchanged config comes back non-empty (a create)
				// instead of empty.
				PreConfig:          func() { fake.DeleteAllStreams() },
				Config:             config,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

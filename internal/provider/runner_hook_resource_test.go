package provider_test

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"

	"github.com/funktionswerk/terraform-provider-datagecko/internal/provider/testutil"
)

const hookName = "datagecko_runner_hook.runner"

func hookConfig(endpointURL, url string) string {
	return providerBlock(endpointURL) + fmt.Sprintf(`
resource "datagecko_ingest_key" "runner" { name = "runner" }

resource "datagecko_runner_hook" "runner" {
  key_prefix = datagecko_ingest_key.runner.id
  url        = %q
}
`, url)
}

// captureAttr stores one attribute from state, for a later step to compare.
func captureAttr(name, attr string, into *string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[name]
		if !ok {
			return fmt.Errorf("resource %s not found in state", name)
		}
		*into = rs.Primary.Attributes[attr]
		return nil
	}
}

func TestAccRunnerHookResource_createImport(t *testing.T) {
	fake := testutil.NewFakeAPI()
	defer fake.Close()
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories(),
		Steps: []resource.TestStep{
			{
				Config: hookConfig(fake.Server.URL, "https://runner.example/ring"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(hookName, "secret"),
					resource.TestCheckResourceAttr(hookName, "url", "https://runner.example/ring"),
					resource.TestCheckResourceAttr(hookName, "last_ring_status", ""),
					resource.TestCheckResourceAttrPair(hookName, "id", hookName, "key_prefix"),
				),
			},
			{
				ResourceName:            hookName,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateIdFunc:       attrImportID(hookName, "key_prefix"),
				ImportStateVerifyIgnore: []string{"secret"},
			},
		},
	})
}

func TestAccRunnerHookResource_urlChangeRotatesTheSecret(t *testing.T) {
	fake := testutil.NewFakeAPI()
	defer fake.Close()
	var first, second string
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories(),
		Steps: []resource.TestStep{
			{
				Config: hookConfig(fake.Server.URL, "https://runner.example/ring"),
				Check:  captureAttr(hookName, "secret", &first),
			},
			{
				Config: hookConfig(fake.Server.URL, "https://runner.example/v2"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(hookName, plancheck.ResourceActionUpdate),
					plancheck.ExpectUnknownValue(hookName, tfjsonpath.New("secret")),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					captureAttr(hookName, "secret", &second),
					func(*terraform.State) error {
						if second == first {
							return fmt.Errorf("expected a new secret after the url changed, kept %q", first)
						}
						return nil
					},
				),
			},
			{
				// An unchanged url keeps the secret: the plan is empty (the harness checks).
				Config: hookConfig(fake.Server.URL, "https://runner.example/v2"),
				Check:  resource.TestCheckResourceAttrPtr(hookName, "secret", &second),
			},
		},
	})
}

func TestAccRunnerHookResource_existingHookIsRefusedOnCreate(t *testing.T) {
	fake := testutil.NewFakeAPI()
	defer fake.Close()
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories(),
		Steps: []resource.TestStep{{
			Config: providerBlock(fake.Server.URL) + `
resource "datagecko_ingest_key" "runner" { name = "runner" }

resource "datagecko_runner_hook" "a" {
  key_prefix = datagecko_ingest_key.runner.id
  url        = "https://runner.example/a"
}

resource "datagecko_runner_hook" "b" {
  key_prefix = datagecko_ingest_key.runner.id
  url        = "https://runner.example/b"
}
`,
			ExpectError: regexp.MustCompile(`declared outside\s+Terraform;\s+import`),
		}},
	})
}

func TestAccRunnerHookResource_httpIsTheServersError(t *testing.T) {
	fake := testutil.NewFakeAPI()
	defer fake.Close()
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories(),
		Steps: []resource.TestStep{{
			Config:      hookConfig(fake.Server.URL, "http://runner.example/ring"),
			ExpectError: regexp.MustCompile("https"),
		}},
	})
}

func TestAccRunnerHookResource_unknownURLThatResolvesUnchangedKeepsTheSecret(t *testing.T) {
	// The url comes from a resource that is replaced in step 2, so it is
	// unknown at plan time — and then resolves to the same value. A working
	// runner's secret must not rotate for that: Terraform re-plans the hook
	// once the url is known, finds no change, and never calls Update.
	fake := testutil.NewFakeAPI()
	defer fake.Close()
	config := func(generation int) string {
		return providerBlock(fake.Server.URL) + fmt.Sprintf(`
resource "terraform_data" "url" {
  triggers_replace = %d
}

resource "datagecko_ingest_key" "runner" { name = "runner" }

resource "datagecko_runner_hook" "runner" {
  key_prefix = datagecko_ingest_key.runner.id
  # substr(id, 0, 0) is "" — but unknown while terraform_data.url is replaced.
  url = "https://runner.example/ring${substr(terraform_data.url.id, 0, 0)}"
}
`, generation)
	}
	var first string
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories(),
		Steps: []resource.TestStep{
			{Config: config(1), Check: captureAttr(hookName, "secret", &first)},
			{
				Config: config(2),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectUnknownValue(hookName, tfjsonpath.New("url")),
				}},
				Check: resource.TestCheckResourceAttrPtr(hookName, "secret", &first),
			},
		},
	})
}

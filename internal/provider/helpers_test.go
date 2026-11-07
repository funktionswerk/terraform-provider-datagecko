package provider_test

import (
	"fmt"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// providerBlock is the provider stanza every acceptance config starts with.
func providerBlock(endpointURL string) string {
	return fmt.Sprintf(`
provider "datagecko" {
  endpoint  = %q
  admin_key = "test"
}
`, endpointURL)
}

// seriesImportID is a series' import identifier, <stream_id>/<series_id>.
func seriesImportID(name string) resource.ImportStateIdFunc {
	return func(s *terraform.State) (string, error) {
		rs, ok := s.RootModule().Resources[name]
		if !ok {
			return "", fmt.Errorf("resource %s not found in state", name)
		}
		return rs.Primary.Attributes["stream_id"] + "/" + rs.Primary.Attributes["id"], nil
	}
}

// attrImportID imports a resource by one of its own attributes.
func attrImportID(name, attr string) resource.ImportStateIdFunc {
	return func(s *terraform.State) (string, error) {
		rs, ok := s.RootModule().Resources[name]
		if !ok {
			return "", fmt.Errorf("resource %s not found in state", name)
		}
		return rs.Primary.Attributes[attr], nil
	}
}

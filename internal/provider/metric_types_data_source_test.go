package provider_test

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/funktionswerk/terraform-provider-datagecko/internal/provider/testutil"
)

func TestAccMetricTypesDataSource_listsEveryType(t *testing.T) {
	fake := testutil.NewFakeAPI()
	defer fake.Close()
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories(),
		Steps: []resource.TestStep{{
			Config: providerBlock(fake.Server.URL) + `data "datagecko_metric_types" "all" {}`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("data.datagecko_metric_types.all", "types.#", "2"),
				resource.TestCheckResourceAttr("data.datagecko_metric_types.all", "types.0.key", "freshness"),
				resource.TestCheckResourceAttr("data.datagecko_metric_types.all", "types.0.defaults", `{"critical_after":7,"warning_after":2}`),
				resource.TestCheckResourceAttr("data.datagecko_metric_types.all", "types.1.per_stream.0", "markers"),
			),
		}},
	})
}

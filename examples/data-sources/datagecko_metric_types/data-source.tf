data "datagecko_metric_types" "all" {}

output "freshness_defaults" {
  value = jsondecode([for t in data.datagecko_metric_types.all.types : t.defaults if t.key == "freshness"][0])
}

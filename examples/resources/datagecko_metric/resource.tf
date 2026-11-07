resource "datagecko_metric" "freshness" {
  type   = "freshness"
  name   = "Freshness"
  params = jsonencode({ warning_after = 2, critical_after = 7 })
}

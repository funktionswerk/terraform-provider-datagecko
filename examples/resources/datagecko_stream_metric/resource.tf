resource "datagecko_stream_metric" "revenue_coverage" {
  stream_id = datagecko_stream.revenue.id
  metric_id = datagecko_metric.coverage.id
  params = jsonencode({
    markers = [datagecko_series.deployed.id]
    target  = datagecko_series.delivery.id
  })
}

resource "datagecko_series" "delivery" {
  stream_id = datagecko_stream.revenue.id
  name      = "delivery"
  unit      = "USD"
}

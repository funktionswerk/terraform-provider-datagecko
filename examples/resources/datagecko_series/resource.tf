resource "datagecko_series" "delivery" {
  stream_id = datagecko_stream.revenue.id
  name      = "delivery"
  unit      = "USD"
}

resource "datagecko_series" "deployed" {
  stream_id     = datagecko_stream.revenue.id
  name          = "deployed"
  kind          = "marker" # shown as a value, never plotted; cannot change after creation
  interval_days = 7        # declared cadence; freshness measures days behind net of it
}

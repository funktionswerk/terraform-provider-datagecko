# One datagecko_stream_query_panels per stream. Two on one stream overwrite each other on every apply.
resource "datagecko_stream_query_panels" "revenue" {
  stream_id = datagecko_stream.revenue.id

  panel {
    title      = "Top rows"
    action     = datagecko_action.rows.name
    parameters = jsonencode({ limit = 10 }) # `dt` is datagecko's to fill
  }
}

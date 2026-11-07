# One datagecko_stream_sections per stream. Two on one stream overwrite each other on every apply.
resource "datagecko_stream_sections" "revenue" {
  stream_id = datagecko_stream.revenue.id

  section {
    title = "What this is"
    body  = <<-EOT
      Daily revenue per sales channel, pushed nightly from the warehouse.

      Contact: data team.
    EOT
  }
}

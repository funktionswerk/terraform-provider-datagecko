resource "datagecko_stream" "revenue" {
  name        = "revenue"
  description = "Daily revenue, by delivery status."

  # A stream's DELETE removes every series it holds and their points, and
  # cannot be undone — guard against an accidental destroy.
  lifecycle {
    prevent_destroy = true
  }
}

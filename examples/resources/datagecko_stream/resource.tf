resource "datagecko_stream" "revenue" {
  name        = "revenue"
  description = "Daily revenue, by delivery status."

  # A stream's DELETE removes every series it holds and their points, and
  # cannot be undone — guard against an accidental destroy.
  lifecycle {
    prevent_destroy = true
  }
}

# tags are part of a stream's identity, together with its name — the same
# name with a different tag set is a different stream. This is how one
# merchant with two sales channels gets two streams under the same name.
resource "datagecko_stream" "buckle_online" {
  name = "buckle.com"
  tags = { sales_channel = "online" }
}

resource "datagecko_stream" "buckle_offline" {
  name = "buckle.com"
  tags = { sales_channel = "offline" }
}

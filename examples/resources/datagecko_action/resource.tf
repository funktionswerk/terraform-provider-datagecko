resource "datagecko_action" "rows" {
  name        = "rows"
  description = "Top rows of the day"
  parameters_schema = jsonencode({
    properties = {
      dt    = { type = "string" }
      limit = { type = "integer" }
    }
  })
  expires_after = 120 # seconds a run may wait for a runner; at most 3600
}

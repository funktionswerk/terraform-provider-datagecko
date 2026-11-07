# One datagecko_stream_charts per stream. Two on one stream overwrite each other on every apply.
resource "datagecko_stream_charts" "revenue" {
  stream_id = datagecko_stream.revenue.id

  chart {
    title = "Delivery vs baseline"
    series {
      id           = datagecko_series.delivery.id
      color        = "#f59e0b"
      width        = 2.5
      point_radius = 3
    }
    series {
      id    = datagecko_series.baseline.id
      color = "#64748b"
      dash  = "dashed"
    }
    marker { id = datagecko_series.deployed.id }
  }
}

resource "datagecko_ingest_key" "shop_feed" {
  name = "shop-feed"
}

output "shop_feed_secret" {
  value     = datagecko_ingest_key.shop_feed.secret
  sensitive = true
}

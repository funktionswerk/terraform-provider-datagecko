resource "datagecko_ingest_key" "runner" {
  name = "runner"
}

resource "datagecko_runner_hook" "runner" {
  key_prefix = datagecko_ingest_key.runner.id
  url        = "https://runner.example.com/datagecko/ring" # https only; never put a secret in it, it is logged
}

# datagecko_runner_hook.runner.secret signs every ring; changing url issues a new one.

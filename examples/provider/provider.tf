terraform {
  required_providers {
    datagecko = {
      source  = "funktionswerk/datagecko"
      version = "~> 0.1"
    }
  }
}

provider "datagecko" {
  # admin_key can also come from the DATAGECKO_ADMIN_KEY environment variable.
  admin_key = var.datagecko_admin_key
}

variable "datagecko_admin_key" {
  type      = string
  sensitive = true
  default   = null
}

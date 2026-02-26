terraform {
  required_providers {
    random = {
      source = "hashicorp/random"
    }
  }
}

resource "random_id" "unit" {
  byte_length = 4
  keepers = {
    prefix = var.prefix
    name   = var.name
  }
}

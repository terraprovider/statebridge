terraform {
  required_providers {
    random = {
      source = "hashicorp/random"
    }
  }
}

# The "after" variant of foreachmod: random_id.items has been removed from
# configuration (it was moved to another layer). Only random_id.keep remains.
# A cross-layer move's source layer switches to this module so the moved
# resource is absent from config, which OpenTofu requires for a removed block.
resource "random_id" "keep" {
  byte_length = 4
  keepers = {
    prefix = var.prefix
    name   = "keep"
  }
}

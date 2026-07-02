terraform {
  required_providers {
    random = {
      source = "hashicorp/random"
    }
  }
}

# For_each resource whose instances are moved out of the (indexed) module.
resource "random_id" "items" {
  for_each    = toset(var.keys)
  byte_length = 4
  keepers = {
    prefix = var.prefix
    key    = each.key
  }
}

# Sibling resource that stays behind. Its presence ensures the module instance
# is not fully emptied, so statebridge emits a resource-level removed block for
# random_id.items rather than consolidating to a module-level removal.
resource "random_id" "keep" {
  byte_length = 4
  keepers = {
    prefix = var.prefix
    name   = "keep"
  }
}

variable "prefix" {
  type = string
}

# Declared for interface compatibility with the foreachmod module block, but
# unused here since random_id.items no longer exists in this variant.
variable "keys" {
  type    = list(string)
  default = []
}

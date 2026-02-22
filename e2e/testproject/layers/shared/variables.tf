variable "prefix" {
  description = "Resource name prefix for isolation"
  type        = string
}

variable "location" {
  description = "Azure region"
  type        = string
  default     = "westeurope"
}

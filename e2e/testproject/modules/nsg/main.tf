terraform {
  required_providers {
    azurerm = {
      source = "hashicorp/azurerm"
    }
  }
}

resource "azurerm_network_security_group" "nsg" {
  name                = "${var.prefix}-e2e-${var.suffix}"
  location            = var.location
  resource_group_name = var.resource_group_name
}

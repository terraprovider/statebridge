terraform {
  required_providers {
    azurerm = {
      source = "hashicorp/azurerm"
    }
  }
}

provider "azurerm" {
  features {}
}

resource "azurerm_resource_group" "test" {
  name     = "${var.prefix}-e2e-shared"
  location = var.location
}

resource "azurerm_storage_account" "accounts" {
  for_each = toset(["alpha", "beta", "gamma"])

  name                     = "${var.prefix}${each.key}"
  resource_group_name      = azurerm_resource_group.test.name
  location                 = azurerm_resource_group.test.location
  account_tier             = "Standard"
  account_replication_type = "LRS"
}

resource "azurerm_virtual_network" "main" {
  name                = "${var.prefix}-e2e-vnet"
  address_space       = ["10.0.0.0/16"]
  location            = azurerm_resource_group.test.location
  resource_group_name = azurerm_resource_group.test.name
}

resource "azurerm_resource_group" "importable" {
  name     = "${var.prefix}-e2e-importable"
  location = var.location
}

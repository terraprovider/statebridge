//go:build e2e

package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestMain(m *testing.M) {
	// Skip all e2e tests if Azure credentials are not configured
	if os.Getenv("ARM_SUBSCRIPTION_ID") == "" {
		fmt.Println("Skipping e2e tests: ARM_SUBSCRIPTION_ID not set")
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// TestE2E_MoveResource tests moving a single resource (VNet) from the shared
// layer to the networking layer.
func TestE2E_MoveResource(t *testing.T) {
	rootDir, prefix, vars := setupTestProject(t)

	sharedDir := filepath.Join(rootDir, "layers", "shared")
	networkingDir := filepath.Join(rootDir, "layers", "networking")

	// Initialize and apply shared layer to create resources
	tofuInit(t, sharedDir)
	tofuApply(t, sharedDir, vars)
	t.Cleanup(func() {
		// Destroy shared resources — networking is destroyed first since it
		// may hold resources originally in shared.
		tofuDestroy(t, networkingDir, vars)
		tofuDestroy(t, sharedDir, vars)
	})

	// Verify VNet exists in shared state
	assertResourceInState(t, sharedDir, "azurerm_virtual_network.main")

	// Write migration YAML
	migDir := writeMigration(t, rootDir, "001_move_vnet.yaml", `
description: "Move VNet from shared to networking"
operations:
  - type: move
    source_layer: "./layers/shared"
    destination_layer: "./layers/networking"
    resources:
      - address: "azurerm_virtual_network.main"
`)

	// Add the VNet resource definition to the networking layer
	updateTfFile(t, networkingDir, "main.tf", fmt.Sprintf(`
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

resource "azurerm_virtual_network" "main" {
  name                = "${var.prefix}-e2e-vnet"
  address_space       = ["10.0.0.0/16"]
  location            = var.location
  resource_group_name = "%s-e2e-shared"
}
`, prefix))

	// Remove VNet from shared layer config
	updateTfFile(t, sharedDir, "main.tf", `
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

resource "azurerm_resource_group" "importable" {
  name     = "${var.prefix}-e2e-importable"
  location = var.location
}
`)

	// Run the migration engine
	files := runGenerate(t, rootDir, []string{migDir})
	if len(files) == 0 {
		t.Fatal("expected generated migration files, got none")
	}
	t.Logf("Generated %d migration file(s): %v", len(files), files)

	// Initialize and apply networking layer (import the VNet)
	tofuInit(t, networkingDir)
	tofuApply(t, networkingDir, vars)

	// Apply shared layer (remove the VNet from state)
	tofuApply(t, sharedDir, vars)

	// Verify: VNet is in networking state, not in shared state
	assertResourceInState(t, networkingDir, "azurerm_virtual_network.main")
	assertResourceNotInState(t, sharedDir, "azurerm_virtual_network.main")

	// Verify clean plans in both layers
	cleanupMigrationFiles(t, sharedDir)
	cleanupMigrationFiles(t, networkingDir)
	assertCleanPlan(t, sharedDir, vars)
	assertCleanPlan(t, networkingDir, vars)
}

// TestE2E_KeyedMove tests moving for_each resources (storage accounts) from
// the shared layer to the app layer with key remapping.
func TestE2E_KeyedMove(t *testing.T) {
	rootDir, prefix, vars := setupTestProject(t)

	sharedDir := filepath.Join(rootDir, "layers", "shared")
	appDir := filepath.Join(rootDir, "layers", "app")

	// Initialize and apply shared layer
	tofuInit(t, sharedDir)
	tofuApply(t, sharedDir, vars)
	t.Cleanup(func() {
		tofuDestroy(t, appDir, vars)
		tofuDestroy(t, sharedDir, vars)
	})

	// Verify storage accounts exist in shared state
	for _, key := range []string{"alpha", "beta", "gamma"} {
		assertResourceInState(t, sharedDir, fmt.Sprintf(`azurerm_storage_account.accounts["%s"]`, key))
	}

	// Write keyed move migration YAML
	migDir := writeMigration(t, rootDir, "001_move_storage.yaml", `
description: "Move storage accounts with key remapping"
operations:
  - type: move
    source_layer: "./layers/shared"
    destination_layer: "./layers/app"
    resources:
      - address: "azurerm_storage_account.accounts"
        keys:
          alpha: app_alpha
          beta: app_beta
          gamma: app_gamma
`)

	// Add storage account resource to the app layer with new keys
	updateTfFile(t, appDir, "main.tf", fmt.Sprintf(`
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

resource "azurerm_storage_account" "accounts" {
  for_each = {
    app_alpha = "alpha"
    app_beta  = "beta"
    app_gamma = "gamma"
  }

  name                     = "${var.prefix}${each.value}"
  resource_group_name      = "%s-e2e-shared"
  location                 = var.location
  account_tier             = "Standard"
  account_replication_type = "LRS"
}
`, prefix))

	// Remove storage accounts from shared layer config
	updateTfFile(t, sharedDir, "main.tf", `
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
`)

	// Run the migration engine
	files := runGenerate(t, rootDir, []string{migDir})
	if len(files) == 0 {
		t.Fatal("expected generated migration files, got none")
	}
	t.Logf("Generated %d migration file(s): %v", len(files), files)

	// Initialize and apply app layer (import storage accounts with new keys)
	tofuInit(t, appDir)
	tofuApply(t, appDir, vars)

	// Apply shared layer (remove storage accounts from state)
	tofuApply(t, sharedDir, vars)

	// Verify: accounts in app state with new keys
	for _, key := range []string{"app_alpha", "app_beta", "app_gamma"} {
		assertResourceInState(t, appDir, fmt.Sprintf(`azurerm_storage_account.accounts["%s"]`, key))
	}

	// Verify clean plans
	cleanupMigrationFiles(t, sharedDir)
	cleanupMigrationFiles(t, appDir)
	assertCleanPlan(t, sharedDir, vars)
	assertCleanPlan(t, appDir, vars)
}

// TestE2E_RenameResource tests renaming a resource within a single layer.
func TestE2E_RenameResource(t *testing.T) {
	rootDir, _, vars := setupTestProject(t)

	sharedDir := filepath.Join(rootDir, "layers", "shared")

	// Initialize and apply shared layer
	tofuInit(t, sharedDir)
	tofuApply(t, sharedDir, vars)
	t.Cleanup(func() {
		tofuDestroy(t, sharedDir, vars)
	})

	// Verify resource exists under old name
	assertResourceInState(t, sharedDir, "azurerm_resource_group.importable")

	// Write rename migration YAML
	migDir := writeMigration(t, rootDir, "001_rename_rg.yaml", `
description: "Rename importable resource group"
operations:
  - type: rename
    layer: "./layers/shared"
    renames:
      - from: "azurerm_resource_group.importable"
        to: "azurerm_resource_group.secondary"
`)

	// Update shared layer config to use the new name
	updateTfFile(t, sharedDir, "main.tf", `
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

resource "azurerm_resource_group" "secondary" {
  name     = "${var.prefix}-e2e-importable"
  location = var.location
}
`)

	// Run the migration engine
	files := runGenerate(t, rootDir, []string{migDir})
	if len(files) == 0 {
		t.Fatal("expected generated migration files, got none")
	}
	t.Logf("Generated %d migration file(s): %v", len(files), files)

	// Apply the rename
	tofuApply(t, sharedDir, vars)

	// Verify: old name gone, new name present
	assertResourceNotInState(t, sharedDir, "azurerm_resource_group.importable")
	assertResourceInState(t, sharedDir, "azurerm_resource_group.secondary")

	// Verify clean plan
	cleanupMigrationFiles(t, sharedDir)
	assertCleanPlan(t, sharedDir, vars)
}

// TestE2E_RemoveAndImport tests removing a resource from state (keeping the
// infrastructure) and then importing it back. These are tested together since
// import requires a resource that exists in Azure but not in state.
func TestE2E_RemoveAndImport(t *testing.T) {
	rootDir, prefix, vars := setupTestProject(t)

	sharedDir := filepath.Join(rootDir, "layers", "shared")
	subscriptionID := os.Getenv("ARM_SUBSCRIPTION_ID")

	// Initialize and apply shared layer
	tofuInit(t, sharedDir)
	tofuApply(t, sharedDir, vars)
	t.Cleanup(func() {
		tofuDestroy(t, sharedDir, vars)
	})

	// Verify importable RG exists in state
	assertResourceInState(t, sharedDir, "azurerm_resource_group.importable")

	// --- Phase 1: Remove ---

	// Write remove migration YAML
	migDir := writeMigration(t, rootDir, "001_remove_rg.yaml", `
description: "Stop managing importable RG"
operations:
  - type: remove
    layer: "./layers/shared"
    addresses:
      - "azurerm_resource_group.importable"
`)

	// Remove the resource definition from shared layer
	updateTfFile(t, sharedDir, "main.tf", `
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
`)

	// Run the migration engine for remove
	files := runGenerate(t, rootDir, []string{migDir})
	if len(files) == 0 {
		t.Fatal("expected generated migration files for remove, got none")
	}
	t.Logf("Remove phase: generated %d migration file(s)", len(files))

	// Apply the removal
	tofuApply(t, sharedDir, vars)

	// Verify: resource is gone from state
	assertResourceNotInState(t, sharedDir, "azurerm_resource_group.importable")

	// Verify clean plan (resource removed from both config and state)
	cleanupMigrationFiles(t, sharedDir)
	assertCleanPlan(t, sharedDir, vars)

	// --- Phase 2: Import ---

	// Clean up old migration files
	migrationsDir := filepath.Join(rootDir, "migrations")
	os.RemoveAll(migrationsDir)

	// Write import migration YAML
	rgName := prefix + "-e2e-importable"
	importID := fmt.Sprintf("/subscriptions/%s/resourceGroups/%s", subscriptionID, rgName)
	migDir = writeMigration(t, rootDir, "002_import_rg.yaml", formatMigrationYAML(`
description: "Import existing resource group"
operations:
  - type: import
    layer: "./layers/shared"
    imports:
      - address: "azurerm_resource_group.importable"
        import_id: "%s"
`, importID))

	// Add the resource definition back to shared layer
	updateTfFile(t, sharedDir, "main.tf", `
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
`)

	// Run the migration engine for import
	files = runGenerate(t, rootDir, []string{migDir})
	if len(files) == 0 {
		t.Fatal("expected generated migration files for import, got none")
	}
	t.Logf("Import phase: generated %d migration file(s)", len(files))

	// Apply the import
	tofuApply(t, sharedDir, vars)

	// Verify: resource is back in state
	assertResourceInState(t, sharedDir, "azurerm_resource_group.importable")

	// Verify clean plan
	cleanupMigrationFiles(t, sharedDir)
	assertCleanPlan(t, sharedDir, vars)
}

// TestE2E_ConditionSkip tests that the condition system correctly skips
// migrations when preconditions are not met.
func TestE2E_ConditionSkip(t *testing.T) {
	rootDir, _, vars := setupTestProject(t)

	sharedDir := filepath.Join(rootDir, "layers", "shared")

	// Initialize and apply shared layer
	tofuInit(t, sharedDir)
	tofuApply(t, sharedDir, vars)
	t.Cleanup(func() {
		tofuDestroy(t, sharedDir, vars)
	})

	// Write a migration with a condition that should NOT be met
	// (resource that doesn't exist in state)
	migDir := writeMigration(t, rootDir, "001_conditional.yaml", `
description: "Should be skipped due to unmet condition"
condition:
  resources_exist:
    - layer: "./layers/shared"
      addresses:
        - "azurerm_resource_group.nonexistent"
operations:
  - type: remove
    layer: "./layers/shared"
    addresses:
      - "azurerm_resource_group.importable"
`)

	// Run the migration engine — should produce no files since condition is not met
	files := runGenerate(t, rootDir, []string{migDir})
	if len(files) != 0 {
		t.Errorf("expected no generated files (condition not met), got %d: %v", len(files), files)
	}

	// Verify: resource is still in state (migration was skipped)
	assertResourceInState(t, sharedDir, "azurerm_resource_group.importable")

	// Now test with a condition that IS met — second migration in same dir
	writeMigration(t, rootDir, "002_conditional_met.yaml", `
description: "Should proceed — condition is met"
condition:
  resources_exist:
    - layer: "./layers/shared"
      addresses:
        - "azurerm_resource_group.importable"
operations:
  - type: rename
    layer: "./layers/shared"
    renames:
      - from: "azurerm_resource_group.importable"
        to: "azurerm_resource_group.secondary"
`)

	// Update shared layer to match the rename
	updateTfFile(t, sharedDir, "main.tf", `
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

resource "azurerm_resource_group" "secondary" {
  name     = "${var.prefix}-e2e-importable"
  location = var.location
}
`)

	// Clean up previously generated migration files since we're re-running the same dir
	cleanupMigrationFiles(t, sharedDir)

	// This time only the second migration (002) should generate output
	files = runGenerate(t, rootDir, []string{migDir})
	if len(files) == 0 {
		t.Fatal("expected generated migration files for conditional rename, got none")
	}

	// Apply
	tofuApply(t, sharedDir, vars)

	// Verify rename happened
	assertResourceInState(t, sharedDir, "azurerm_resource_group.secondary")
	assertResourceNotInState(t, sharedDir, "azurerm_resource_group.importable")

	// Verify clean plan
	cleanupMigrationFiles(t, sharedDir)
	assertCleanPlan(t, sharedDir, vars)
}

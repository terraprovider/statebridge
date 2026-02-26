//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/redtenant/tfmigrate/pkg/download"
	"github.com/redtenant/tfmigrate/pkg/upload"
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
	t.Parallel()
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

	// Write migration YAML with absolute layer paths
	migDir := writeMigration(t, rootDir, "001_move_vnet.yaml", fmt.Sprintf(`
description: "Move VNet from shared to networking"
operations:
  - type: move
    source_layer: "%s"
    destination_layer: "%s"
    resources:
      - from: "azurerm_virtual_network.main"
`, sharedDir, networkingDir))

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

resource "azurerm_network_security_group" "nsgs" {
  for_each = toset(["alpha", "beta", "gamma"])

  name                = "${var.prefix}-e2e-${each.key}"
  location            = azurerm_resource_group.test.location
  resource_group_name = azurerm_resource_group.test.name
}

resource "azurerm_resource_group" "importable" {
  name     = "${var.prefix}-e2e-importable"
  location = var.location
}
`)

	// Run the migration engine
	files := runGenerate(t, []string{migDir})
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

// TestE2E_KeyedMove tests moving for_each resources (NSGs) from
// the shared layer to the app layer with key remapping.
func TestE2E_KeyedMove(t *testing.T) {
	t.Parallel()
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

	// Verify NSGs exist in shared state
	for _, key := range []string{"alpha", "beta", "gamma"} {
		assertResourceInState(t, sharedDir, fmt.Sprintf(`azurerm_network_security_group.nsgs["%s"]`, key))
	}

	// Write keyed move migration YAML with absolute layer paths
	migDir := writeMigration(t, rootDir, "001_move_nsgs.yaml", fmt.Sprintf(`
description: "Move NSGs with key remapping"
operations:
  - type: move
    source_layer: "%s"
    destination_layer: "%s"
    resources:
      - from: "azurerm_network_security_group.nsgs"
        keys:
          alpha: app_alpha
          beta: app_beta
          gamma: app_gamma
`, sharedDir, appDir))

	// Add NSG resource to the app layer with new keys
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

resource "azurerm_network_security_group" "nsgs" {
  for_each = {
    app_alpha = "alpha"
    app_beta  = "beta"
    app_gamma = "gamma"
  }

  name                = "${var.prefix}-e2e-${each.value}"
  resource_group_name = "%s-e2e-shared"
  location            = var.location
}
`, prefix))

	// Remove NSGs from shared layer config
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
	files := runGenerate(t, []string{migDir})
	if len(files) == 0 {
		t.Fatal("expected generated migration files, got none")
	}
	t.Logf("Generated %d migration file(s): %v", len(files), files)

	// Initialize and apply app layer (import NSGs with new keys)
	tofuInit(t, appDir)
	tofuApply(t, appDir, vars)

	// Apply shared layer (remove NSGs from state)
	tofuApply(t, sharedDir, vars)

	// Verify: NSGs in app state with new keys
	for _, key := range []string{"app_alpha", "app_beta", "app_gamma"} {
		assertResourceInState(t, appDir, fmt.Sprintf(`azurerm_network_security_group.nsgs["%s"]`, key))
	}

	// Verify clean plans
	cleanupMigrationFiles(t, sharedDir)
	cleanupMigrationFiles(t, appDir)
	assertCleanPlan(t, sharedDir, vars)
	assertCleanPlan(t, appDir, vars)
}

// TestE2E_RenameResource tests renaming a resource within a single layer.
func TestE2E_RenameResource(t *testing.T) {
	t.Parallel()
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

	// Write rename migration YAML with absolute layer path
	migDir := writeMigration(t, rootDir, "001_rename_rg.yaml", fmt.Sprintf(`
description: "Rename importable resource group"
operations:
  - type: rename
    layer: "%s"
    renames:
      - from: "azurerm_resource_group.importable"
        to: "azurerm_resource_group.secondary"
`, sharedDir))

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

resource "azurerm_network_security_group" "nsgs" {
  for_each = toset(["alpha", "beta", "gamma"])

  name                = "${var.prefix}-e2e-${each.key}"
  location            = azurerm_resource_group.test.location
  resource_group_name = azurerm_resource_group.test.name
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
	files := runGenerate(t, []string{migDir})
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
	t.Parallel()
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

	// Write remove migration YAML with absolute layer path
	migDir := writeMigration(t, rootDir, "001_remove_rg.yaml", fmt.Sprintf(`
description: "Stop managing importable RG"
operations:
  - type: remove
    layer: "%s"
    entries:
      - address: "azurerm_resource_group.importable"
`, sharedDir))

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

resource "azurerm_network_security_group" "nsgs" {
  for_each = toset(["alpha", "beta", "gamma"])

  name                = "${var.prefix}-e2e-${each.key}"
  location            = azurerm_resource_group.test.location
  resource_group_name = azurerm_resource_group.test.name
}

resource "azurerm_virtual_network" "main" {
  name                = "${var.prefix}-e2e-vnet"
  address_space       = ["10.0.0.0/16"]
  location            = azurerm_resource_group.test.location
  resource_group_name = azurerm_resource_group.test.name
}
`)

	// Run the migration engine for remove
	files := runGenerate(t, []string{migDir})
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

	// Write import migration YAML with absolute layer path
	rgName := prefix + "-e2e-importable"
	importID := fmt.Sprintf("/subscriptions/%s/resourceGroups/%s", subscriptionID, rgName)
	migDir = writeMigration(t, rootDir, "002_import_rg.yaml", fmt.Sprintf(`
description: "Import existing resource group"
operations:
  - type: import
    layer: "%s"
    imports:
      - address: "azurerm_resource_group.importable"
        id: "%s"
`, sharedDir, importID))

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

resource "azurerm_network_security_group" "nsgs" {
  for_each = toset(["alpha", "beta", "gamma"])

  name                = "${var.prefix}-e2e-${each.key}"
  location            = azurerm_resource_group.test.location
  resource_group_name = azurerm_resource_group.test.name
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
	files = runGenerate(t, []string{migDir})
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
	t.Parallel()
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
	migDir := writeMigration(t, rootDir, "001_conditional.yaml", fmt.Sprintf(`
description: "Should be skipped due to unmet condition"
condition:
  resources_exist:
    - layer: "%s"
      addresses:
        - "azurerm_resource_group.nonexistent"
operations:
  - type: remove
    layer: "%s"
    entries:
      - address: "azurerm_resource_group.importable"
`, sharedDir, sharedDir))

	// Run the migration engine — should produce no files since condition is not met
	files := runGenerate(t, []string{migDir})
	if len(files) != 0 {
		t.Errorf("expected no generated files (condition not met), got %d: %v", len(files), files)
	}

	// Verify: resource is still in state (migration was skipped)
	assertResourceInState(t, sharedDir, "azurerm_resource_group.importable")

	// Now test with a condition that IS met — second migration in same dir
	writeMigration(t, rootDir, "002_conditional_met.yaml", fmt.Sprintf(`
description: "Should proceed — condition is met"
condition:
  resources_exist:
    - layer: "%s"
      addresses:
        - "azurerm_resource_group.importable"
operations:
  - type: rename
    layer: "%s"
    renames:
      - from: "azurerm_resource_group.importable"
        to: "azurerm_resource_group.secondary"
`, sharedDir, sharedDir))

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

resource "azurerm_network_security_group" "nsgs" {
  for_each = toset(["alpha", "beta", "gamma"])

  name                = "${var.prefix}-e2e-${each.key}"
  location            = azurerm_resource_group.test.location
  resource_group_name = azurerm_resource_group.test.name
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
	files = runGenerate(t, []string{migDir})
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

// TestE2E_UploadDownload tests the full upload/download pipeline: generate
// migration files, upload them to Azure Blob Storage, delete the local copies,
// download them back, and verify the migration applies cleanly.
func TestE2E_UploadDownload(t *testing.T) {
	t.Parallel()

	storageAccountName := os.Getenv("E2E_STORAGE_ACCOUNT_NAME")
	if storageAccountName == "" {
		t.Skip("skipping: E2E_STORAGE_ACCOUNT_NAME not set")
	}

	rootDir, prefix, vars := setupTestProject(t)
	ctx := context.Background()

	sharedDir := filepath.Join(rootDir, "layers", "shared")
	networkingDir := filepath.Join(rootDir, "layers", "networking")

	// Initialize and apply shared layer to create resources
	tofuInit(t, sharedDir)
	tofuApply(t, sharedDir, vars)
	t.Cleanup(func() {
		tofuDestroy(t, networkingDir, vars)
		tofuDestroy(t, sharedDir, vars)
	})

	// Verify VNet exists in shared state
	assertResourceInState(t, sharedDir, "azurerm_virtual_network.main")

	// Write migration YAML
	migDir := writeMigration(t, rootDir, "001_move_vnet.yaml", fmt.Sprintf(`
description: "Move VNet from shared to networking"
operations:
  - type: move
    source_layer: "%s"
    destination_layer: "%s"
    resources:
      - from: "azurerm_virtual_network.main"
`, sharedDir, networkingDir))

	// Add VNet resource to networking layer
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

resource "azurerm_network_security_group" "nsgs" {
  for_each = toset(["alpha", "beta", "gamma"])

  name                = "${var.prefix}-e2e-${each.key}"
  location            = azurerm_resource_group.test.location
  resource_group_name = azurerm_resource_group.test.name
}

resource "azurerm_resource_group" "importable" {
  name     = "${var.prefix}-e2e-importable"
  location = var.location
}
`)

	// Generate migration files (writes to disk in both layers)
	files := runGenerate(t, []string{migDir})
	if len(files) == 0 {
		t.Fatal("expected generated migration files, got none")
	}
	t.Logf("Generated %d migration file(s): %v", len(files), files)

	// Initialize networking layer (needed for state reads during download condition evaluation)
	tofuInit(t, networkingDir)

	// Create unique blob container for this test
	containerName := prefix
	cred := getCredential(t)

	createContainer(t, ctx, cred, storageAccountName, containerName)
	t.Cleanup(func() {
		deleteContainer(t, context.Background(), cred, storageAccountName, containerName)
	})

	// Upload migration files from networking layer to blob storage
	initArgs := []string{
		"-backend-config=storage_account_name=" + storageAccountName,
		"-backend-config=container_name=" + containerName,
	}

	mgr := upload.NewManager(cred, initArgs)
	if err := mgr.UploadFromDisk(ctx, []string{networkingDir}); err != nil {
		t.Fatalf("uploading migration files: %v", err)
	}

	// Remove local migration files from networking layer
	cleanupMigrationFiles(t, networkingDir)

	// Verify no migration files remain on disk
	matches, _ := filepath.Glob(filepath.Join(networkingDir, "migration.*.tf"))
	if len(matches) != 0 {
		t.Fatalf("expected no migration files after cleanup, got %d", len(matches))
	}

	// Download from blob storage back to networking layer
	dl := download.NewDownloader(cred, initArgs, tofuExecPath(t), false)
	downloaded, err := dl.Download(ctx, networkingDir)
	if err != nil {
		t.Fatalf("downloading migration files: %v", err)
	}
	if len(downloaded) == 0 {
		t.Fatal("expected downloaded migration files, got none")
	}
	t.Logf("Downloaded %d migration file(s): %v", len(downloaded), downloaded)

	// Apply: first networking (imports VNet), then shared (removes VNet from state)
	tofuApply(t, networkingDir, vars)
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

//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

resource "azurerm_resource_group" "importable" {
	name     = "${var.prefix}-e2e-importable"
	location = var.location
}
`)

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

resource "azurerm_resource_group" "importable" {
	name     = "${var.prefix}-e2e-importable"
	location = var.location
}
`)
	networkingDir := filepath.Join(rootDir, "layers", "networking")

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
`)

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
`)

	// Run the migration engine
	requireGenerate(t, migDir)

	// Initialize and apply networking layer (import the VNet)
	tofuInit(t, networkingDir)
	tofuApply(t, networkingDir, vars)

	// Apply shared layer (remove the VNet from state)
	tofuApply(t, sharedDir, vars)

	// Verify: VNet is in networking state, not in shared state
	assertResourceInState(t, networkingDir, "azurerm_virtual_network.main")
	assertResourceNotInState(t, sharedDir, "azurerm_virtual_network.main")

	// Verify clean plans in both layers
	cleanupAndAssertClean(t, vars, sharedDir, networkingDir)
}

// TestE2E_KeyedMove tests moving for_each resources (NSGs) from
// the shared layer to the app layer with key remapping.
func TestE2E_KeyedMove(t *testing.T) {
	t.Parallel()
	rootDir, prefix, vars := setupTestProject(t)

	sharedDir := filepath.Join(rootDir, "layers", "shared")
	appDir := filepath.Join(rootDir, "layers", "app")

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
`)

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
`)

	// Run the migration engine
	requireGenerate(t, migDir)

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
	cleanupAndAssertClean(t, vars, sharedDir, appDir)
}

// TestE2E_ModuleMove tests moving a module between layers.
func TestE2E_ModuleMove(t *testing.T) {
	t.Parallel()
	rootDir, prefix, vars := setupTestProject(t)

	sharedDir := filepath.Join(rootDir, "layers", "shared")
	appDir := filepath.Join(rootDir, "layers", "app")

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

module "mod_nsg" {
  source              = "../../modules/nsg"
  prefix              = var.prefix
  suffix              = "module"
  location            = var.location
  resource_group_name = azurerm_resource_group.test.name
}
`)

	// Initialize and apply shared layer
	tofuInit(t, sharedDir)
	tofuApply(t, sharedDir, vars)
	t.Cleanup(func() {
		tofuDestroy(t, appDir, vars)
		tofuDestroy(t, sharedDir, vars)
	})

	// Verify module resource exists in shared state
	assertResourceInState(t, sharedDir, "module.mod_nsg.azurerm_network_security_group.nsg")

	// Write module move migration YAML with absolute layer paths
	migDir := writeMigration(t, rootDir, "001_move_module.yaml", fmt.Sprintf(`
description: "Move module NSG from shared to app"
operations:
  - type: move
    source_layer: "%s"
    destination_layer: "%s"
    resources:
      - from: "module.mod_nsg"
`, sharedDir, appDir))

	// Add the module to the app layer
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

module "mod_nsg" {
  source              = "../../modules/nsg"
  prefix              = var.prefix
  suffix              = "module"
  location            = var.location
  resource_group_name = "%s-e2e-shared"
}
`, prefix))

	// Remove the module from shared layer config
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
`)

	// Run the migration engine
	files := requireGenerate(t, migDir)
	var sharedMigration string
	for _, file := range files {
		if filepath.Dir(file) == sharedDir {
			sharedMigration = file
			break
		}
	}
	if sharedMigration == "" {
		t.Fatal("expected a generated migration file in the shared layer")
	}
	assertFileContains(t, sharedMigration, "from = module.mod_nsg")

	// Initialize and apply app layer (import module resources)
	tofuInit(t, appDir)
	tofuApply(t, appDir, vars)

	// Apply shared layer (remove module from state)
	tofuApply(t, sharedDir, vars)

	// Verify: module resources are in app state, not in shared state
	assertResourceInState(t, appDir, "module.mod_nsg.azurerm_network_security_group.nsg")
	assertResourceNotInState(t, sharedDir, "module.mod_nsg.azurerm_network_security_group.nsg")

	// Verify clean plans
	cleanupAndAssertClean(t, vars, sharedDir, appDir)
}

// TestE2E_AllResourcesMoveWithOverridesOmit tests a bulk move with overrides and omit entries.
func TestE2E_AllResourcesMoveWithOverridesOmit(t *testing.T) {
	t.Parallel()
	rootDir, _, vars := setupTestProject(t)

	sharedDir := filepath.Join(rootDir, "layers", "shared")
	appDir := filepath.Join(rootDir, "layers", "app")

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

resource "azurerm_resource_group" "bulk" {
  name     = "${var.prefix}-e2e-bulk"
  location = var.location
}

resource "azurerm_network_security_group" "bulk" {
  name                = "${var.prefix}-e2e-bulk-nsg"
  location            = azurerm_resource_group.bulk.location
  resource_group_name = azurerm_resource_group.bulk.name
}

resource "azurerm_network_security_group" "omit" {
  name                = "${var.prefix}-e2e-omit-nsg"
  location            = azurerm_resource_group.bulk.location
  resource_group_name = azurerm_resource_group.bulk.name
}
`)

	// Initialize and apply shared layer
	tofuInit(t, sharedDir)
	tofuApply(t, sharedDir, vars)
	t.Cleanup(func() {
		tofuDestroy(t, appDir, vars)
		tofuDestroy(t, sharedDir, vars)
	})

	assertResourceInState(t, sharedDir, "azurerm_resource_group.bulk")
	assertResourceInState(t, sharedDir, "azurerm_network_security_group.bulk")
	assertResourceInState(t, sharedDir, "azurerm_network_security_group.omit")

	// Write bulk move migration YAML
	migDir := writeMigration(t, rootDir, "001_move_all.yaml", fmt.Sprintf(`
description: "Move all resources with overrides and omit"
operations:
  - type: move
    source_layer: "%s"
    destination_layer: "%s"
    all_resources: true
    overrides:
      - from: "azurerm_network_security_group.bulk"
        to: "azurerm_network_security_group.renamed"
    omit:
      - address: "azurerm_network_security_group.omit"
        destroy: true
`, sharedDir, appDir))

	// Add resources to app layer with override name
	updateTfFile(t, appDir, "main.tf", `
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

resource "azurerm_resource_group" "bulk" {
  name     = "${var.prefix}-e2e-bulk"
  location = var.location
}

resource "azurerm_network_security_group" "renamed" {
  name                = "${var.prefix}-e2e-bulk-nsg"
  location            = azurerm_resource_group.bulk.location
  resource_group_name = azurerm_resource_group.bulk.name
}
`)

	// Remove resources from shared layer config
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
`)

	// Run the migration engine
	requireGenerate(t, migDir)

	// Initialize and apply app layer (import resources)
	tofuInit(t, appDir)
	tofuApply(t, appDir, vars)

	// Apply shared layer (remove resources from state)
	tofuApply(t, sharedDir, vars)

	// Verify: resources are in app state with override name, omitted is gone
	assertResourceInState(t, appDir, "azurerm_resource_group.bulk")
	assertResourceInState(t, appDir, "azurerm_network_security_group.renamed")
	assertResourceNotInState(t, appDir, "azurerm_network_security_group.omit")
	assertResourceNotInState(t, sharedDir, "azurerm_network_security_group.bulk")

	// Verify clean plans
	cleanupAndAssertClean(t, vars, sharedDir, appDir)
}

// TestE2E_KeyPatternMove tests key pattern mapping with prefix and catch-all rules.
func TestE2E_KeyPatternMove(t *testing.T) {
	t.Parallel()
	rootDir, _, vars := setupTestProject(t)

	sharedDir := filepath.Join(rootDir, "layers", "shared")
	appDir := filepath.Join(rootDir, "layers", "app")

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
  for_each = toset(["app_alpha", "app_beta", "core_gamma"])

  name                = "${var.prefix}-e2e-${each.key}"
  location            = azurerm_resource_group.test.location
  resource_group_name = azurerm_resource_group.test.name
}
`)

	// Initialize and apply shared layer
	tofuInit(t, sharedDir)
	tofuApply(t, sharedDir, vars)
	t.Cleanup(func() {
		tofuDestroy(t, appDir, vars)
		tofuDestroy(t, sharedDir, vars)
	})

	// Write key-pattern move migration YAML
	migDir := writeMigration(t, rootDir, "001_move_nsgs_key_patterns.yaml", fmt.Sprintf(`
description: "Move NSGs with key patterns"
operations:
  - type: move
    source_layer: "%s"
    destination_layer: "%s"
    resources:
      - from: "azurerm_network_security_group.nsgs"
        keys:
          "app_*": '{{ .Key | trimPrefix "app_" }}'
          "*": '{{ .Key }}'
`, sharedDir, appDir))

	// Add NSG resource to the app layer with new keys
	updateTfFile(t, appDir, "main.tf", `
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
    alpha      = "app_alpha"
    beta       = "app_beta"
    core_gamma = "core_gamma"
  }

  name                = "${var.prefix}-e2e-${each.value}"
  resource_group_name = "${var.prefix}-e2e-shared"
  location            = var.location
}
`)

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
`)

	// Run the migration engine
	requireGenerate(t, migDir)

	// Initialize and apply app layer (import NSGs with new keys)
	tofuInit(t, appDir)
	tofuApply(t, appDir, vars)

	// Apply shared layer (remove NSGs from state)
	tofuApply(t, sharedDir, vars)

	// Verify: NSGs in app state with remapped keys
	for _, key := range []string{"alpha", "beta", "core_gamma"} {
		assertResourceInState(t, appDir, fmt.Sprintf(`azurerm_network_security_group.nsgs["%s"]`, key))
	}

	// Verify clean plans
	cleanupAndAssertClean(t, vars, sharedDir, appDir)
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

resource "azurerm_resource_group" "secondary" {
  name     = "${var.prefix}-e2e-importable"
  location = var.location
}
`)

	// Run the migration engine
	requireGenerate(t, migDir)

	// Apply the rename
	tofuApply(t, sharedDir, vars)

	// Verify: old name gone, new name present
	assertResourceNotInState(t, sharedDir, "azurerm_resource_group.importable")
	assertResourceInState(t, sharedDir, "azurerm_resource_group.secondary")

	// Verify clean plan
	cleanupAndAssertClean(t, vars, sharedDir)
}

// TestE2E_RemoveAndImport tests removing a resource from state (keeping the
// infrastructure) and then importing it back. These are tested together since
// import requires a resource that exists in Azure but not in state.
func TestE2E_RemoveAndImport(t *testing.T) {
	t.Parallel()
	rootDir, prefix, vars := setupTestProject(t)

	sharedDir := filepath.Join(rootDir, "layers", "shared")
	subscriptionID := os.Getenv("ARM_SUBSCRIPTION_ID")

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

resource "azurerm_resource_group" "importable" {
	name     = "${var.prefix}-e2e-importable"
	location = var.location
}
`)

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
`)

	// Run the migration engine for remove
	requireGenerate(t, migDir)

	// Apply the removal
	tofuApply(t, sharedDir, vars)

	// Verify: resource is gone from state
	assertResourceNotInState(t, sharedDir, "azurerm_resource_group.importable")

	// Verify clean plan (resource removed from both config and state)
	cleanupAndAssertClean(t, vars, sharedDir)

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

resource "azurerm_resource_group" "importable" {
  name     = "${var.prefix}-e2e-importable"
  location = var.location
}
`)

	// Run the migration engine for import
	requireGenerate(t, migDir)

	// Apply the import
	tofuApply(t, sharedDir, vars)

	// Verify: resource is back in state
	assertResourceInState(t, sharedDir, "azurerm_resource_group.importable")

	// Verify clean plan
	cleanupAndAssertClean(t, vars, sharedDir)
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
	cleanupAndAssertClean(t, vars, sharedDir)
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
`)

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
`)

	// Generate migration files (writes to disk in both layers)
	requireGenerate(t, migDir)

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

	mgr := upload.NewManager(upload.BucketUploaderFactory(cred), initArgs)
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
	dl := download.NewDownloader(upload.BucketUploaderFactory(cred), initArgs, download.WithTofuPath(tofuExecPath(t)))
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
	cleanupAndAssertClean(t, vars, sharedDir, networkingDir)
}

// TestE2E_UploadGuard tests the upload guard that prevents overwriting active migrations.
func TestE2E_UploadGuard(t *testing.T) {
	t.Parallel()

	storageAccountName := os.Getenv("E2E_STORAGE_ACCOUNT_NAME")
	if storageAccountName == "" {
		t.Skip("skipping: E2E_STORAGE_ACCOUNT_NAME not set")
	}

	rootDir, prefix, vars := setupTestProject(t)
	ctx := context.Background()

	sharedDir := filepath.Join(rootDir, "layers", "shared")
	networkingDir := filepath.Join(rootDir, "layers", "networking")

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

resource "azurerm_network_security_group" "guard" {
  name                = "${var.prefix}-e2e-guard-nsg"
  location            = azurerm_resource_group.test.location
  resource_group_name = azurerm_resource_group.test.name
}
`)

	// Initialize and apply shared layer to create resources
	tofuInit(t, sharedDir)
	tofuApply(t, sharedDir, vars)
	t.Cleanup(func() {
		tofuDestroy(t, networkingDir, vars)
		tofuDestroy(t, sharedDir, vars)
	})

	assertResourceInState(t, sharedDir, "azurerm_network_security_group.guard")

	// Write migration YAML to move NSG to networking
	migDir := writeMigration(t, rootDir, "001_guard_move.yaml", fmt.Sprintf(`
description: "Move guard NSG from shared to networking"
operations:
  - type: move
    source_layer: "%s"
    destination_layer: "%s"
    resources:
      - from: "azurerm_network_security_group.guard"
`, sharedDir, networkingDir))

	// Add NSG resource to the networking layer
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

resource "azurerm_network_security_group" "guard" {
  name                = "${var.prefix}-e2e-guard-nsg"
  location            = var.location
  resource_group_name = "%s-e2e-shared"
}
`, prefix))

	// Remove NSG from shared layer config
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
`)

	// Generate migration files (writes to disk in both layers)
	files := runGenerate(t, []string{migDir})
	if len(files) == 0 {
		t.Fatal("expected generated migration files, got none")
	}

	// Initialize networking layer for guard state evaluation
	tofuInit(t, networkingDir)

	// Create unique blob container for this test
	containerName := prefix
	cred := getCredential(t)

	createContainer(t, ctx, cred, storageAccountName, containerName)
	t.Cleanup(func() {
		deleteContainer(t, context.Background(), cred, storageAccountName, containerName)
	})

	initArgs := []string{
		"-backend-config=storage_account_name=" + storageAccountName,
		"-backend-config=container_name=" + containerName,
	}

	factory := upload.BucketUploaderFactory(cred)

	guardedMgr := upload.NewManager(factory, initArgs, upload.WithTofuPath(tofuExecPath(t), initArgs))
	if err := guardedMgr.UploadFromDisk(ctx, []string{networkingDir}); err != nil {
		t.Fatalf("initial upload failed: %v", err)
	}

	// Remove local migration files and regenerate with a different metadata set
	cleanupMigrationFiles(t, networkingDir)
	cleanupMigrationFiles(t, sharedDir)

	writeMigration(t, rootDir, "001_guard_move.yaml", fmt.Sprintf(`
description: "Move guard NSG from shared to networking (guarded)"
condition:
  layer_exists:
    - "%s"
operations:
  - type: move
    source_layer: "%s"
    destination_layer: "%s"
    resources:
      - from: "azurerm_network_security_group.guard"
`, sharedDir, sharedDir, networkingDir))

	files = runGenerate(t, []string{migDir})
	if len(files) == 0 {
		t.Fatal("expected regenerated migration files, got none")
	}

	// Guard should refuse to overwrite the active migration
	if err := guardedMgr.UploadFromDisk(ctx, []string{networkingDir}); err == nil {
		t.Fatal("expected guard to refuse overwrite, but upload succeeded")
	} else if !strings.Contains(err.Error(), "refusing to overwrite") {
		t.Fatalf("unexpected guard error: %v", err)
	}

	// Force should bypass guard and succeed
	forceMgr := upload.NewManager(
		factory,
		initArgs,
		upload.WithTofuPath(tofuExecPath(t), initArgs),
		upload.WithForce(true),
	)
	if err := forceMgr.UploadFromDisk(ctx, []string{networkingDir}); err != nil {
		t.Fatalf("force upload failed: %v", err)
	}
}

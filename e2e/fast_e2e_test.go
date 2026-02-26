//go:build e2e_fast

package e2e

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/redtenant/tfmigrate/pkg/conditions"
	"github.com/redtenant/tfmigrate/pkg/download"
	"github.com/redtenant/tfmigrate/pkg/engine"
	"github.com/redtenant/tfmigrate/pkg/generator"
	"github.com/redtenant/tfmigrate/pkg/state"
	"github.com/redtenant/tfmigrate/pkg/upload"
)

// TestE2EFast_MoveResource tests moving a random_id between layers.
func TestE2EFast_MoveResource(t *testing.T) {
  t.Parallel()
  rootDir, _, vars := setupFastProject(t)

  sharedDir := filepath.Join(rootDir, "layers", "shared")
  networkingDir := filepath.Join(rootDir, "layers", "networking")

  updateTfFile(t, sharedDir, "main.tf", `
terraform {
  required_providers {
    random = {
      source = "hashicorp/random"
    }
  }
}

provider "random" {}

resource "random_id" "moved" {
  byte_length = 4
  keepers = {
    prefix = var.prefix
    name   = "moved"
  }
}

resource "random_id" "importable" {
  byte_length = 4
  keepers = {
    prefix = var.prefix
    name   = "importable"
  }
}
`)

  tofuInit(t, sharedDir)
  tofuApply(t, sharedDir, vars)
  t.Cleanup(func() {
    tofuDestroy(t, networkingDir, vars)
    tofuDestroy(t, sharedDir, vars)
  })

  assertResourceInState(t, sharedDir, "random_id.moved")

  migDir := writeMigration(t, rootDir, "001_move_random_id.yaml", fmt.Sprintf(`
description: "Move random_id from shared to networking"
operations:
  - type: move
    source_layer: "%s"
    destination_layer: "%s"
    resources:
      - from: "random_id.moved"
`, sharedDir, networkingDir))

  updateTfFile(t, networkingDir, "main.tf", `
terraform {
  required_providers {
    random = {
      source = "hashicorp/random"
    }
  }
}

provider "random" {}

resource "random_id" "moved" {
  byte_length = 4
  keepers = {
    prefix = var.prefix
    name   = "moved"
  }
}
`)

  updateTfFile(t, sharedDir, "main.tf", `
terraform {
  required_providers {
    random = {
      source = "hashicorp/random"
    }
  }
}

provider "random" {}

resource "random_id" "importable" {
  byte_length = 4
  keepers = {
    prefix = var.prefix
    name   = "importable"
  }
}
`)

  files := runGenerate(t, []string{migDir})
  if len(files) == 0 {
    t.Fatal("expected generated migration files, got none")
  }

  tofuInit(t, networkingDir)
  tofuApply(t, networkingDir, vars)
  tofuApply(t, sharedDir, vars)

  assertResourceInState(t, networkingDir, "random_id.moved")
  assertResourceNotInState(t, sharedDir, "random_id.moved")

  cleanupMigrationFiles(t, sharedDir)
  cleanupMigrationFiles(t, networkingDir)
  assertCleanPlan(t, sharedDir, vars)
  assertCleanPlan(t, networkingDir, vars)
}

// TestE2EFast_ModuleMove tests moving a module between layers.
func TestE2EFast_ModuleMove(t *testing.T) {
  t.Parallel()
  rootDir, _, vars := setupFastProject(t)

  sharedDir := filepath.Join(rootDir, "layers", "shared")
  appDir := filepath.Join(rootDir, "layers", "app")

  updateTfFile(t, sharedDir, "main.tf", `
terraform {
  required_providers {
    random = {
      source = "hashicorp/random"
    }
  }
}

provider "random" {}

module "mod_null" {
  source = "../../modules/nullmod"
  prefix = var.prefix
  name   = "module"
}
`)

  tofuInit(t, sharedDir)
  tofuApply(t, sharedDir, vars)
  t.Cleanup(func() {
    tofuDestroy(t, appDir, vars)
    tofuDestroy(t, sharedDir, vars)
  })

  assertResourceInState(t, sharedDir, "module.mod_null.random_id.unit")

  migDir := writeMigration(t, rootDir, "001_move_module.yaml", fmt.Sprintf(`
description: "Move random module from shared to app"
operations:
  - type: move
    source_layer: "%s"
    destination_layer: "%s"
    resources:
      - from: "module.mod_null"
`, sharedDir, appDir))

  updateTfFile(t, appDir, "main.tf", `
terraform {
  required_providers {
    random = {
      source = "hashicorp/random"
    }
  }
}

provider "random" {}

module "mod_null" {
  source = "../../modules/nullmod"
  prefix = var.prefix
  name   = "module"
}
`)

  updateTfFile(t, sharedDir, "main.tf", `
terraform {
  required_providers {
    random = {
      source = "hashicorp/random"
    }
  }
}

provider "random" {}
`)

  files := runGenerate(t, []string{migDir})
  if len(files) == 0 {
    t.Fatal("expected generated migration files, got none")
  }
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
  assertFileContains(t, sharedMigration, "from = module.mod_null")

  tofuInit(t, appDir)
  tofuApply(t, appDir, vars)
  tofuApply(t, sharedDir, vars)

  assertResourceInState(t, appDir, "module.mod_null.random_id.unit")
  assertResourceNotInState(t, sharedDir, "module.mod_null.random_id.unit")

  cleanupMigrationFiles(t, sharedDir)
  cleanupMigrationFiles(t, appDir)
  assertCleanPlan(t, sharedDir, vars)
  assertCleanPlan(t, appDir, vars)
}

// TestE2EFast_AllResourcesMoveWithOverridesOmit tests bulk move with overrides and omit.
func TestE2EFast_AllResourcesMoveWithOverridesOmit(t *testing.T) {
  t.Parallel()
  rootDir, _, vars := setupFastProject(t)

  sharedDir := filepath.Join(rootDir, "layers", "shared")
  appDir := filepath.Join(rootDir, "layers", "app")

  updateTfFile(t, sharedDir, "main.tf", `
terraform {
  required_providers {
    random = {
      source = "hashicorp/random"
    }
  }
}

provider "random" {}

resource "random_id" "bulk" {
  byte_length = 4
  keepers = {
    prefix = var.prefix
    name   = "bulk"
  }
}

resource "random_id" "omit" {
  byte_length = 4
  keepers = {
    prefix = var.prefix
    name   = "omit"
  }
}
`)

  tofuInit(t, sharedDir)
  tofuApply(t, sharedDir, vars)
  t.Cleanup(func() {
    tofuDestroy(t, appDir, vars)
    tofuDestroy(t, sharedDir, vars)
  })

  assertResourceInState(t, sharedDir, "random_id.bulk")
  assertResourceInState(t, sharedDir, "random_id.omit")

  migDir := writeMigration(t, rootDir, "001_move_all.yaml", fmt.Sprintf(`
description: "Move all resources with overrides and omit"
operations:
  - type: move
    source_layer: "%s"
    destination_layer: "%s"
    all_resources: true
    overrides:
      - from: "random_id.bulk"
        to: "random_id.renamed"
    omit:
      - address: "random_id.omit"
        destroy: true
`, sharedDir, appDir))

  updateTfFile(t, appDir, "main.tf", `
terraform {
  required_providers {
    random = {
      source = "hashicorp/random"
    }
  }
}

provider "random" {}

resource "random_id" "renamed" {
  byte_length = 4
  keepers = {
    prefix = var.prefix
    name   = "bulk"
  }
}
`)

  updateTfFile(t, sharedDir, "main.tf", `
terraform {
  required_providers {
    random = {
      source = "hashicorp/random"
    }
  }
}

provider "random" {}
`)

  files := runGenerate(t, []string{migDir})
  if len(files) == 0 {
    t.Fatal("expected generated migration files, got none")
  }

  tofuInit(t, appDir)
  tofuApply(t, appDir, vars)
  tofuApply(t, sharedDir, vars)

  assertResourceInState(t, appDir, "random_id.renamed")
  assertResourceNotInState(t, appDir, "random_id.omit")
  assertResourceNotInState(t, sharedDir, "random_id.bulk")

  cleanupMigrationFiles(t, sharedDir)
  cleanupMigrationFiles(t, appDir)
  assertCleanPlan(t, sharedDir, vars)
  assertCleanPlan(t, appDir, vars)
}

// TestE2EFast_KeyPatternMove tests key pattern mapping with prefix and catch-all rules.
func TestE2EFast_KeyPatternMove(t *testing.T) {
  t.Parallel()
  rootDir, _, vars := setupFastProject(t)

  sharedDir := filepath.Join(rootDir, "layers", "shared")
  appDir := filepath.Join(rootDir, "layers", "app")

  updateTfFile(t, sharedDir, "main.tf", `
terraform {
  required_providers {
    random = {
      source = "hashicorp/random"
    }
  }
}

provider "random" {}

resource "random_id" "nsgs" {
  for_each = toset(["app_alpha", "app_beta", "core_gamma"])
  byte_length = 4
  keepers = {
    prefix = var.prefix
    key    = each.key
  }
}
`)

  tofuInit(t, sharedDir)
  tofuApply(t, sharedDir, vars)
  t.Cleanup(func() {
    tofuDestroy(t, appDir, vars)
    tofuDestroy(t, sharedDir, vars)
  })

  migDir := writeMigration(t, rootDir, "001_move_key_patterns.yaml", fmt.Sprintf(`
description: "Move random resources with key patterns"
operations:
  - type: move
    source_layer: "%s"
    destination_layer: "%s"
    resources:
      - from: "random_id.nsgs"
        keys:
          "app_*": '{{ .Key | trimPrefix "app_" }}'
          "*": '{{ .Key }}'
`, sharedDir, appDir))

  updateTfFile(t, appDir, "main.tf", `
terraform {
  required_providers {
    random = {
      source = "hashicorp/random"
    }
  }
}

provider "random" {}

resource "random_id" "nsgs" {
  for_each = {
    alpha      = "app_alpha"
    beta       = "app_beta"
    core_gamma = "core_gamma"
  }
  byte_length = 4
  keepers = {
    prefix = var.prefix
    key    = each.value
  }
}
`)

  updateTfFile(t, sharedDir, "main.tf", `
terraform {
  required_providers {
    random = {
      source = "hashicorp/random"
    }
  }
}

provider "random" {}
`)

  files := runGenerate(t, []string{migDir})
  if len(files) == 0 {
    t.Fatal("expected generated migration files, got none")
  }

  tofuInit(t, appDir)
  tofuApply(t, appDir, vars)
  tofuApply(t, sharedDir, vars)

  for _, key := range []string{"alpha", "beta", "core_gamma"} {
    assertResourceInState(t, appDir, fmt.Sprintf(`random_id.nsgs["%s"]`, key))
  }

  cleanupMigrationFiles(t, sharedDir)
  cleanupMigrationFiles(t, appDir)
  assertCleanPlan(t, sharedDir, vars)
  assertCleanPlan(t, appDir, vars)
}

// TestE2EFast_RenameResource tests renaming a resource within a single layer.
func TestE2EFast_RenameResource(t *testing.T) {
  t.Parallel()
  rootDir, _, vars := setupFastProject(t)

  sharedDir := filepath.Join(rootDir, "layers", "shared")

  updateTfFile(t, sharedDir, "main.tf", `
terraform {
  required_providers {
    random = {
      source = "hashicorp/random"
    }
  }
}

provider "random" {}

resource "random_id" "importable" {
  byte_length = 4
  keepers = {
    prefix = var.prefix
    name   = "importable"
  }
}
`)

  tofuInit(t, sharedDir)
  tofuApply(t, sharedDir, vars)
  t.Cleanup(func() {
    tofuDestroy(t, sharedDir, vars)
  })

  assertResourceInState(t, sharedDir, "random_id.importable")

  migDir := writeMigration(t, rootDir, "001_rename_random.yaml", fmt.Sprintf(`
description: "Rename random_id importable"
operations:
  - type: rename
    layer: "%s"
    renames:
      - from: "random_id.importable"
        to: "random_id.renamed"
`, sharedDir))

  updateTfFile(t, sharedDir, "main.tf", `
terraform {
  required_providers {
    random = {
      source = "hashicorp/random"
    }
  }
}

provider "random" {}

resource "random_id" "renamed" {
  byte_length = 4
  keepers = {
    prefix = var.prefix
    name   = "importable"
  }
}
`)

  files := runGenerate(t, []string{migDir})
  if len(files) == 0 {
    t.Fatal("expected generated migration files, got none")
  }

  tofuApply(t, sharedDir, vars)

  assertResourceNotInState(t, sharedDir, "random_id.importable")
  assertResourceInState(t, sharedDir, "random_id.renamed")

  cleanupMigrationFiles(t, sharedDir)
  assertCleanPlan(t, sharedDir, vars)
}

// TestE2EFast_RemoveAndImport tests removing a resource from state and importing it back.
func TestE2EFast_RemoveAndImport(t *testing.T) {
  t.Parallel()
  rootDir, _, vars := setupFastProject(t)

  sharedDir := filepath.Join(rootDir, "layers", "shared")

  updateTfFile(t, sharedDir, "main.tf", `
terraform {
  required_providers {
    random = {
      source = "hashicorp/random"
    }
  }
}

provider "random" {}

resource "random_id" "importable" {
  byte_length = 4
  keepers = {
    prefix = var.prefix
    name   = "importable"
  }
}
`)

  tofuInit(t, sharedDir)
  tofuApply(t, sharedDir, vars)
  t.Cleanup(func() {
    tofuDestroy(t, sharedDir, vars)
  })

  assertResourceInState(t, sharedDir, "random_id.importable")

  // Remove phase
  importID := resourceAttribute(t, sharedDir, "random_id.importable", "id")
  migDir := writeMigration(t, rootDir, "001_remove_random.yaml", fmt.Sprintf(`
description: "Stop managing random_id importable"
operations:
  - type: remove
    layer: "%s"
    entries:
      - address: "random_id.importable"
`, sharedDir))

  updateTfFile(t, sharedDir, "main.tf", `
terraform {
  required_providers {
    random = {
      source = "hashicorp/random"
    }
  }
}

provider "random" {}
`)

  files := runGenerate(t, []string{migDir})
  if len(files) == 0 {
    t.Fatal("expected generated migration files for remove, got none")
  }

  tofuApply(t, sharedDir, vars)

  assertResourceNotInState(t, sharedDir, "random_id.importable")

  cleanupMigrationFiles(t, sharedDir)
  assertCleanPlan(t, sharedDir, vars)

  // Import phase
  os.RemoveAll(filepath.Join(rootDir, "migrations"))
  migDir = writeMigration(t, rootDir, "002_import_random.yaml", fmt.Sprintf(`
description: "Import random_id importable"
operations:
  - type: import
    layer: "%s"
    imports:
      - address: "random_id.importable"
        id: "%s"
`, sharedDir, importID))

  updateTfFile(t, sharedDir, "main.tf", `
terraform {
  required_providers {
    random = {
      source = "hashicorp/random"
    }
  }
}

provider "random" {}

resource "random_id" "importable" {
  byte_length = 4
  keepers = {
    prefix = var.prefix
    name   = "importable"
  }
}
`)

  files = runGenerate(t, []string{migDir})
  if len(files) == 0 {
    t.Fatal("expected generated migration files for import, got none")
  }

  tofuApply(t, sharedDir, vars)

  assertResourceInState(t, sharedDir, "random_id.importable")

  cleanupMigrationFiles(t, sharedDir)
  assertCleanPlan(t, sharedDir, vars)
}

// TestE2EFast_KeyedMove tests moving for_each resources with exact key remapping.
func TestE2EFast_KeyedMove(t *testing.T) {
  t.Parallel()
  rootDir, _, vars := setupFastProject(t)

  sharedDir := filepath.Join(rootDir, "layers", "shared")
  appDir := filepath.Join(rootDir, "layers", "app")

  updateTfFile(t, sharedDir, "main.tf", `
terraform {
  required_providers {
    random = {
      source = "hashicorp/random"
    }
  }
}

provider "random" {}

resource "random_id" "items" {
  for_each = toset(["alpha", "beta", "gamma"])
  byte_length = 4
  keepers = {
    prefix = var.prefix
    key    = each.key
  }
}
`)

  tofuInit(t, sharedDir)
  tofuApply(t, sharedDir, vars)
  t.Cleanup(func() {
    tofuDestroy(t, appDir, vars)
    tofuDestroy(t, sharedDir, vars)
  })

  for _, key := range []string{"alpha", "beta", "gamma"} {
    assertResourceInState(t, sharedDir, fmt.Sprintf(`random_id.items["%s"]`, key))
  }

  migDir := writeMigration(t, rootDir, "001_keyed_move.yaml", fmt.Sprintf(`
description: "Move random_id items with key remapping"
operations:
  - type: move
    source_layer: "%s"
    destination_layer: "%s"
    resources:
      - from: "random_id.items"
        keys:
          alpha: app_alpha
          beta: app_beta
          gamma: app_gamma
`, sharedDir, appDir))

  updateTfFile(t, appDir, "main.tf", `
terraform {
  required_providers {
    random = {
      source = "hashicorp/random"
    }
  }
}

provider "random" {}

resource "random_id" "items" {
  for_each = toset(["app_alpha", "app_beta", "app_gamma"])
  byte_length = 4
  keepers = {
    prefix = var.prefix
    key    = each.key
  }
}
`)

  updateTfFile(t, sharedDir, "main.tf", `
terraform {
  required_providers {
    random = {
      source = "hashicorp/random"
    }
  }
}

provider "random" {}
`)

  files := runGenerate(t, []string{migDir})
  if len(files) == 0 {
    t.Fatal("expected generated migration files, got none")
  }

  tofuInit(t, appDir)
  tofuApply(t, appDir, vars)
  tofuApply(t, sharedDir, vars)

  for _, key := range []string{"app_alpha", "app_beta", "app_gamma"} {
    assertResourceInState(t, appDir, fmt.Sprintf(`random_id.items["%s"]`, key))
  }

  cleanupMigrationFiles(t, sharedDir)
  cleanupMigrationFiles(t, appDir)
  assertCleanPlan(t, sharedDir, vars)
  assertCleanPlan(t, appDir, vars)
}

// TestE2EFast_MoveWithAddressRename tests moving a resource while changing its address.
func TestE2EFast_MoveWithAddressRename(t *testing.T) {
  t.Parallel()
  rootDir, _, vars := setupFastProject(t)

  sharedDir := filepath.Join(rootDir, "layers", "shared")
  appDir := filepath.Join(rootDir, "layers", "app")

  updateTfFile(t, sharedDir, "main.tf", `
terraform {
  required_providers {
    random = {
      source = "hashicorp/random"
    }
  }
}

provider "random" {}

resource "random_id" "old_name" {
  byte_length = 4
  keepers = {
    prefix = var.prefix
    name   = "renamed"
  }
}
`)

  tofuInit(t, sharedDir)
  tofuApply(t, sharedDir, vars)
  t.Cleanup(func() {
    tofuDestroy(t, appDir, vars)
    tofuDestroy(t, sharedDir, vars)
  })

  assertResourceInState(t, sharedDir, "random_id.old_name")

  migDir := writeMigration(t, rootDir, "001_move_rename.yaml", fmt.Sprintf(`
description: "Move random_id and rename it"
operations:
  - type: move
    source_layer: "%s"
    destination_layer: "%s"
    resources:
      - from: "random_id.old_name"
        to: "random_id.new_name"
`, sharedDir, appDir))

  updateTfFile(t, appDir, "main.tf", `
terraform {
  required_providers {
    random = {
      source = "hashicorp/random"
    }
  }
}

provider "random" {}

resource "random_id" "new_name" {
  byte_length = 4
  keepers = {
    prefix = var.prefix
    name   = "renamed"
  }
}
`)

  updateTfFile(t, sharedDir, "main.tf", `
terraform {
  required_providers {
    random = {
      source = "hashicorp/random"
    }
  }
}

provider "random" {}
`)

  files := runGenerate(t, []string{migDir})
  if len(files) == 0 {
    t.Fatal("expected generated migration files, got none")
  }

  tofuInit(t, appDir)
  tofuApply(t, appDir, vars)
  tofuApply(t, sharedDir, vars)

  assertResourceInState(t, appDir, "random_id.new_name")
  assertResourceNotInState(t, sharedDir, "random_id.old_name")

  cleanupMigrationFiles(t, sharedDir)
  cleanupMigrationFiles(t, appDir)
  assertCleanPlan(t, sharedDir, vars)
  assertCleanPlan(t, appDir, vars)
}

// TestE2EFast_AddressPrefix tests using address_prefix to factor out common module paths.
func TestE2EFast_AddressPrefix(t *testing.T) {
  t.Parallel()
  rootDir, _, vars := setupFastProject(t)

  sharedDir := filepath.Join(rootDir, "layers", "shared")
  appDir := filepath.Join(rootDir, "layers", "app")

  updateTfFile(t, sharedDir, "main.tf", `
terraform {
  required_providers {
    random = {
      source = "hashicorp/random"
    }
  }
}

provider "random" {}

module "wrapper" {
  source = "../../modules/nullmod"
  prefix = var.prefix
  name   = "prefixed"
}
`)

  tofuInit(t, sharedDir)
  tofuApply(t, sharedDir, vars)
  t.Cleanup(func() {
    tofuDestroy(t, appDir, vars)
    tofuDestroy(t, sharedDir, vars)
  })

  assertResourceInState(t, sharedDir, "module.wrapper.random_id.unit")

  migDir := writeMigration(t, rootDir, "001_prefix_move.yaml", fmt.Sprintf(`
description: "Move resource using address_prefix"
operations:
  - type: move
    source_layer: "%s"
    destination_layer: "%s"
    address_prefix: "module.wrapper"
    resources:
      - from: "random_id.unit"
`, sharedDir, appDir))

  updateTfFile(t, appDir, "main.tf", `
terraform {
  required_providers {
    random = {
      source = "hashicorp/random"
    }
  }
}

provider "random" {}

module "wrapper" {
  source = "../../modules/nullmod"
  prefix = var.prefix
  name   = "prefixed"
}
`)

  updateTfFile(t, sharedDir, "main.tf", `
terraform {
  required_providers {
    random = {
      source = "hashicorp/random"
    }
  }
}

provider "random" {}
`)

  files := runGenerate(t, []string{migDir})
  if len(files) == 0 {
    t.Fatal("expected generated migration files, got none")
  }

  tofuInit(t, appDir)
  tofuApply(t, appDir, vars)
  tofuApply(t, sharedDir, vars)

  assertResourceInState(t, appDir, "module.wrapper.random_id.unit")
  assertResourceNotInState(t, sharedDir, "module.wrapper.random_id.unit")

  cleanupMigrationFiles(t, sharedDir)
  cleanupMigrationFiles(t, appDir)
  assertCleanPlan(t, sharedDir, vars)
  assertCleanPlan(t, appDir, vars)
}

// TestE2EFast_ConditionSkip tests that migrations are skipped when conditions are not met,
// and proceed when conditions are satisfied.
func TestE2EFast_ConditionSkip(t *testing.T) {
  t.Parallel()
  rootDir, _, vars := setupFastProject(t)

  sharedDir := filepath.Join(rootDir, "layers", "shared")

  updateTfFile(t, sharedDir, "main.tf", `
terraform {
  required_providers {
    random = {
      source = "hashicorp/random"
    }
  }
}

provider "random" {}

resource "random_id" "conditioned" {
  byte_length = 4
  keepers = {
    prefix = var.prefix
    name   = "conditioned"
  }
}
`)

  tofuInit(t, sharedDir)
  tofuApply(t, sharedDir, vars)
  t.Cleanup(func() {
    tofuDestroy(t, sharedDir, vars)
  })

  assertResourceInState(t, sharedDir, "random_id.conditioned")

  // Migration with unmet condition (resource that doesn't exist)
  migDir := writeMigration(t, rootDir, "001_skip.yaml", fmt.Sprintf(`
description: "Should be skipped — unmet condition"
condition:
  resources_exist:
    - layer: "%s"
      addresses:
        - "random_id.nonexistent"
operations:
  - type: remove
    layer: "%s"
    entries:
      - address: "random_id.conditioned"
`, sharedDir, sharedDir))

  result := runGenerateResult(t, []string{migDir})
  if len(result.OutputFiles) != 0 {
    t.Errorf("expected no generated files (condition not met), got %d: %v", len(result.OutputFiles), result.OutputFiles)
  }
  if len(result.SkippedFiles) == 0 {
    t.Fatal("expected skipped files recorded, got none")
  }
  if result.SkippedFiles[0].Reason != engine.SkipCondition {
    t.Errorf("expected skip reason %d, got %d", engine.SkipCondition, result.SkippedFiles[0].Reason)
  }

  assertResourceInState(t, sharedDir, "random_id.conditioned")

  // Migration with met condition
  writeMigration(t, rootDir, "002_proceed.yaml", fmt.Sprintf(`
description: "Should proceed — condition is met"
condition:
  resources_exist:
    - layer: "%s"
      addresses:
        - "random_id.conditioned"
operations:
  - type: rename
    layer: "%s"
    renames:
      - from: "random_id.conditioned"
        to: "random_id.processed"
`, sharedDir, sharedDir))

  updateTfFile(t, sharedDir, "main.tf", `
terraform {
  required_providers {
    random = {
      source = "hashicorp/random"
    }
  }
}

provider "random" {}

resource "random_id" "processed" {
  byte_length = 4
  keepers = {
    prefix = var.prefix
    name   = "conditioned"
  }
}
`)

  cleanupMigrationFiles(t, sharedDir)

  result = runGenerateResult(t, []string{migDir})
  if len(result.OutputFiles) == 0 {
    t.Fatal("expected generated migration files for met condition, got none")
  }

  tofuApply(t, sharedDir, vars)

  assertResourceInState(t, sharedDir, "random_id.processed")
  assertResourceNotInState(t, sharedDir, "random_id.conditioned")

  cleanupMigrationFiles(t, sharedDir)
  assertCleanPlan(t, sharedDir, vars)
}

// TestE2EFast_ConditionLayerExists tests layer_exists and layer_not_exists conditions.
func TestE2EFast_ConditionLayerExists(t *testing.T) {
  t.Parallel()
  rootDir, _, vars := setupFastProject(t)

  sharedDir := filepath.Join(rootDir, "layers", "shared")
  nonexistentDir := filepath.Join(rootDir, "layers", "deleted")

  updateTfFile(t, sharedDir, "main.tf", `
terraform {
  required_providers {
    random = {
      source = "hashicorp/random"
    }
  }
}

provider "random" {}

resource "random_id" "layer_check" {
  byte_length = 4
  keepers = {
    prefix = var.prefix
    name   = "layer_check"
  }
}
`)

  tofuInit(t, sharedDir)
  tofuApply(t, sharedDir, vars)
  t.Cleanup(func() {
    tofuDestroy(t, sharedDir, vars)
  })

  // Should skip: requires a non-existent layer to exist
  migDir := writeMigration(t, rootDir, "001_layer_missing.yaml", fmt.Sprintf(`
description: "Should skip — referencing non-existent layer"
condition:
  layer_exists:
    - "%s"
operations:
  - type: remove
    layer: "%s"
    entries:
      - address: "random_id.layer_check"
`, nonexistentDir, sharedDir))

  result := runGenerateResult(t, []string{migDir})
  if len(result.OutputFiles) != 0 {
    t.Errorf("expected no output (layer_exists not met), got %d files", len(result.OutputFiles))
  }

  assertResourceInState(t, sharedDir, "random_id.layer_check")

  // Should proceed: requires existing layer to exist
  os.RemoveAll(filepath.Join(rootDir, "migrations"))
  migDir = writeMigration(t, rootDir, "002_layer_present.yaml", fmt.Sprintf(`
description: "Should skip if deleted layer exists"
condition:
  layer_not_exists:
    - "%s"
  resources_exist:
    - layer: "%s"
      addresses:
        - "random_id.layer_check"
operations:
  - type: rename
    layer: "%s"
    renames:
      - from: "random_id.layer_check"
        to: "random_id.layer_ok"
`, nonexistentDir, sharedDir, sharedDir))

  updateTfFile(t, sharedDir, "main.tf", `
terraform {
  required_providers {
    random = {
      source = "hashicorp/random"
    }
  }
}

provider "random" {}

resource "random_id" "layer_ok" {
  byte_length = 4
  keepers = {
    prefix = var.prefix
    name   = "layer_check"
  }
}
`)

  result = runGenerateResult(t, []string{migDir})
  if len(result.OutputFiles) == 0 {
    t.Fatal("expected generated files (layer_not_exists met), got none")
  }

  tofuApply(t, sharedDir, vars)

  assertResourceInState(t, sharedDir, "random_id.layer_ok")

  cleanupMigrationFiles(t, sharedDir)
  assertCleanPlan(t, sharedDir, vars)
}

// TestE2EFast_RetiredStatus tests that migrations with status: retired are skipped entirely.
func TestE2EFast_RetiredStatus(t *testing.T) {
  t.Parallel()
  rootDir, _, vars := setupFastProject(t)

  sharedDir := filepath.Join(rootDir, "layers", "shared")

  updateTfFile(t, sharedDir, "main.tf", `
terraform {
  required_providers {
    random = {
      source = "hashicorp/random"
    }
  }
}

provider "random" {}

resource "random_id" "keep" {
  byte_length = 4
  keepers = {
    prefix = var.prefix
    name   = "keep"
  }
}
`)

  tofuInit(t, sharedDir)
  tofuApply(t, sharedDir, vars)
  t.Cleanup(func() {
    tofuDestroy(t, sharedDir, vars)
  })

  migDir := writeMigration(t, rootDir, "001_retired.yaml", fmt.Sprintf(`
description: "This migration is retired and should be skipped"
status: retired
operations:
  - type: remove
    layer: "%s"
    entries:
      - address: "random_id.keep"
`, sharedDir))

  result := runGenerateResult(t, []string{migDir})
  if len(result.OutputFiles) != 0 {
    t.Errorf("expected no output for retired migration, got %d files", len(result.OutputFiles))
  }
  if len(result.SkippedFiles) == 0 {
    t.Fatal("expected skipped file recorded for retired migration")
  }
  if result.SkippedFiles[0].Reason != engine.SkipRetired {
    t.Errorf("expected skip reason %d, got %d", engine.SkipRetired, result.SkippedFiles[0].Reason)
  }

  // Resource should still be in state (migration was not processed)
  assertResourceInState(t, sharedDir, "random_id.keep")
  assertCleanPlan(t, sharedDir, vars)
}

// TestE2EFast_Idempotency tests that re-running the same migration after it was applied
// is handled gracefully. Auto-inferred conditions make downloads idempotent, but at
// generate time the engine detects the source resource is gone and skips the file.
// This test also verifies the recommended pattern: using explicit conditions in the YAML
// to make generate idempotent without errors.
func TestE2EFast_Idempotency(t *testing.T) {
  t.Parallel()
  rootDir, _, vars := setupFastProject(t)

  sharedDir := filepath.Join(rootDir, "layers", "shared")
  appDir := filepath.Join(rootDir, "layers", "app")

  updateTfFile(t, sharedDir, "main.tf", `
terraform {
  required_providers {
    random = {
      source = "hashicorp/random"
    }
  }
}

provider "random" {}

resource "random_id" "idempotent" {
  byte_length = 4
  keepers = {
    prefix = var.prefix
    name   = "idempotent"
  }
}
`)

  tofuInit(t, sharedDir)
  tofuApply(t, sharedDir, vars)
  t.Cleanup(func() {
    tofuDestroy(t, appDir, vars)
    tofuDestroy(t, sharedDir, vars)
  })

  // Use explicit conditions so generate is truly idempotent (skips cleanly via SkipCondition).
  migDir := writeMigration(t, rootDir, "001_idempotent.yaml", fmt.Sprintf(`
description: "Move resource for idempotency test"
condition:
  resources_exist:
    - layer: "%s"
      addresses:
        - "random_id.idempotent"
operations:
  - type: move
    source_layer: "%s"
    destination_layer: "%s"
    resources:
      - from: "random_id.idempotent"
`, sharedDir, sharedDir, appDir))

  updateTfFile(t, appDir, "main.tf", `
terraform {
  required_providers {
    random = {
      source = "hashicorp/random"
    }
  }
}

provider "random" {}

resource "random_id" "idempotent" {
  byte_length = 4
  keepers = {
    prefix = var.prefix
    name   = "idempotent"
  }
}
`)

  updateTfFile(t, sharedDir, "main.tf", `
terraform {
  required_providers {
    random = {
      source = "hashicorp/random"
    }
  }
}

provider "random" {}
`)

  // First run: condition met, should generate migration files
  files := runGenerate(t, []string{migDir})
  if len(files) == 0 {
    t.Fatal("expected generated migration files on first run, got none")
  }

  tofuInit(t, appDir)
  tofuApply(t, appDir, vars)
  tofuApply(t, sharedDir, vars)

  assertResourceInState(t, appDir, "random_id.idempotent")
  assertResourceNotInState(t, sharedDir, "random_id.idempotent")

  cleanupMigrationFiles(t, sharedDir)
  cleanupMigrationFiles(t, appDir)

  // Second run: explicit condition detects source resource is gone, skips cleanly
  result := runGenerateResult(t, []string{migDir})
  if len(result.OutputFiles) != 0 {
    t.Errorf("expected no output on idempotent re-run, got %d files: %v", len(result.OutputFiles), result.OutputFiles)
  }
  // Verify it was skipped due to condition, not an error
  if len(result.SkippedFiles) == 0 {
    t.Fatal("expected skipped files on idempotent re-run")
  }
  if result.SkippedFiles[0].Reason != engine.SkipCondition {
    t.Errorf("expected skip reason SkipCondition (%d), got %d", engine.SkipCondition, result.SkippedFiles[0].Reason)
  }

  // State should remain unchanged
  assertResourceInState(t, appDir, "random_id.idempotent")
  assertCleanPlan(t, sharedDir, vars)
  assertCleanPlan(t, appDir, vars)
}

// TestE2EFast_MultiFileMigration tests processing multiple migration YAML files
// in a single directory, including proper ordering and independent processing.
func TestE2EFast_MultiFileMigration(t *testing.T) {
  t.Parallel()
  rootDir, _, vars := setupFastProject(t)

  sharedDir := filepath.Join(rootDir, "layers", "shared")

  updateTfFile(t, sharedDir, "main.tf", `
terraform {
  required_providers {
    random = {
      source = "hashicorp/random"
    }
  }
}

provider "random" {}

resource "random_id" "first" {
  byte_length = 4
  keepers = {
    prefix = var.prefix
    name   = "first"
  }
}

resource "random_id" "second" {
  byte_length = 4
  keepers = {
    prefix = var.prefix
    name   = "second"
  }
}
`)

  tofuInit(t, sharedDir)
  tofuApply(t, sharedDir, vars)
  t.Cleanup(func() {
    tofuDestroy(t, sharedDir, vars)
  })

  // Two independent rename migrations in the same directory
  migDir := writeMigration(t, rootDir, "001_rename_first.yaml", fmt.Sprintf(`
description: "Rename first resource"
operations:
  - type: rename
    layer: "%s"
    renames:
      - from: "random_id.first"
        to: "random_id.alpha"
`, sharedDir))

  writeMigration(t, rootDir, "002_rename_second.yaml", fmt.Sprintf(`
description: "Rename second resource"
operations:
  - type: rename
    layer: "%s"
    renames:
      - from: "random_id.second"
        to: "random_id.beta"
`, sharedDir))

  updateTfFile(t, sharedDir, "main.tf", `
terraform {
  required_providers {
    random = {
      source = "hashicorp/random"
    }
  }
}

provider "random" {}

resource "random_id" "alpha" {
  byte_length = 4
  keepers = {
    prefix = var.prefix
    name   = "first"
  }
}

resource "random_id" "beta" {
  byte_length = 4
  keepers = {
    prefix = var.prefix
    name   = "second"
  }
}
`)

  files := runGenerate(t, []string{migDir})
  if len(files) < 2 {
    t.Fatalf("expected at least 2 generated migration files, got %d: %v", len(files), files)
  }

  tofuApply(t, sharedDir, vars)

  assertResourceInState(t, sharedDir, "random_id.alpha")
  assertResourceInState(t, sharedDir, "random_id.beta")
  assertResourceNotInState(t, sharedDir, "random_id.first")
  assertResourceNotInState(t, sharedDir, "random_id.second")

  cleanupMigrationFiles(t, sharedDir)
  assertCleanPlan(t, sharedDir, vars)
}

// TestE2EFast_ImportWithTemplateID tests the import operation with a Go template
// expression for the import ID, simulating composite ID construction from state attributes.
func TestE2EFast_ImportWithTemplateID(t *testing.T) {
  t.Parallel()
  rootDir, _, vars := setupFastProject(t)

  sharedDir := filepath.Join(rootDir, "layers", "shared")
  appDir := filepath.Join(rootDir, "layers", "app")

  updateTfFile(t, sharedDir, "main.tf", `
terraform {
  required_providers {
    random = {
      source = "hashicorp/random"
    }
  }
}

provider "random" {}

resource "random_id" "composite" {
  byte_length = 4
  keepers = {
    prefix = var.prefix
    name   = "composite"
  }
}
`)

  tofuInit(t, sharedDir)
  tofuApply(t, sharedDir, vars)
  t.Cleanup(func() {
    tofuDestroy(t, appDir, vars)
    tofuDestroy(t, sharedDir, vars)
  })

  assertResourceInState(t, sharedDir, "random_id.composite")

  // Move with import_id using template to construct ID from state attributes.
  // For random_id, the import ID is the "id" attribute (base64url encoded).
  // Using {{ .Attributes.id }} tests the template engine with real state data.
  migDir := writeMigration(t, rootDir, "001_template_import.yaml", fmt.Sprintf(`
description: "Move with template import_id"
operations:
  - type: move
    source_layer: "%s"
    destination_layer: "%s"
    resources:
      - from: "random_id.composite"
        import_id: '{{ .Attributes.id }}'
`, sharedDir, appDir))

  updateTfFile(t, appDir, "main.tf", `
terraform {
  required_providers {
    random = {
      source = "hashicorp/random"
    }
  }
}

provider "random" {}

resource "random_id" "composite" {
  byte_length = 4
  keepers = {
    prefix = var.prefix
    name   = "composite"
  }
}
`)

  updateTfFile(t, sharedDir, "main.tf", `
terraform {
  required_providers {
    random = {
      source = "hashicorp/random"
    }
  }
}

provider "random" {}
`)

  files := runGenerate(t, []string{migDir})
  if len(files) == 0 {
    t.Fatal("expected generated migration files, got none")
  }

  tofuInit(t, appDir)
  tofuApply(t, appDir, vars)
  tofuApply(t, sharedDir, vars)

  assertResourceInState(t, appDir, "random_id.composite")
  assertResourceNotInState(t, sharedDir, "random_id.composite")

  cleanupMigrationFiles(t, sharedDir)
  cleanupMigrationFiles(t, appDir)
  assertCleanPlan(t, sharedDir, vars)
  assertCleanPlan(t, appDir, vars)
}

// TestE2EFast_MoveForEachWithoutKeys tests moving all instances of a for_each
// resource without explicit key mapping (preserves all keys as-is).
func TestE2EFast_MoveForEachWithoutKeys(t *testing.T) {
  t.Parallel()
  rootDir, _, vars := setupFastProject(t)

  sharedDir := filepath.Join(rootDir, "layers", "shared")
  appDir := filepath.Join(rootDir, "layers", "app")

  updateTfFile(t, sharedDir, "main.tf", `
terraform {
  required_providers {
    random = {
      source = "hashicorp/random"
    }
  }
}

provider "random" {}

resource "random_id" "things" {
  for_each = toset(["x", "y", "z"])
  byte_length = 4
  keepers = {
    prefix = var.prefix
    key    = each.key
  }
}
`)

  tofuInit(t, sharedDir)
  tofuApply(t, sharedDir, vars)
  t.Cleanup(func() {
    tofuDestroy(t, appDir, vars)
    tofuDestroy(t, sharedDir, vars)
  })

  migDir := writeMigration(t, rootDir, "001_move_foreach.yaml", fmt.Sprintf(`
description: "Move all for_each instances without key mapping"
operations:
  - type: move
    source_layer: "%s"
    destination_layer: "%s"
    resources:
      - from: "random_id.things"
`, sharedDir, appDir))

  updateTfFile(t, appDir, "main.tf", `
terraform {
  required_providers {
    random = {
      source = "hashicorp/random"
    }
  }
}

provider "random" {}

resource "random_id" "things" {
  for_each = toset(["x", "y", "z"])
  byte_length = 4
  keepers = {
    prefix = var.prefix
    key    = each.key
  }
}
`)

  updateTfFile(t, sharedDir, "main.tf", `
terraform {
  required_providers {
    random = {
      source = "hashicorp/random"
    }
  }
}

provider "random" {}
`)

  files := runGenerate(t, []string{migDir})
  if len(files) == 0 {
    t.Fatal("expected generated migration files, got none")
  }

  tofuInit(t, appDir)
  tofuApply(t, appDir, vars)
  tofuApply(t, sharedDir, vars)

  for _, key := range []string{"x", "y", "z"} {
    assertResourceInState(t, appDir, fmt.Sprintf(`random_id.things["%s"]`, key))
    assertResourceNotInState(t, sharedDir, fmt.Sprintf(`random_id.things["%s"]`, key))
  }

  cleanupMigrationFiles(t, sharedDir)
  cleanupMigrationFiles(t, appDir)
  assertCleanPlan(t, sharedDir, vars)
  assertCleanPlan(t, appDir, vars)
}

// TestE2EFast_SplitForEachToMultipleLayers tests routing different for_each keys
// to different destination layers using multiple move operations.
func TestE2EFast_SplitForEachToMultipleLayers(t *testing.T) {
  t.Parallel()
  rootDir, _, vars := setupFastProject(t)

  sharedDir := filepath.Join(rootDir, "layers", "shared")
  appDir := filepath.Join(rootDir, "layers", "app")
  networkingDir := filepath.Join(rootDir, "layers", "networking")

  updateTfFile(t, sharedDir, "main.tf", `
terraform {
  required_providers {
    random = {
      source = "hashicorp/random"
    }
  }
}

provider "random" {}

resource "random_id" "split" {
  for_each = toset(["app_one", "app_two", "net_one"])
  byte_length = 4
  keepers = {
    prefix = var.prefix
    key    = each.key
  }
}
`)

  tofuInit(t, sharedDir)
  tofuApply(t, sharedDir, vars)
  t.Cleanup(func() {
    tofuDestroy(t, networkingDir, vars)
    tofuDestroy(t, appDir, vars)
    tofuDestroy(t, sharedDir, vars)
  })

  migDir := writeMigration(t, rootDir, "001_split.yaml", fmt.Sprintf(`
description: "Split for_each keys to different layers"
operations:
  - type: move
    source_layer: "%s"
    destination_layer: "%s"
    resources:
      - from: "random_id.split"
        keys:
          "app_*": '{{ .Key | trimPrefix "app_" }}'
  - type: move
    source_layer: "%s"
    destination_layer: "%s"
    resources:
      - from: "random_id.split"
        keys:
          "net_*": '{{ .Key | trimPrefix "net_" }}'
`, sharedDir, appDir, sharedDir, networkingDir))

  updateTfFile(t, appDir, "main.tf", `
terraform {
  required_providers {
    random = {
      source = "hashicorp/random"
    }
  }
}

provider "random" {}

resource "random_id" "split" {
  for_each = toset(["one", "two"])
  byte_length = 4
  keepers = {
    prefix = var.prefix
    key    = each.key
  }
}
`)

  updateTfFile(t, networkingDir, "main.tf", `
terraform {
  required_providers {
    random = {
      source = "hashicorp/random"
    }
  }
}

provider "random" {}

resource "random_id" "split" {
  for_each = toset(["one"])
  byte_length = 4
  keepers = {
    prefix = var.prefix
    key    = each.key
  }
}
`)

  updateTfFile(t, sharedDir, "main.tf", `
terraform {
  required_providers {
    random = {
      source = "hashicorp/random"
    }
  }
}

provider "random" {}
`)

  files := runGenerate(t, []string{migDir})
  if len(files) == 0 {
    t.Fatal("expected generated migration files, got none")
  }

  tofuInit(t, appDir)
  tofuInit(t, networkingDir)
  tofuApply(t, appDir, vars)
  tofuApply(t, networkingDir, vars)
  tofuApply(t, sharedDir, vars)

  assertResourceInState(t, appDir, `random_id.split["one"]`)
  assertResourceInState(t, appDir, `random_id.split["two"]`)
  assertResourceInState(t, networkingDir, `random_id.split["one"]`)

  cleanupMigrationFiles(t, sharedDir)
  cleanupMigrationFiles(t, appDir)
  cleanupMigrationFiles(t, networkingDir)
  assertCleanPlan(t, sharedDir, vars)
  assertCleanPlan(t, appDir, vars)
  assertCleanPlan(t, networkingDir, vars)
}

// ---------------------------------------------------------------------------
// Azure Blob Storage persistence tests (skipped when E2E_STORAGE_ACCOUNT_NAME
// is not set)
// ---------------------------------------------------------------------------

// TestE2EFast_UploadDownload tests the full generate → upload → download → apply
// pipeline using real Azure Blob Storage.
func TestE2EFast_UploadDownload(t *testing.T) {
  t.Parallel()
  storageAccount := requireEnv(t, "E2E_STORAGE_ACCOUNT_NAME")
  ctx := context.Background()
  cred := getCredential(t)

  rootDir, prefix, vars := setupFastProject(t)
  containerName := prefix
  createContainer(t, ctx, cred, storageAccount, containerName)
  t.Cleanup(func() { deleteContainer(t, ctx, cred, storageAccount, containerName) })

  sharedDir := filepath.Join(rootDir, "layers", "shared")
  appDir := filepath.Join(rootDir, "layers", "app")
  initArgs := storageInitArgs(storageAccount, containerName)

  // Create resource in shared layer
  updateTfFile(t, sharedDir, "main.tf", `
terraform {
  required_providers {
    random = {
      source = "hashicorp/random"
    }
  }
}

provider "random" {}

resource "random_id" "upload_test" {
  byte_length = 4
  keepers = {
    prefix = var.prefix
    name   = "upload_test"
  }
}
`)

  tofuInit(t, sharedDir)
  tofuApply(t, sharedDir, vars)
  tofuInit(t, appDir)
  t.Cleanup(func() {
    tofuDestroy(t, appDir, vars)
    tofuDestroy(t, sharedDir, vars)
  })

  // Write move migration and generate
  migDir := writeMigration(t, rootDir, "001_upload.yaml", fmt.Sprintf(`
description: "Move resource for upload/download test"
operations:
  - type: move
    source_layer: "%s"
    destination_layer: "%s"
    resources:
      - from: "random_id.upload_test"
`, sharedDir, appDir))

  updateTfFile(t, appDir, "main.tf", `
terraform {
  required_providers {
    random = {
      source = "hashicorp/random"
    }
  }
}

provider "random" {}

resource "random_id" "upload_test" {
  byte_length = 4
  keepers = {
    prefix = var.prefix
    name   = "upload_test"
  }
}
`)

  updateTfFile(t, sharedDir, "main.tf", `
terraform {
  required_providers {
    random = {
      source = "hashicorp/random"
    }
  }
}

provider "random" {}
`)

  // Re-initialize app layer after updating main.tf to update lock file
  tofuInit(t, appDir)

  files := runGenerate(t, []string{migDir})
  if len(files) == 0 {
    t.Fatal("expected generated migration files")
  }

  // Upload generated files from the app layer
  mgr := upload.NewManager(cred, initArgs)
  if err := mgr.UploadFromDisk(ctx, []string{appDir}); err != nil {
    t.Fatalf("upload: %v", err)
  }

  // Also upload from shared layer (removed blocks)
  if err := mgr.UploadFromDisk(ctx, []string{sharedDir}); err != nil {
    t.Fatalf("upload shared: %v", err)
  }

  // Clean local migration files to simulate fresh download
  cleanupMigrationFiles(t, appDir)
  cleanupMigrationFiles(t, sharedDir)

  // Download into app layer
  dl := download.NewDownloader(cred, initArgs, tofuExecPath(t), false)
  downloaded, err := dl.Download(ctx, appDir)
  if err != nil {
    t.Fatalf("download: %v", err)
  }
  if len(downloaded) == 0 {
    t.Fatal("expected downloaded migration files, got none")
  }

  // Verify downloaded file contains import block
  for _, f := range downloaded {
    assertFileContains(t, f, "import")
  }

  // Apply the migration
  tofuInit(t, appDir)
  tofuApply(t, appDir, vars)
  tofuApply(t, sharedDir, vars)

  assertResourceInState(t, appDir, "random_id.upload_test")
  assertResourceNotInState(t, sharedDir, "random_id.upload_test")

  cleanupMigrationFiles(t, appDir)
  cleanupMigrationFiles(t, sharedDir)
  assertCleanPlan(t, sharedDir, vars)
  assertCleanPlan(t, appDir, vars)
}

// TestE2EFast_UploadGuard tests that the upload guard prevents overwriting
// active migrations and that --force bypasses the guard.
func TestE2EFast_UploadGuard(t *testing.T) {
  t.Parallel()
  storageAccount := requireEnv(t, "E2E_STORAGE_ACCOUNT_NAME")
  ctx := context.Background()
  cred := getCredential(t)

  rootDir, prefix, vars := setupFastProject(t)
  containerName := prefix
  createContainer(t, ctx, cred, storageAccount, containerName)
  t.Cleanup(func() { deleteContainer(t, ctx, cred, storageAccount, containerName) })

  sharedDir := filepath.Join(rootDir, "layers", "shared")
  appDir := filepath.Join(rootDir, "layers", "app")
  initArgs := storageInitArgs(storageAccount, containerName)

  // Create resource
  updateTfFile(t, sharedDir, "main.tf", `
terraform {
  required_providers {
    random = {
      source = "hashicorp/random"
    }
  }
}

provider "random" {}

resource "random_id" "guard_test" {
  byte_length = 4
  keepers = {
    prefix = var.prefix
    name   = "guard_test"
  }
}
`)

  tofuInit(t, sharedDir)
  tofuApply(t, sharedDir, vars)
  tofuInit(t, appDir)
  t.Cleanup(func() {
    tofuDestroy(t, appDir, vars)
    tofuDestroy(t, sharedDir, vars)
  })

  // Generate first migration
  migDir := writeMigration(t, rootDir, "001_guard.yaml", fmt.Sprintf(`
description: "Move resource for guard test"
operations:
  - type: move
    source_layer: "%s"
    destination_layer: "%s"
    resources:
      - from: "random_id.guard_test"
`, sharedDir, appDir))

  updateTfFile(t, appDir, "main.tf", `
terraform {
  required_providers {
    random = {
      source = "hashicorp/random"
    }
  }
}

provider "random" {}

resource "random_id" "guard_test" {
  byte_length = 4
  keepers = {
    prefix = var.prefix
    name   = "guard_test"
  }
}
`)

  updateTfFile(t, sharedDir, "main.tf", `
terraform {
  required_providers {
    random = {
      source = "hashicorp/random"
    }
  }
}

provider "random" {}
`)

  files := runGenerate(t, []string{migDir})
  if len(files) == 0 {
    t.Fatal("expected generated migration files")
  }

  // First upload: should succeed
  mgr := upload.NewManager(cred, initArgs,
    upload.WithTofuPath(tofuExecPath(t), initArgs),
  )
  if err := mgr.UploadFromDisk(ctx, []string{appDir}); err != nil {
    t.Fatalf("first upload: %v", err)
  }

  // Modify migration to produce different content hash
  cleanupMigrationFiles(t, appDir)
  cleanupMigrationFiles(t, sharedDir)
  writeMigration(t, rootDir, "001_guard.yaml", fmt.Sprintf(`
description: "Move resource for guard test (modified)"
operations:
  - type: move
    source_layer: "%s"
    destination_layer: "%s"
    resources:
      - from: "random_id.guard_test"
`, sharedDir, appDir))
  runGenerate(t, []string{migDir})

  // Second upload with guard: should be refused (migration still active)
  mgr2 := upload.NewManager(cred, initArgs,
    upload.WithTofuPath(tofuExecPath(t), initArgs),
  )
  err := mgr2.UploadFromDisk(ctx, []string{appDir})
  if err == nil {
    t.Fatal("expected upload guard to refuse overwrite, but upload succeeded")
  }
  if !strings.Contains(err.Error(), "refusing to overwrite") {
    t.Fatalf("expected 'refusing to overwrite' error, got: %v", err)
  }

  // Upload with --force: should succeed
  mgr3 := upload.NewManager(cred, initArgs,
    upload.WithTofuPath(tofuExecPath(t), initArgs),
    upload.WithForce(true),
  )
  if err := mgr3.UploadFromDisk(ctx, []string{appDir}); err != nil {
    t.Fatalf("force upload: %v", err)
  }
}

// TestE2EFast_DownloadConditionSkip tests that download skips migration files
// whose auto-inferred conditions are no longer met (migration already applied).
func TestE2EFast_DownloadConditionSkip(t *testing.T) {
  t.Parallel()
  storageAccount := requireEnv(t, "E2E_STORAGE_ACCOUNT_NAME")
  ctx := context.Background()
  cred := getCredential(t)

  rootDir, prefix, vars := setupFastProject(t)
  containerName := prefix
  createContainer(t, ctx, cred, storageAccount, containerName)
  t.Cleanup(func() { deleteContainer(t, ctx, cred, storageAccount, containerName) })

  sharedDir := filepath.Join(rootDir, "layers", "shared")
  appDir := filepath.Join(rootDir, "layers", "app")
  initArgs := storageInitArgs(storageAccount, containerName)

  // Create resource
  updateTfFile(t, sharedDir, "main.tf", `
terraform {
  required_providers {
    random = {
      source = "hashicorp/random"
    }
  }
}

provider "random" {}

resource "random_id" "condskip" {
  byte_length = 4
  keepers = {
    prefix = var.prefix
    name   = "condskip"
  }
}
`)

  tofuInit(t, sharedDir)
  tofuApply(t, sharedDir, vars)
  tofuInit(t, appDir)
  t.Cleanup(func() {
    tofuDestroy(t, appDir, vars)
    tofuDestroy(t, sharedDir, vars)
  })

  // Generate and upload the migration
  migDir := writeMigration(t, rootDir, "001_condskip.yaml", fmt.Sprintf(`
description: "Move for condition skip test"
operations:
  - type: move
    source_layer: "%s"
    destination_layer: "%s"
    resources:
      - from: "random_id.condskip"
`, sharedDir, appDir))

  updateTfFile(t, appDir, "main.tf", `
terraform {
  required_providers {
    random = {
      source = "hashicorp/random"
    }
  }
}

provider "random" {}

resource "random_id" "condskip" {
  byte_length = 4
  keepers = {
    prefix = var.prefix
    name   = "condskip"
  }
}
`)

  updateTfFile(t, sharedDir, "main.tf", `
terraform {
  required_providers {
    random = {
      source = "hashicorp/random"
    }
  }
}

provider "random" {}
`)

  files := runGenerate(t, []string{migDir})
  if len(files) == 0 {
    t.Fatal("expected generated migration files")
  }

  // Upload migration files from app layer (import blocks)
  mgr := upload.NewManager(cred, initArgs)
  if err := mgr.UploadFromDisk(ctx, []string{appDir}); err != nil {
    t.Fatalf("upload: %v", err)
  }

  // Apply the migration so the resource is now in appDir
  tofuInit(t, appDir)
  tofuApply(t, appDir, vars)
  tofuApply(t, sharedDir, vars)

  assertResourceInState(t, appDir, "random_id.condskip")
  assertResourceNotInState(t, sharedDir, "random_id.condskip")

  // Clean local migration files
  cleanupMigrationFiles(t, appDir)

  // Download: auto-inferred conditions should detect migration is done (import
  // block has resources_not_exist for "random_id.condskip", but it IS in state)
  dl := download.NewDownloader(cred, initArgs, tofuExecPath(t), false)
  downloaded, err := dl.Download(ctx, appDir)
  if err != nil {
    t.Fatalf("download: %v", err)
  }

  // The migration file should be skipped because the import condition (resources_not_exist)
  // fails — the resource already exists in the app layer's state
  if len(downloaded) != 0 {
    t.Errorf("expected no files downloaded (migration already applied), got %d: %v", len(downloaded), downloaded)
  }

  // Verify no migration files on disk
  matches, _ := filepath.Glob(filepath.Join(appDir, "migration.*.tf"))
  if len(matches) != 0 {
    t.Errorf("expected no migration files on disk, got: %v", matches)
  }
}

// TestE2EFast_Prune tests that completed migrations are pruned from blob storage
// after their conditions no longer hold.
func TestE2EFast_Prune(t *testing.T) {
  t.Parallel()
  storageAccount := requireEnv(t, "E2E_STORAGE_ACCOUNT_NAME")
  ctx := context.Background()
  cred := getCredential(t)

  rootDir, prefix, vars := setupFastProject(t)
  containerName := prefix
  createContainer(t, ctx, cred, storageAccount, containerName)
  t.Cleanup(func() { deleteContainer(t, ctx, cred, storageAccount, containerName) })

  sharedDir := filepath.Join(rootDir, "layers", "shared")
  appDir := filepath.Join(rootDir, "layers", "app")
  initArgs := storageInitArgs(storageAccount, containerName)

  // Create resource
  updateTfFile(t, sharedDir, "main.tf", `
terraform {
  required_providers {
    random = {
      source = "hashicorp/random"
    }
  }
}

provider "random" {}

resource "random_id" "prune_test" {
  byte_length = 4
  keepers = {
    prefix = var.prefix
    name   = "prune_test"
  }
}
`)

  tofuInit(t, sharedDir)
  tofuApply(t, sharedDir, vars)
  tofuInit(t, appDir)
  t.Cleanup(func() {
    tofuDestroy(t, appDir, vars)
    tofuDestroy(t, sharedDir, vars)
  })

  // Generate and upload
  migDir := writeMigration(t, rootDir, "001_prune.yaml", fmt.Sprintf(`
description: "Move for prune test"
operations:
  - type: move
    source_layer: "%s"
    destination_layer: "%s"
    resources:
      - from: "random_id.prune_test"
`, sharedDir, appDir))

  updateTfFile(t, appDir, "main.tf", `
terraform {
  required_providers {
    random = {
      source = "hashicorp/random"
    }
  }
}

provider "random" {}

resource "random_id" "prune_test" {
  byte_length = 4
  keepers = {
    prefix = var.prefix
    name   = "prune_test"
  }
}
`)

  updateTfFile(t, sharedDir, "main.tf", `
terraform {
  required_providers {
    random = {
      source = "hashicorp/random"
    }
  }
}

provider "random" {}
`)

  files := runGenerate(t, []string{migDir})
  if len(files) == 0 {
    t.Fatal("expected generated migration files")
  }

  // Upload from app layer
  mgr := upload.NewManager(cred, initArgs)
  if err := mgr.UploadFromDisk(ctx, []string{appDir}); err != nil {
    t.Fatalf("upload: %v", err)
  }

  // Verify blobs exist before prune
  uploader, err := upload.DefaultUploaderFactory(storageAccount, containerName, cred)
  if err != nil {
    t.Fatalf("creating uploader: %v", err)
  }
  blobsBefore, err := uploader.ListBlobs(ctx, "migrations/")
  if err != nil {
    t.Fatalf("listing blobs: %v", err)
  }
  if len(blobsBefore) == 0 {
    t.Fatal("expected blobs in storage before prune")
  }

  // Apply the migration
  tofuInit(t, appDir)
  tofuApply(t, appDir, vars)
  tofuApply(t, sharedDir, vars)

  assertResourceInState(t, appDir, "random_id.prune_test")
  assertResourceNotInState(t, sharedDir, "random_id.prune_test")

  // Prune: replicate the prune logic since cmd.pruneLayer is unexported.
  // For each blob, download → parse metadata → evaluate conditions → prune if done.
  stateReader, err := state.NewTofuStateReaderFromPath(nil)
  if err != nil {
    t.Fatalf("creating state reader: %v", err)
  }
  readState := conditions.NewStateReaderFunc(stateReader)

  var pruned int
  for _, blobName := range blobsBefore {
    if !strings.HasSuffix(blobName, ".tf") {
      continue
    }
    content, err := uploader.DownloadBlob(ctx, blobName)
    if err != nil {
      t.Fatalf("downloading blob %s: %v", blobName, err)
    }
    meta, err := generator.ParseMetadataComment(string(content))
    if err != nil {
      t.Fatalf("parsing metadata for %s: %v", blobName, err)
    }
    if meta == nil || meta.Conditions == nil {
      continue
    }
    active, err := conditions.EvaluateMetadataConditions(ctx, meta, readState, appDir)
    if err != nil {
      t.Fatalf("evaluating conditions for %s: %v", blobName, err)
    }
    if !active {
      // Conditions failed = migration complete = safe to prune
      if err := uploader.DeleteBlob(ctx, blobName); err != nil {
        t.Fatalf("deleting blob %s: %v", blobName, err)
      }
      pruned++
    }
  }

  if pruned == 0 {
    t.Error("expected at least one blob to be pruned after migration was applied")
  }

  // Verify blobs are gone
  blobsAfter, err := uploader.ListBlobs(ctx, "migrations/")
  if err != nil {
    t.Fatalf("listing blobs after prune: %v", err)
  }
  if len(blobsAfter) != 0 {
    t.Errorf("expected no blobs after prune, got %d: %v", len(blobsAfter), blobsAfter)
  }

  cleanupMigrationFiles(t, appDir)
  cleanupMigrationFiles(t, sharedDir)
}

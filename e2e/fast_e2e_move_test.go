//go:build e2e_fast

package e2e

import (
	"fmt"
	"path/filepath"
	"testing"
)

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

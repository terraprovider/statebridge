//go:build e2e_fast

package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

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

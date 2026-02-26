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

	updateTfFile(t, sharedDir, "main.tf", randomProviderHCL+randomIDResource("importable"))
	tofuInit(t, sharedDir)
	tofuApply(t, sharedDir, vars)
	t.Cleanup(func() { tofuDestroy(t, sharedDir, vars) })

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

	updateTfFile(t, sharedDir, "main.tf", randomProviderHCL+`
resource "random_id" "renamed" {
  byte_length = 4
  keepers = {
    prefix = var.prefix
    name   = "importable"
  }
}
`)

	requireGenerate(t, migDir)
	tofuApply(t, sharedDir, vars)

	assertResourceNotInState(t, sharedDir, "random_id.importable")
	assertResourceInState(t, sharedDir, "random_id.renamed")

	cleanupAndAssertClean(t, vars, sharedDir)
}

// TestE2EFast_RemoveAndImport tests removing a resource from state and importing it back.
func TestE2EFast_RemoveAndImport(t *testing.T) {
	t.Parallel()
	rootDir, _, vars := setupFastProject(t)

	sharedDir := filepath.Join(rootDir, "layers", "shared")

	updateTfFile(t, sharedDir, "main.tf", randomProviderHCL+randomIDResource("importable"))
	tofuInit(t, sharedDir)
	tofuApply(t, sharedDir, vars)
	t.Cleanup(func() { tofuDestroy(t, sharedDir, vars) })

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

	updateTfFile(t, sharedDir, "main.tf", randomProviderHCL)

	requireGenerate(t, migDir)
	tofuApply(t, sharedDir, vars)

	assertResourceNotInState(t, sharedDir, "random_id.importable")

	cleanupAndAssertClean(t, vars, sharedDir)

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

	updateTfFile(t, sharedDir, "main.tf", randomProviderHCL+randomIDResource("importable"))

	requireGenerate(t, migDir)
	tofuApply(t, sharedDir, vars)

	assertResourceInState(t, sharedDir, "random_id.importable")

	cleanupAndAssertClean(t, vars, sharedDir)
}

//go:build e2e_fast

package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestE2EFast_ImportFromSource tests importing a resource whose import ID is
// derived from another resource's state attributes (source-based import).
func TestE2EFast_ImportFromSource(t *testing.T) {
	t.Parallel()
	rootDir, _, vars := setupFastProject(t)

	sharedDir := filepath.Join(rootDir, "layers", "shared")
	appDir := filepath.Join(rootDir, "layers", "app")

	// 1. Create source resource in shared layer.
	updateTfFile(t, sharedDir, "main.tf", randomProviderHCL+randomIDResource("source"))
	tofuInit(t, sharedDir)
	tofuApply(t, sharedDir, vars)
	t.Cleanup(func() { tofuDestroy(t, sharedDir, vars) })

	assertResourceInState(t, sharedDir, "random_id.source")

	// Read the id attribute (base64url) to verify the generated import ID later.
	sourceID := resourceAttribute(t, sharedDir, "random_id.source", "id")

	// 2. Write migration: import from source with template-based ID.
	// random_id must be imported using the 'id' attr (base64url), not 'b64_std'.
	migDir := writeMigration(t, rootDir, "001_import_from_source.yaml", fmt.Sprintf(`
description: "Import derived resource from source"
operations:
  - type: import
    layer: "%s"
    imports:
      - address: "random_id.derived"
        id: '{{ .Attributes.id }}'
        source:
          layer: "%s"
          address: "random_id.source"
`, appDir, sharedDir))

	// 3. Set up app layer with matching resource definition.
	updateTfFile(t, appDir, "main.tf", randomProviderHCL+`
resource "random_id" "derived" {
  byte_length = 4
  keepers = {
    prefix = var.prefix
    name   = "source"
  }
}
`)
	tofuInit(t, appDir)

	// 4. Generate and verify.
	files := requireGenerate(t, migDir)
	for _, f := range files {
		assertFileContains(t, f, sourceID)
		assertFileContains(t, f, `random_id.derived`)
	}

	// 5. Apply and verify resource appears in app state.
	tofuApply(t, appDir, vars)
	assertResourceInState(t, appDir, "random_id.derived")

	// 6. Cleanup.
	cleanupMigrationFiles(t, appDir)
	assertCleanPlan(t, appDir, vars)
	t.Cleanup(func() { tofuDestroy(t, appDir, vars) })
}

// TestE2EFast_ImportFromSourceForEach tests source-based import from a for_each
// resource with key remapping.
func TestE2EFast_ImportFromSourceForEach(t *testing.T) {
	t.Parallel()
	rootDir, _, vars := setupFastProject(t)

	sharedDir := filepath.Join(rootDir, "layers", "shared")
	appDir := filepath.Join(rootDir, "layers", "app")

	// 1. Create for_each source in shared layer.
	updateTfFile(t, sharedDir, "main.tf", randomProviderHCL+randomIDForEachResource("source", []string{"alpha", "beta"}))
	tofuInit(t, sharedDir)
	tofuApply(t, sharedDir, vars)
	t.Cleanup(func() { tofuDestroy(t, sharedDir, vars) })

	assertResourceInState(t, sharedDir, `random_id.source["alpha"]`)
	assertResourceInState(t, sharedDir, `random_id.source["beta"]`)

	// Read import IDs (base64url 'id' attr) to verify later.
	alphaID := resourceAttribute(t, sharedDir, `random_id.source["alpha"]`, "id")
	betaID := resourceAttribute(t, sharedDir, `random_id.source["beta"]`, "id")

	// 2. Write migration: import from for_each source with key remapping.
	// random_id must be imported using the 'id' attr (base64url), not 'b64_std'.
	migDir := writeMigration(t, rootDir, "001_import_foreach.yaml", fmt.Sprintf(`
description: "Import from for_each source"
operations:
  - type: import
    layer: "%s"
    imports:
      - address: "random_id.derived"
        id: '{{ .Attributes.id }}'
        key: 'app_{{ .Key }}'
        source:
          layer: "%s"
          address: "random_id.source"
`, appDir, sharedDir))

	// 3. Set up app layer with matching for_each definition using remapped keys.
	updateTfFile(t, appDir, "main.tf", randomProviderHCL+randomIDForEachResource("derived", []string{"app_alpha", "app_beta"}))
	tofuInit(t, appDir)

	// 4. Generate and verify both IDs appear.
	files := requireGenerate(t, migDir)
	for _, f := range files {
		assertFileContains(t, f, alphaID)
		assertFileContains(t, f, betaID)
		assertFileContains(t, f, `random_id.derived["app_alpha"]`)
		assertFileContains(t, f, `random_id.derived["app_beta"]`)
	}

	// 5. Apply and verify.
	tofuApply(t, appDir, vars)
	assertResourceInState(t, appDir, `random_id.derived["app_alpha"]`)
	assertResourceInState(t, appDir, `random_id.derived["app_beta"]`)

	// 6. Cleanup.
	cleanupMigrationFiles(t, appDir)
	assertCleanPlan(t, appDir, vars)
	t.Cleanup(func() { tofuDestroy(t, appDir, vars) })
}

// TestE2EFast_ImportFromSourceWithExpand tests source-based import with attribute
// expansion. A random_shuffle resource's "input" list attribute is expanded to
// produce multiple import blocks.
func TestE2EFast_ImportFromSourceWithExpand(t *testing.T) {
	t.Parallel()
	rootDir, _, vars := setupFastProject(t)

	sharedDir := filepath.Join(rootDir, "layers", "shared")
	appDir := filepath.Join(rootDir, "layers", "app")

	// 1. Create a random_shuffle resource in shared layer with known input list.
	updateTfFile(t, sharedDir, "main.tf", randomProviderHCL+`
resource "random_shuffle" "expander" {
  input        = ["alpha", "beta", "gamma"]
  result_count = 3
  keepers = {
    prefix = var.prefix
  }
}
`)
	tofuInit(t, sharedDir)
	tofuApply(t, sharedDir, vars)
	t.Cleanup(func() { tofuDestroy(t, sharedDir, vars) })

	assertResourceInState(t, sharedDir, "random_shuffle.expander")

	// 2. Write migration with expand on "input" attribute.
	// Each element of the input list becomes a separate import block.
	// We use random_pet here as the target since it can import with any string.
	migDir := writeMigration(t, rootDir, "001_expand_import.yaml", fmt.Sprintf(`
description: "Expand import from source list attribute"
operations:
  - type: import
    layer: "%s"
    imports:
      - address: "random_pet.expanded"
        id: '{{ .Item }}'
        key: '{{ .Item }}'
        source:
          layer: "%s"
          address: "random_shuffle.expander"
          expand: "input"
`, appDir, sharedDir))

	// 3. Set up app layer with matching for_each definition.
	updateTfFile(t, appDir, "main.tf", randomProviderHCL+`
resource "random_pet" "expanded" {
  for_each = toset(["alpha", "beta", "gamma"])
  keepers = {
    prefix = var.prefix
    item   = each.key
  }
}
`)
	tofuInit(t, appDir)

	// 4. Generate and verify that all three expanded items appear.
	files := requireGenerate(t, migDir)
	for _, f := range files {
		assertFileContains(t, f, `random_pet.expanded["alpha"]`)
		assertFileContains(t, f, `random_pet.expanded["beta"]`)
		assertFileContains(t, f, `random_pet.expanded["gamma"]`)
	}

	// 5. Cleanup generated migration files only (import IDs won't match real
	// random_pet resources, so we don't apply).
	cleanupMigrationFiles(t, appDir)

	// Remove the migrations directory so it doesn't interfere.
	os.RemoveAll(filepath.Join(rootDir, "migrations"))
}

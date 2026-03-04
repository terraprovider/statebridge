//go:build e2e_fast

package e2e

import (
	"fmt"
	"path/filepath"
	"testing"
)

// TestE2EFast_SameLayerMoveRename tests that a move operation within the same
// layer generates a moved block (not removed+import) and correctly renames
// the resource in state.
func TestE2EFast_SameLayerMoveRename(t *testing.T) {
	t.Parallel()
	rootDir, _, vars := setupFastProject(t)

	sharedDir := filepath.Join(rootDir, "layers", "shared")

	updateTfFile(t, sharedDir, "main.tf", randomProviderHCL+randomIDResource("old_name"))
	tofuInit(t, sharedDir)
	tofuApply(t, sharedDir, vars)
	t.Cleanup(func() { tofuDestroy(t, sharedDir, vars) })

	assertResourceInState(t, sharedDir, "random_id.old_name")

	// Write migration: same-layer move with rename
	migDir := writeMigration(t, rootDir, "001_same_layer_rename.yaml", fmt.Sprintf(`
description: "Rename resource within same layer using move"
operations:
  - type: move
    source_layer: "%s"
    destination_layer: "%s"
    resources:
      - from: "random_id.old_name"
        to: "random_id.new_name"
`, sharedDir, sharedDir))

	// Update TF to reflect the new name
	updateTfFile(t, sharedDir, "main.tf", randomProviderHCL+`
resource "random_id" "new_name" {
  byte_length = 4
  keepers = {
    prefix = var.prefix
    name   = "old_name"
  }
}
`)

	// Generate and verify it produces a moved block
	files := requireGenerate(t, migDir)

	// Only one layer, so one output file
	if len(files) != 1 {
		t.Fatalf("expected 1 generated file, got %d", len(files))
	}
	assertFileContains(t, files[0], "moved {")
	assertFileContains(t, files[0], "random_id.old_name")
	assertFileContains(t, files[0], "random_id.new_name")

	// Apply
	tofuApply(t, sharedDir, vars)

	// Verify state
	assertResourceInState(t, sharedDir, "random_id.new_name")
	assertResourceNotInState(t, sharedDir, "random_id.old_name")

	cleanupAndAssertClean(t, vars, sharedDir)
}

// TestE2EFast_SameLayerKeyedMove tests that a same-layer move with key
// remapping generates moved blocks for each remapped key.
func TestE2EFast_SameLayerKeyedMove(t *testing.T) {
	t.Parallel()
	rootDir, _, vars := setupFastProject(t)

	sharedDir := filepath.Join(rootDir, "layers", "shared")

	updateTfFile(t, sharedDir, "main.tf", randomProviderHCL+
		randomIDForEachResource("items", []string{"alpha", "beta"}))
	tofuInit(t, sharedDir)
	tofuApply(t, sharedDir, vars)
	t.Cleanup(func() { tofuDestroy(t, sharedDir, vars) })

	assertResourceInState(t, sharedDir, `random_id.items["alpha"]`)
	assertResourceInState(t, sharedDir, `random_id.items["beta"]`)

	// Write migration: same-layer keyed move
	migDir := writeMigration(t, rootDir, "001_same_layer_keyed.yaml", fmt.Sprintf(`
description: "Re-key for_each within same layer"
operations:
  - type: move
    source_layer: "%s"
    destination_layer: "%s"
    resources:
      - from: "random_id.items"
        keys:
          alpha: new_alpha
          beta: new_beta
`, sharedDir, sharedDir))

	// Update TF to reflect new keys
	updateTfFile(t, sharedDir, "main.tf", randomProviderHCL+
		randomIDForEachResource("items", []string{"new_alpha", "new_beta"}))

	// Generate
	files := requireGenerate(t, migDir)

	if len(files) != 1 {
		t.Fatalf("expected 1 generated file, got %d", len(files))
	}
	assertFileContains(t, files[0], "moved {")
	assertFileContains(t, files[0], `random_id.items["alpha"]`)
	assertFileContains(t, files[0], `random_id.items["new_alpha"]`)

	// Apply
	tofuApply(t, sharedDir, vars)

	// Verify state
	assertResourceInState(t, sharedDir, `random_id.items["new_alpha"]`)
	assertResourceInState(t, sharedDir, `random_id.items["new_beta"]`)
	assertResourceNotInState(t, sharedDir, `random_id.items["alpha"]`)
	assertResourceNotInState(t, sharedDir, `random_id.items["beta"]`)

	cleanupAndAssertClean(t, vars, sharedDir)
}

// TestE2EFast_SameLayerModuleMove tests that a same-layer module move with
// a different destination name generates a single moved block for the module.
func TestE2EFast_SameLayerModuleMove(t *testing.T) {
	t.Parallel()
	rootDir, _, vars := setupFastProject(t)

	sharedDir := filepath.Join(rootDir, "layers", "shared")

	updateTfFile(t, sharedDir, "main.tf", randomProviderHCL+moduleBlock("old_mod", "module"))
	tofuInit(t, sharedDir)
	tofuApply(t, sharedDir, vars)
	t.Cleanup(func() { tofuDestroy(t, sharedDir, vars) })

	assertResourceInState(t, sharedDir, "module.old_mod.random_id.unit")

	// Write migration: rename module within same layer
	migDir := writeMigration(t, rootDir, "001_same_layer_module.yaml", fmt.Sprintf(`
description: "Rename module within same layer"
operations:
  - type: move
    source_layer: "%s"
    destination_layer: "%s"
    resources:
      - from: "module.old_mod"
        to: "module.new_mod"
`, sharedDir, sharedDir))

	// Update TF config to use new module name
	updateTfFile(t, sharedDir, "main.tf", randomProviderHCL+moduleBlock("new_mod", "module"))

	// Generate
	files := requireGenerate(t, migDir)

	if len(files) != 1 {
		t.Fatalf("expected 1 generated file, got %d", len(files))
	}
	assertFileContains(t, files[0], "moved {")
	assertFileContains(t, files[0], "module.old_mod")
	assertFileContains(t, files[0], "module.new_mod")

	// Apply
	tofuInit(t, sharedDir)
	tofuApply(t, sharedDir, vars)

	// Verify state
	assertResourceInState(t, sharedDir, "module.new_mod.random_id.unit")
	assertResourceNotInState(t, sharedDir, "module.old_mod.random_id.unit")

	cleanupAndAssertClean(t, vars, sharedDir)
}

// TestE2EFast_SourceDestinationPrefix tests cross-layer moves using the new
// source_prefix and destination_prefix fields.
func TestE2EFast_SourceDestinationPrefix(t *testing.T) {
	t.Parallel()
	rootDir, _, vars := setupFastProject(t)

	sharedDir := filepath.Join(rootDir, "layers", "shared")
	appDir := filepath.Join(rootDir, "layers", "app")

	// Create a module with a resource in the shared layer
	updateTfFile(t, sharedDir, "main.tf", randomProviderHCL+
		moduleBlock("src_mod", "prefix_test")+
		randomIDResource("anchor"))
	tofuInit(t, sharedDir)
	tofuApply(t, sharedDir, vars)
	t.Cleanup(func() {
		tofuDestroy(t, appDir, vars)
		tofuDestroy(t, sharedDir, vars)
	})

	assertResourceInState(t, sharedDir, "module.src_mod.random_id.unit")

	// Move the resource from module.src_mod in shared to module.dst_mod in app
	migDir := writeMigration(t, rootDir, "001_prefix_move.yaml", fmt.Sprintf(`
description: "Move with source and destination prefix"
operations:
  - type: move
    source_layer: "%s"
    destination_layer: "%s"
    source_prefix: "module.src_mod"
    destination_prefix: "module.dst_mod"
    resources:
      - from: "random_id.unit"
`, sharedDir, appDir))

	// Destination layer gets the module under the new name
	updateTfFile(t, appDir, "main.tf", randomProviderHCL+moduleBlock("dst_mod", "prefix_test"))

	// Remove the module from source (keep anchor to prevent empty layer)
	updateTfFile(t, sharedDir, "main.tf", randomProviderHCL+randomIDResource("anchor"))

	// Generate
	files := requireGenerate(t, migDir)

	if len(files) < 2 {
		t.Fatalf("expected at least 2 generated files (source + dest), got %d", len(files))
	}

	// Check source has removed block, dest has import block
	srcFile := ""
	dstFile := ""
	for _, f := range files {
		if filepath.Dir(f) == sharedDir {
			srcFile = f
		}
		if filepath.Dir(f) == appDir {
			dstFile = f
		}
	}
	if srcFile == "" {
		t.Fatal("no generated file in source layer")
	}
	if dstFile == "" {
		t.Fatal("no generated file in destination layer")
	}

	assertFileContains(t, srcFile, "removed {")
	assertFileContains(t, srcFile, "module.src_mod.random_id.unit")
	assertFileContains(t, dstFile, "import {")
	assertFileContains(t, dstFile, "module.dst_mod.random_id.unit")

	// Apply
	tofuInit(t, appDir)
	tofuApply(t, appDir, vars)
	tofuApply(t, sharedDir, vars)

	// Verify state
	assertResourceInState(t, appDir, "module.dst_mod.random_id.unit")
	assertResourceNotInState(t, sharedDir, "module.src_mod.random_id.unit")

	cleanupAndAssertClean(t, vars, sharedDir, appDir)
}

// TestE2EFast_SameLayerMoveWithPrefixes tests a same-layer move using
// source_prefix and destination_prefix — generating moved blocks.
func TestE2EFast_SameLayerMoveWithPrefixes(t *testing.T) {
	t.Parallel()
	rootDir, _, vars := setupFastProject(t)

	sharedDir := filepath.Join(rootDir, "layers", "shared")

	// Create two modules in the shared layer
	updateTfFile(t, sharedDir, "main.tf", randomProviderHCL+
		moduleBlock("old_mod", "prefix_same")+
		randomIDResource("anchor"))
	tofuInit(t, sharedDir)
	tofuApply(t, sharedDir, vars)
	t.Cleanup(func() { tofuDestroy(t, sharedDir, vars) })

	assertResourceInState(t, sharedDir, "module.old_mod.random_id.unit")

	// Write migration: same-layer move with different module prefixes
	migDir := writeMigration(t, rootDir, "001_same_layer_prefix.yaml", fmt.Sprintf(`
description: "Same-layer move with source/destination prefix"
operations:
  - type: move
    source_layer: "%s"
    destination_layer: "%s"
    source_prefix: "module.old_mod"
    destination_prefix: "module.new_mod"
    resources:
      - from: "random_id.unit"
`, sharedDir, sharedDir))

	// Update TF config: remove old module, add new module
	updateTfFile(t, sharedDir, "main.tf", randomProviderHCL+
		moduleBlock("new_mod", "prefix_same")+
		randomIDResource("anchor"))

	// Generate
	files := requireGenerate(t, migDir)

	if len(files) != 1 {
		t.Fatalf("expected 1 generated file, got %d", len(files))
	}
	assertFileContains(t, files[0], "moved {")
	assertFileContains(t, files[0], "module.old_mod.random_id.unit")
	assertFileContains(t, files[0], "module.new_mod.random_id.unit")

	// Apply
	tofuInit(t, sharedDir)
	tofuApply(t, sharedDir, vars)

	// Verify state
	assertResourceInState(t, sharedDir, "module.new_mod.random_id.unit")
	assertResourceNotInState(t, sharedDir, "module.old_mod.random_id.unit")

	cleanupAndAssertClean(t, vars, sharedDir)
}

//go:build e2e_fast

package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/terraprovider/statebridge/pkg/engine"
)

func TestE2EFast_ConditionSkip(t *testing.T) {
	t.Parallel()
	rootDir, _, vars := setupFastProject(t)

	sharedDir := filepath.Join(rootDir, "layers", "shared")

	updateTfFile(t, sharedDir, "main.tf", randomProviderHCL+randomIDResource("conditioned"))
	tofuInit(t, sharedDir)
	tofuApply(t, sharedDir, vars)
	t.Cleanup(func() { tofuDestroy(t, sharedDir, vars) })

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

	updateTfFile(t, sharedDir, "main.tf", randomProviderHCL+`
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

	cleanupAndAssertClean(t, vars, sharedDir)
}

// TestE2EFast_ConditionLayerExists tests layer_exists and layer_not_exists conditions.
func TestE2EFast_ConditionLayerExists(t *testing.T) {
	t.Parallel()
	rootDir, _, vars := setupFastProject(t)

	sharedDir := filepath.Join(rootDir, "layers", "shared")
	nonexistentDir := filepath.Join(rootDir, "layers", "deleted")

	updateTfFile(t, sharedDir, "main.tf", randomProviderHCL+randomIDResource("layer_check"))
	tofuInit(t, sharedDir)
	tofuApply(t, sharedDir, vars)
	t.Cleanup(func() { tofuDestroy(t, sharedDir, vars) })

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

	updateTfFile(t, sharedDir, "main.tf", randomProviderHCL+`
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

	cleanupAndAssertClean(t, vars, sharedDir)
}

// TestE2EFast_RetiredStatus tests that migrations with status: retired are skipped entirely.
func TestE2EFast_RetiredStatus(t *testing.T) {
	t.Parallel()
	rootDir, _, vars := setupFastProject(t)

	sharedDir := filepath.Join(rootDir, "layers", "shared")

	updateTfFile(t, sharedDir, "main.tf", randomProviderHCL+randomIDResource("keep"))
	tofuInit(t, sharedDir)
	tofuApply(t, sharedDir, vars)
	t.Cleanup(func() { tofuDestroy(t, sharedDir, vars) })

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
// is handled gracefully via explicit conditions.
func TestE2EFast_Idempotency(t *testing.T) {
	t.Parallel()
	rootDir, _, vars := setupFastProject(t)

	sharedDir := filepath.Join(rootDir, "layers", "shared")
	appDir := filepath.Join(rootDir, "layers", "app")

	updateTfFile(t, sharedDir, "main.tf", randomProviderHCL+randomIDResource("idempotent"))
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

	updateTfFile(t, appDir, "main.tf", randomProviderHCL+randomIDResource("idempotent"))
	updateTfFile(t, sharedDir, "main.tf", randomProviderHCL)

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

	updateTfFile(t, sharedDir, "main.tf", randomProviderHCL+
		randomIDResource("first")+randomIDResource("second"))
	tofuInit(t, sharedDir)
	tofuApply(t, sharedDir, vars)
	t.Cleanup(func() { tofuDestroy(t, sharedDir, vars) })

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

	updateTfFile(t, sharedDir, "main.tf", randomProviderHCL+`
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

	cleanupAndAssertClean(t, vars, sharedDir)
}

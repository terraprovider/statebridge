//go:build e2e_fast

package e2e

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/terraprovider/statebridge/pkg/engine"
	"github.com/terraprovider/statebridge/pkg/state"
)

// TestE2EFast_MergeDuplicates tests the merge_duplicates feature: two source
// for_each resources produce the same destination address via keyed moves.
// With merge_duplicates: true, the engine should generate only one import block
// for the shared destination key.
func TestE2EFast_MergeDuplicates(t *testing.T) {
	t.Parallel()
	rootDir, _, vars := setupFastProject(t)

	sharedDir := filepath.Join(rootDir, "layers", "shared")
	appDir := filepath.Join(rootDir, "layers", "app")

	// Source: two for_each resources with different key sets but a keepers.prefix
	// attribute that can produce matching import_ids via templates.
	updateTfFile(t, sharedDir, "main.tf", randomProviderHCL+`
resource "random_id" "policy_active" {
  for_each    = toset(["key_a", "key_b"])
  byte_length = 4
  keepers = {
    prefix = var.prefix
    key    = each.key
    name   = "policy_active"
  }
}

resource "random_id" "policy_eligible" {
  for_each    = toset(["key_a", "key_c"])
  byte_length = 4
  keepers = {
    prefix = var.prefix
    key    = each.key
    name   = "policy_eligible"
  }
}
`)
	tofuInit(t, sharedDir)
	tofuApply(t, sharedDir, vars)
	t.Cleanup(func() {
		tofuDestroy(t, appDir, vars)
		tofuDestroy(t, sharedDir, vars)
	})

	// Since policy_active["key_a"] and policy_eligible["key_a"] have different
	// random IDs, the merge_duplicates with mismatching IDs would fail.
	//
	// To test the happy path (same import ID), we use a fixed import_id for
	// the shared destination key. Both resources route key_a → "shared",
	// both using '{{ attr .Attributes "keepers" "prefix" }}' as import ID (same value).
	migDir := writeMigration(t, rootDir, "001_merge.yaml", fmt.Sprintf(`
description: "Merge duplicates test"
operations:
  - type: move
    source_layer: "%s"
    destination_layer: "%s"
    resources:
      - from: "random_id.policy_active"
        to: "random_id.policy"
        merge_duplicates: true
        import_id: '{{ attr .Attributes "keepers" "prefix" }}'
        keys:
          key_a: shared
          key_b: only_active
      - from: "random_id.policy_eligible"
        to: "random_id.policy"
        merge_duplicates: true
        import_id: '{{ attr .Attributes "keepers" "prefix" }}'
        keys:
          key_a: shared
          key_c: only_eligible
`, sharedDir, appDir))

	// Generate migration files
	files := requireGenerate(t, migDir)

	// Verify generated files: should have 2 files (shared + app)
	if len(files) < 2 {
		t.Fatalf("expected at least 2 generated files, got %d: %v", len(files), files)
	}

	// Find the app layer file and check import block count
	var appFileContent string
	for _, f := range files {
		if strings.HasPrefix(f, appDir) {
			data, err := os.ReadFile(f)
			if err != nil {
				t.Fatalf("reading %s: %v", f, err)
			}
			appFileContent = string(data)
			break
		}
	}
	if appFileContent == "" {
		t.Fatal("no generated file found for app layer")
	}

	// Should have 3 import blocks (not 4): "shared" is deduplicated
	importCount := strings.Count(appFileContent, "import {")
	if importCount != 3 {
		t.Errorf("expected 3 import blocks (shared deduplicated), got %d in:\n%s", importCount, appFileContent)
	}

	// Verify the shared key appears exactly once
	sharedCount := strings.Count(appFileContent, `random_id.policy["shared"]`)
	if sharedCount != 1 {
		t.Errorf("expected 1 occurrence of shared destination key, got %d", sharedCount)
	}

	// Verify source has 2 removed blocks
	var sharedFileContent string
	for _, f := range files {
		if strings.HasPrefix(f, sharedDir) {
			data, err := os.ReadFile(f)
			if err != nil {
				t.Fatalf("reading %s: %v", f, err)
			}
			sharedFileContent = string(data)
			break
		}
	}
	if sharedFileContent == "" {
		t.Fatal("no generated file found for shared layer")
	}
	removedCount := strings.Count(sharedFileContent, "removed {")
	if removedCount != 2 {
		t.Errorf("expected 2 removed blocks, got %d in:\n%s", removedCount, sharedFileContent)
	}
}

// TestE2EFast_MergeDuplicatesConflict tests that merge_duplicates errors
// when the import IDs for the same destination address differ.
func TestE2EFast_MergeDuplicatesConflict(t *testing.T) {
	t.Parallel()
	rootDir, _, vars := setupFastProject(t)

	sharedDir := filepath.Join(rootDir, "layers", "shared")
	appDir := filepath.Join(rootDir, "layers", "app")

	updateTfFile(t, sharedDir, "main.tf", randomProviderHCL+`
resource "random_id" "policy_active" {
  for_each    = toset(["key_a"])
  byte_length = 4
  keepers = {
    prefix = var.prefix
    key    = each.key
    name   = "active"
  }
}

resource "random_id" "policy_eligible" {
  for_each    = toset(["key_a"])
  byte_length = 4
  keepers = {
    prefix = var.prefix
    key    = each.key
    name   = "eligible"
  }
}
`)
	tofuInit(t, sharedDir)
	tofuApply(t, sharedDir, vars)
	t.Cleanup(func() {
		tofuDestroy(t, appDir, vars)
		tofuDestroy(t, sharedDir, vars)
	})

	// Both resources map to the same destination key, but with default import_id
	// (which uses the resource's own "id" attribute — different for each resource).
	// This should produce a merge_duplicates conflict error.
	migDir := writeMigration(t, rootDir, "001_conflict.yaml", fmt.Sprintf(`
description: "Merge duplicates conflict test"
operations:
  - type: move
    source_layer: "%s"
    destination_layer: "%s"
    resources:
      - from: "random_id.policy_active"
        to: "random_id.policy"
        merge_duplicates: true
        keys:
          key_a: shared
      - from: "random_id.policy_eligible"
        to: "random_id.policy"
        merge_duplicates: true
        keys:
          key_a: shared
`, sharedDir, appDir))

	// Generate should fail because the import IDs differ.
	// When the only migration file is error-skipped, ProcessFiles returns an error
	// ("all migration files were skipped"), so we call the engine directly.
	reader, err := state.NewTofuStateReaderFromPath(nil)
	if err != nil {
		t.Fatalf("creating state reader: %v", err)
	}
	eng := engine.New(engine.Config{StateReader: reader, DryRun: false})
	_, err = eng.ProcessFiles(context.Background(), []string{migDir})
	if err == nil {
		t.Fatal("expected error from ProcessFiles due to merge_duplicates conflict")
	}
	// The error says "all migration files were skipped: <path>"; the detailed
	// conflict message (with differing import IDs) is logged to stderr.
	// Just confirm the file was indeed skipped as an error.
	if !strings.Contains(err.Error(), "skipped") {
		t.Errorf("expected error to mention files being skipped, got: %v", err)
	}
}

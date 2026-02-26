package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tfjson "github.com/hashicorp/terraform-json"

	"github.com/redtenant/tfmigrate/internal/testutil"
)

func TestEngine_ProcessFiles_ValidationError(t *testing.T) {
	dir := t.TempDir()
	migrationContent := `
description: "Invalid migration"
operations:
  - type: move
`
	migrationFile := filepath.Join(dir, "001_bad.yaml")
	if err := os.WriteFile(migrationFile, []byte(migrationContent), 0o644); err != nil {
		t.Fatal(err)
	}

	engine := New(Config{StateReader: testutil.NewMockStateReader(nil)})

	_, err := engine.ProcessFiles(context.Background(), []string{migrationFile})
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "validation") {
		t.Errorf("expected validation error message, got: %v", err)
	}
}


func TestEngine_ProcessFiles_DryRun(t *testing.T) {
	dir := t.TempDir()
	layerDir := filepath.Join(dir, "layers", "net")
	if err := os.MkdirAll(layerDir, 0o755); err != nil {
		t.Fatal(err)
	}

	migrationContent := `
description: "Rename VPC"
operations:
  - type: rename
    layer: "` + layerDir + `"
    renames:
      - from: "module.old"
        to: "module.new"
`
	migrationFile := filepath.Join(dir, "001_rename.yaml")
	if err := os.WriteFile(migrationFile, []byte(migrationContent), 0o644); err != nil {
		t.Fatal(err)
	}

	engine := New(Config{
		StateReader: testutil.NewMockStateReader(nil),
		DryRun:      true,
	})

	result, err := engine.ProcessFiles(context.Background(), []string{migrationFile})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.OutputFiles) != 1 {
		t.Fatalf("expected 1 file path, got %d", len(result.OutputFiles))
	}

	// Verify file was NOT written in dry run mode
	if _, statErr := os.Stat(result.OutputFiles[0]); !os.IsNotExist(statErr) {
		t.Error("expected file to not exist in dry-run mode")
	}
}


func TestEngine_ProcessFiles_StatusRetired(t *testing.T) {
	dir := t.TempDir()
	layerDir := filepath.Join(dir, "layers", "net")
	if err := os.MkdirAll(layerDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// First file: status retired → should be skipped entirely
	retiredContent := `
description: "Old migration"
status: retired
operations:
  - type: rename
    layer: "` + layerDir + `"
    renames:
      - from: "module.old"
        to: "module.new"
`
	retiredFile := filepath.Join(dir, "001_retired.yaml")
	if err := os.WriteFile(retiredFile, []byte(retiredContent), 0o644); err != nil {
		t.Fatal(err)
	}

	engine := New(Config{StateReader: testutil.NewMockStateReader(nil)})

	// Retired-only: should return empty results with no error
	result, err := engine.ProcessFiles(context.Background(), []string{retiredFile})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.OutputFiles) != 0 {
		t.Errorf("expected 0 files for retired migration, got %d: %v", len(result.OutputFiles), result.OutputFiles)
	}
	if len(result.SkippedFiles) != 1 {
		t.Fatalf("expected 1 skipped file, got %d", len(result.SkippedFiles))
	}
	if result.SkippedFiles[0].Reason != SkipRetired {
		t.Errorf("expected SkipRetired reason, got %v", result.SkippedFiles[0].Reason)
	}

	// Second file: active rename → should produce output
	activeContent := `
description: "Active rename"
operations:
  - type: rename
    layer: "` + layerDir + `"
    renames:
      - from: "module.alpha"
        to: "module.beta"
`
	activeFile := filepath.Join(dir, "002_active.yaml")
	if err := os.WriteFile(activeFile, []byte(activeContent), 0o644); err != nil {
		t.Fatal(err)
	}

	engine2 := New(Config{StateReader: testutil.NewMockStateReader(nil)})

	result, err = engine2.ProcessFiles(context.Background(), []string{retiredFile, activeFile})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.OutputFiles) != 1 {
		t.Fatalf("expected 1 file (only active), got %d: %v", len(result.OutputFiles), result.OutputFiles)
	}

	content, err := os.ReadFile(result.OutputFiles[0])
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	if !strings.Contains(string(content), "module.alpha") {
		t.Error("expected module.alpha from active file")
	}
	if !strings.Contains(string(content), "module.beta") {
		t.Error("expected module.beta from active file")
	}
}


func TestEngine_ProcessFiles_LayerAutoSkip(t *testing.T) {
	// Migration file references a non-existent source_layer.
	// In non-strict mode, it should be auto-skipped gracefully.
	dir := t.TempDir()
	dstLayer := filepath.Join(dir, "layers", "app")
	if err := os.MkdirAll(dstLayer, 0o755); err != nil {
		t.Fatal(err)
	}

	nonExistentLayer := filepath.Join(dir, "layers", "nonexistent")

	migrationContent := `
description: "Move from nonexistent layer"
operations:
  - type: move
    source_layer: "` + nonExistentLayer + `"
    destination_layer: "` + dstLayer + `"
    resources:
      - from: "aws_instance.web"
        import_id: "i-0abc123"
`
	migrationFile := filepath.Join(dir, "001_missing_layer.yaml")
	if err := os.WriteFile(migrationFile, []byte(migrationContent), 0o644); err != nil {
		t.Fatal(err)
	}

	engine := New(Config{StateReader: testutil.NewMockStateReader(nil)})

	// Non-strict mode: auto-skipped, no error (does NOT count toward "all skipped")
	result, err := engine.ProcessFiles(context.Background(), []string{migrationFile})
	if err != nil {
		t.Fatalf("expected no error in non-strict mode (auto-skip), got: %v", err)
	}
	if len(result.OutputFiles) != 0 {
		t.Errorf("expected 0 files for auto-skipped migration, got %d: %v", len(result.OutputFiles), result.OutputFiles)
	}
	if len(result.SkippedFiles) != 1 {
		t.Fatalf("expected 1 skipped file, got %d", len(result.SkippedFiles))
	}
	if result.SkippedFiles[0].Reason != SkipLayerMissing {
		t.Errorf("expected SkipLayerMissing reason, got %v", result.SkippedFiles[0].Reason)
	}
}


func TestEngine_ProcessFiles_AutoSkipRename(t *testing.T) {
	// Rename operation with non-existent layer should be auto-skipped in non-strict mode.
	dir := t.TempDir()
	nonExistentLayer := filepath.Join(dir, "layers", "gone")

	migrationContent := `
description: "Rename in missing layer"
operations:
  - type: rename
    layer: "` + nonExistentLayer + `"
    renames:
      - from: "module.old"
        to: "module.new"
`
	migrationFile := filepath.Join(dir, "001_rename_gone.yaml")
	if err := os.WriteFile(migrationFile, []byte(migrationContent), 0o644); err != nil {
		t.Fatal(err)
	}

	engine := New(Config{StateReader: testutil.NewMockStateReader(nil)})

	result, err := engine.ProcessFiles(context.Background(), []string{migrationFile})
	if err != nil {
		t.Fatalf("expected no error (auto-skip), got: %v", err)
	}
	if len(result.OutputFiles) != 0 {
		t.Errorf("expected 0 output files, got %d", len(result.OutputFiles))
	}
	if len(result.SkippedFiles) != 1 {
		t.Fatalf("expected 1 skipped file, got %d", len(result.SkippedFiles))
	}
	if result.SkippedFiles[0].Reason != SkipLayerMissing {
		t.Errorf("expected SkipLayerMissing, got %v", result.SkippedFiles[0].Reason)
	}

	// Strict mode should error
	strictEngine := New(Config{StateReader: testutil.NewMockStateReader(nil), Strict: true})
	_, err = strictEngine.ProcessFiles(context.Background(), []string{migrationFile})
	if err == nil {
		t.Fatal("expected error in strict mode for missing layer")
	}
}


func TestEngine_ProcessFiles_AutoSkipRemove(t *testing.T) {
	// Remove operation with non-existent layer should be auto-skipped.
	dir := t.TempDir()
	nonExistentLayer := filepath.Join(dir, "layers", "deleted")

	migrationContent := `
description: "Remove from missing layer"
operations:
  - type: remove
    layer: "` + nonExistentLayer + `"
    entries:
      - address: "aws_iam_role.old"
`
	migrationFile := filepath.Join(dir, "001_remove_gone.yaml")
	if err := os.WriteFile(migrationFile, []byte(migrationContent), 0o644); err != nil {
		t.Fatal(err)
	}

	engine := New(Config{StateReader: testutil.NewMockStateReader(nil)})

	result, err := engine.ProcessFiles(context.Background(), []string{migrationFile})
	if err != nil {
		t.Fatalf("expected no error (auto-skip), got: %v", err)
	}
	if len(result.OutputFiles) != 0 {
		t.Errorf("expected 0 output files, got %d", len(result.OutputFiles))
	}
	if len(result.SkippedFiles) != 1 {
		t.Fatalf("expected 1 skipped file, got %d", len(result.SkippedFiles))
	}
	if result.SkippedFiles[0].Reason != SkipLayerMissing {
		t.Errorf("expected SkipLayerMissing, got %v", result.SkippedFiles[0].Reason)
	}
}


func TestEngine_ProcessFiles_AutoSkipImport(t *testing.T) {
	// Import operation with non-existent layer should be auto-skipped.
	dir := t.TempDir()
	nonExistentLayer := filepath.Join(dir, "layers", "missing")

	migrationContent := `
description: "Import to missing layer"
operations:
  - type: import
    layer: "` + nonExistentLayer + `"
    imports:
      - address: "aws_db_instance.primary"
        id: "db-123"
`
	migrationFile := filepath.Join(dir, "001_import_gone.yaml")
	if err := os.WriteFile(migrationFile, []byte(migrationContent), 0o644); err != nil {
		t.Fatal(err)
	}

	engine := New(Config{StateReader: testutil.NewMockStateReader(nil)})

	result, err := engine.ProcessFiles(context.Background(), []string{migrationFile})
	if err != nil {
		t.Fatalf("expected no error (auto-skip), got: %v", err)
	}
	if len(result.OutputFiles) != 0 {
		t.Errorf("expected 0 output files, got %d", len(result.OutputFiles))
	}
	if len(result.SkippedFiles) != 1 {
		t.Fatalf("expected 1 skipped file, got %d", len(result.SkippedFiles))
	}
	if result.SkippedFiles[0].Reason != SkipLayerMissing {
		t.Errorf("expected SkipLayerMissing, got %v", result.SkippedFiles[0].Reason)
	}
}


func TestEngine_ProcessFiles_AutoSkipMixedFile(t *testing.T) {
	// File with multiple operations where one references a missing layer.
	// Since collectLayerPaths checks all ops, the whole file should be auto-skipped.
	dir := t.TempDir()
	existingLayer := filepath.Join(dir, "layers", "net")
	if err := os.MkdirAll(existingLayer, 0o755); err != nil {
		t.Fatal(err)
	}
	nonExistentLayer := filepath.Join(dir, "layers", "gone")

	migrationContent := `
description: "Mixed operations with missing layer"
operations:
  - type: rename
    layer: "` + existingLayer + `"
    renames:
      - from: "module.old"
        to: "module.new"
  - type: move
    source_layer: "` + nonExistentLayer + `"
    destination_layer: "` + existingLayer + `"
    resources:
      - from: "aws_instance.web"
        import_id: "i-123"
`
	migrationFile := filepath.Join(dir, "001_mixed.yaml")
	if err := os.WriteFile(migrationFile, []byte(migrationContent), 0o644); err != nil {
		t.Fatal(err)
	}

	engine := New(Config{StateReader: testutil.NewMockStateReader(nil)})

	result, err := engine.ProcessFiles(context.Background(), []string{migrationFile})
	if err != nil {
		t.Fatalf("expected no error (auto-skip), got: %v", err)
	}
	if len(result.OutputFiles) != 0 {
		t.Errorf("expected 0 output files, got %d", len(result.OutputFiles))
	}
	if len(result.SkippedFiles) != 1 {
		t.Fatalf("expected 1 skipped file, got %d", len(result.SkippedFiles))
	}
	if result.SkippedFiles[0].Reason != SkipLayerMissing {
		t.Errorf("expected SkipLayerMissing, got %v", result.SkippedFiles[0].Reason)
	}
}


func TestEngine_ProcessFiles_DryRunContent(t *testing.T) {
	// Verify dry-run returns an output path containing the expected content description,
	// but does NOT write the file to disk.
	dir := t.TempDir()
	srcLayer := filepath.Join(dir, "layers", "compute")
	dstLayer := filepath.Join(dir, "layers", "app")
	if err := os.MkdirAll(srcLayer, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dstLayer, 0o755); err != nil {
		t.Fatal(err)
	}

	migrationContent := `
description: "Move in dry-run"
operations:
  - type: move
    source_layer: "` + srcLayer + `"
    destination_layer: "` + dstLayer + `"
    resources:
      - from: "aws_instance.web"
        import_id: "i-0abc123"
`
	migrationFile := filepath.Join(dir, "001_move.yaml")
	if err := os.WriteFile(migrationFile, []byte(migrationContent), 0o644); err != nil {
		t.Fatal(err)
	}

	mock := testutil.NewMockStateReader(map[string]*tfjson.State{
		srcLayer: testutil.BuildState(
			testutil.NewResource("aws_instance.web", "aws_instance", "web", nil,
				map[string]interface{}{"id": "i-0abc123"}),
		),
	})

	engine := New(Config{
		StateReader: mock,
		DryRun:      true,
	})

	result, err := engine.ProcessFiles(context.Background(), []string{migrationFile})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The move produces 2 files: removed in source, import in destination
	if len(result.OutputFiles) != 2 {
		t.Fatalf("expected 2 file paths, got %d: %v", len(result.OutputFiles), result.OutputFiles)
	}

	// Verify NO files were actually written
	for _, f := range result.OutputFiles {
		if _, err := os.Stat(f); !os.IsNotExist(err) {
			t.Errorf("expected file %q to NOT exist in dry-run mode", f)
		}
	}

	// Verify output paths are in the expected layer directories
	srcFile := findLayerFile(result.OutputFiles, srcLayer)
	dstFile := findLayerFile(result.OutputFiles, dstLayer)
	if srcFile == "" {
		t.Error("expected output file in source layer directory")
	}
	if dstFile == "" {
		t.Error("expected output file in destination layer directory")
	}

	// Verify no skipped files
	if len(result.SkippedFiles) != 0 {
		t.Errorf("expected 0 skipped files, got %d", len(result.SkippedFiles))
	}
}


func TestEngine_ProcessFiles_StrictWithValidLayers(t *testing.T) {
	// Strict mode should succeed when all referenced layers exist on disk.
	dir := t.TempDir()
	layerDir := filepath.Join(dir, "layers", "net")
	if err := os.MkdirAll(layerDir, 0o755); err != nil {
		t.Fatal(err)
	}

	migrationContent := `
description: "Rename in strict mode"
operations:
  - type: rename
    layer: "` + layerDir + `"
    renames:
      - from: "module.old_vpc"
        to: "module.new_vpc"
`
	migrationFile := filepath.Join(dir, "001_rename.yaml")
	if err := os.WriteFile(migrationFile, []byte(migrationContent), 0o644); err != nil {
		t.Fatal(err)
	}

	engine := New(Config{
		StateReader: testutil.NewMockStateReader(nil),
		Strict:      true,
	})

	result, err := engine.ProcessFiles(context.Background(), []string{migrationFile})
	if err != nil {
		t.Fatalf("unexpected error in strict mode with valid layers: %v", err)
	}
	if len(result.OutputFiles) != 1 {
		t.Fatalf("expected 1 output file, got %d", len(result.OutputFiles))
	}

	content, err := os.ReadFile(result.OutputFiles[0])
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	s := string(content)
	if !strings.Contains(s, "module.old_vpc") {
		t.Error("expected module.old_vpc in moved block")
	}
	if !strings.Contains(s, "module.new_vpc") {
		t.Error("expected module.new_vpc in moved block")
	}
}


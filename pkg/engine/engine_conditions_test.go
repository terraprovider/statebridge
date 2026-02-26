package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tfjson "github.com/hashicorp/terraform-json"

	"github.com/redtenant/tfmigrate/internal/testutil"
)

func TestEngine_ProcessFiles_ConditionMet(t *testing.T) {
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
description: "Move with met condition"
condition:
  resources_exist:
    - layer: "` + srcLayer + `"
      addresses:
        - "aws_instance.web"
operations:
  - type: move
    source_layer: "` + srcLayer + `"
    destination_layer: "` + dstLayer + `"
    resources:
      - from: "aws_instance.web"
        import_id: "i-0abc123"
`
	migrationFile := filepath.Join(dir, "001_cond.yaml")
	if err := os.WriteFile(migrationFile, []byte(migrationContent), 0o644); err != nil {
		t.Fatal(err)
	}

	mock := testutil.NewMockStateReader(map[string]*tfjson.State{
		srcLayer: testutil.BuildState(
			testutil.NewResource("aws_instance.web", "aws_instance", "web", nil, map[string]interface{}{
				"id": "i-0abc123",
			}),
		),
	})

	engine := New(Config{StateReader: mock})

	result, err := engine.ProcessFiles(context.Background(), []string{migrationFile})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.OutputFiles) != 2 {
		t.Fatalf("expected 2 files (condition met, migration proceeds), got %d", len(result.OutputFiles))
	}
}


func TestEngine_ProcessFiles_ConditionResourcesExistFails(t *testing.T) {
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
description: "Move with failing condition"
condition:
  resources_exist:
    - layer: "` + srcLayer + `"
      addresses:
        - "aws_instance.web"
        - "aws_instance.missing"
operations:
  - type: move
    source_layer: "` + srcLayer + `"
    destination_layer: "` + dstLayer + `"
    resources:
      - from: "aws_instance.web"
        import_id: "i-0abc123"
`
	migrationFile := filepath.Join(dir, "001_cond_fail.yaml")
	if err := os.WriteFile(migrationFile, []byte(migrationContent), 0o644); err != nil {
		t.Fatal(err)
	}

	mock := testutil.NewMockStateReader(map[string]*tfjson.State{
		srcLayer: testutil.BuildState(
			testutil.NewResource("aws_instance.web", "aws_instance", "web", nil, map[string]interface{}{
				"id": "i-0abc123",
			}),
		),
	})

	engine := New(Config{StateReader: mock})

	result, err := engine.ProcessFiles(context.Background(), []string{migrationFile})
	if err != nil {
		t.Fatalf("unexpected error (should silently skip): %v", err)
	}

	if len(result.OutputFiles) != 0 {
		t.Errorf("expected 0 files (condition not met, migration skipped), got %d", len(result.OutputFiles))
	}
}


func TestEngine_ProcessFiles_ConditionResourcesNotExistFails(t *testing.T) {
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
description: "Move blocked by destination check"
condition:
  resources_not_exist:
    - layer: "` + dstLayer + `"
      addresses:
        - "aws_instance.web"
operations:
  - type: move
    source_layer: "` + srcLayer + `"
    destination_layer: "` + dstLayer + `"
    resources:
      - from: "aws_instance.web"
        import_id: "i-0abc123"
`
	migrationFile := filepath.Join(dir, "001_not_exist_fail.yaml")
	if err := os.WriteFile(migrationFile, []byte(migrationContent), 0o644); err != nil {
		t.Fatal(err)
	}

	mock := testutil.NewMockStateReader(map[string]*tfjson.State{
		srcLayer: testutil.BuildState(
			testutil.NewResource("aws_instance.web", "aws_instance", "web", nil, map[string]interface{}{
				"id": "i-0abc123",
			}),
		),
		dstLayer: testutil.BuildState(
			testutil.NewResource("aws_instance.web", "aws_instance", "web", nil, map[string]interface{}{
				"id": "i-0abc123",
			}),
		),
	})

	engine := New(Config{StateReader: mock})

	result, err := engine.ProcessFiles(context.Background(), []string{migrationFile})
	if err != nil {
		t.Fatalf("unexpected error (should silently skip): %v", err)
	}

	if len(result.OutputFiles) != 0 {
		t.Errorf("expected 0 files (resource already exists in destination), got %d", len(result.OutputFiles))
	}
}


func TestEngine_ProcessFiles_ConditionStateError(t *testing.T) {
	dir := t.TempDir()

	migrationContent := `
description: "Move with state error"
condition:
  resources_exist:
    - layer: "/nonexistent/layer"
      addresses:
        - "aws_instance.web"
operations:
  - type: move
    source_layer: "/nonexistent/layer"
    destination_layer: "/other/layer"
    resources:
      - from: "aws_instance.web"
        import_id: "i-0abc123"
`
	migrationFile := filepath.Join(dir, "001_state_err.yaml")
	if err := os.WriteFile(migrationFile, []byte(migrationContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// MockStateReader returns error for unknown layers
	mock := testutil.NewMockStateReader(nil)

	// In strict mode, missing layers cause hard errors.
	engine := New(Config{StateReader: mock, Strict: true})
	result, err := engine.ProcessFiles(context.Background(), []string{migrationFile})
	if err == nil {
		t.Fatal("expected error in strict mode when layer does not exist")
	}
	if result != nil {
		t.Error("expected nil result in strict mode error")
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("expected error to mention layer does not exist, got: %v", err)
	}

	// In non-strict mode (default), missing layers are gracefully auto-skipped.
	// Auto-skipped files do NOT count toward "all files skipped" error.
	engine2 := New(Config{StateReader: mock})
	result, err = engine2.ProcessFiles(context.Background(), []string{migrationFile})
	if err != nil {
		t.Fatalf("expected no error in non-strict mode (graceful auto-skip), got: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result in non-strict mode")
	}
	if len(result.SkippedFiles) != 1 {
		t.Fatalf("expected 1 skipped file, got %d", len(result.SkippedFiles))
	}
	if result.SkippedFiles[0].Reason != SkipLayerMissing {
		t.Errorf("expected SkipLayerMissing reason, got %v", result.SkippedFiles[0].Reason)
	}
}


func TestEngine_ProcessFiles_NoConditionUnchanged(t *testing.T) {
	dir := t.TempDir()
	layerDir := filepath.Join(dir, "layers", "net")
	if err := os.MkdirAll(layerDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// No condition field at all — should proceed normally
	migrationContent := `
description: "Rename without condition"
operations:
  - type: rename
    layer: "` + layerDir + `"
    renames:
      - from: "module.old"
        to: "module.new"
`
	migrationFile := filepath.Join(dir, "001_no_cond.yaml")
	if err := os.WriteFile(migrationFile, []byte(migrationContent), 0o644); err != nil {
		t.Fatal(err)
	}

	engine := New(Config{StateReader: testutil.NewMockStateReader(nil)})

	result, err := engine.ProcessFiles(context.Background(), []string{migrationFile})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.OutputFiles) != 1 {
		t.Fatalf("expected 1 file (no condition, proceeds normally), got %d", len(result.OutputFiles))
	}
}


func TestEngine_ProcessFiles_PartialSkip(t *testing.T) {
	// Two YAML files: first one references a missing resource and should be skipped,
	// second one is a simple rename and should succeed.
	dir := t.TempDir()
	srcLayer := filepath.Join(dir, "layers", "compute")
	dstLayer := filepath.Join(dir, "layers", "app")
	renameLayer := filepath.Join(dir, "layers", "net")
	for _, d := range []string{srcLayer, dstLayer, renameLayer} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	// First file: move with missing resource → will be skipped
	moveContent := `
description: "Move missing resource"
operations:
  - type: move
    source_layer: "` + srcLayer + `"
    destination_layer: "` + dstLayer + `"
    resources:
      - from: "aws_instance.gone"
        import_id: "i-gone"
`
	moveFile := filepath.Join(dir, "001_move.yaml")
	if err := os.WriteFile(moveFile, []byte(moveContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// Second file: simple rename → should succeed
	renameContent := `
description: "Rename VPC"
operations:
  - type: rename
    layer: "` + renameLayer + `"
    renames:
      - from: "module.old"
        to: "module.new"
`
	renameFile := filepath.Join(dir, "002_rename.yaml")
	if err := os.WriteFile(renameFile, []byte(renameContent), 0o644); err != nil {
		t.Fatal(err)
	}

	mock := testutil.NewMockStateReader(map[string]*tfjson.State{
		srcLayer: testutil.BuildState(), // empty state — resource not found
	})

	engine := New(Config{StateReader: mock})

	result, err := engine.ProcessFiles(context.Background(), []string{moveFile, renameFile})
	if err != nil {
		t.Fatalf("expected partial success (first skipped, second succeeds), got error: %v", err)
	}

	if len(result.OutputFiles) != 1 {
		t.Fatalf("expected 1 output file from the rename, got %d: %v", len(result.OutputFiles), result.OutputFiles)
	}

	content, err := os.ReadFile(result.OutputFiles[0])
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	if !strings.Contains(string(content), "moved {") {
		t.Error("expected moved block from the rename operation")
	}
}


func TestEngine_ProcessFiles_AllSkipped(t *testing.T) {
	// Two YAML files that both fail during processing. ProcessFiles should
	// return an error indicating all files were skipped.
	dir := t.TempDir()
	srcLayer := filepath.Join(dir, "layers", "compute")
	dstLayer := filepath.Join(dir, "layers", "app")
	if err := os.MkdirAll(srcLayer, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dstLayer, 0o755); err != nil {
		t.Fatal(err)
	}

	// Both files reference missing resources
	for i, name := range []string{"001_move_a.yaml", "002_move_b.yaml"} {
		content := `
description: "Move missing resource ` + strings.TrimSuffix(name, ".yaml") + `"
operations:
  - type: move
    source_layer: "` + srcLayer + `"
    destination_layer: "` + dstLayer + `"
    resources:
      - from: "aws_instance.missing` + fmt.Sprintf("%d", i) + `"
        import_id: "i-missing"
`
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	mock := testutil.NewMockStateReader(map[string]*tfjson.State{
		srcLayer: testutil.BuildState(), // empty state
	})

	engine := New(Config{StateReader: mock})

	_, err := engine.ProcessFiles(context.Background(), []string{
		filepath.Join(dir, "001_move_a.yaml"),
		filepath.Join(dir, "002_move_b.yaml"),
	})
	if err == nil {
		t.Fatal("expected error when all migration files are skipped")
	}
	if !strings.Contains(err.Error(), "skipped") {
		t.Errorf("expected error to mention 'skipped', got: %v", err)
	}
	if !strings.Contains(err.Error(), "001_move_a.yaml") {
		t.Errorf("expected error to mention first file, got: %v", err)
	}
	if !strings.Contains(err.Error(), "002_move_b.yaml") {
		t.Errorf("expected error to mention second file, got: %v", err)
	}
}


func TestEngine_ProcessFiles_ConditionErrorWithSuccessfulFile(t *testing.T) {
	// One file has a condition that fails with a state error, another succeeds.
	dir := t.TempDir()
	layerDir := filepath.Join(dir, "layers", "net")
	if err := os.MkdirAll(layerDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// First file: condition references nonexistent layer → state error → skip
	condContent := `
description: "Move with bad condition"
condition:
  resources_exist:
    - layer: "/nonexistent/layer"
      addresses:
        - "aws_instance.web"
operations:
  - type: move
    source_layer: "/nonexistent/layer"
    destination_layer: "/other/layer"
    resources:
      - from: "aws_instance.web"
        import_id: "i-0abc123"
`
	condFile := filepath.Join(dir, "001_cond_err.yaml")
	if err := os.WriteFile(condFile, []byte(condContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// Second file: simple rename → succeeds
	renameContent := `
description: "Simple rename"
operations:
  - type: rename
    layer: "` + layerDir + `"
    renames:
      - from: "module.old"
        to: "module.new"
`
	renameFile := filepath.Join(dir, "002_rename.yaml")
	if err := os.WriteFile(renameFile, []byte(renameContent), 0o644); err != nil {
		t.Fatal(err)
	}

	mock := testutil.NewMockStateReader(nil) // no state configured → error for any reads

	engine := New(Config{StateReader: mock})

	result, err := engine.ProcessFiles(context.Background(), []string{condFile, renameFile})
	if err != nil {
		t.Fatalf("expected partial success, got error: %v", err)
	}

	if len(result.OutputFiles) != 1 {
		t.Fatalf("expected 1 output file from the rename, got %d: %v", len(result.OutputFiles), result.OutputFiles)
	}

	content, err := os.ReadFile(result.OutputFiles[0])
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	if !strings.Contains(string(content), "moved {") {
		t.Error("expected moved block from the rename operation")
	}
}


func TestEngine_ProcessFiles_LayerExistsCondition(t *testing.T) {
	dir := t.TempDir()
	layerDir := filepath.Join(dir, "layers", "net")
	existingDir := filepath.Join(dir, "layers", "source")
	if err := os.MkdirAll(layerDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(existingDir, 0o755); err != nil {
		t.Fatal(err)
	}

	nonExistentDir := filepath.Join(dir, "layers", "gone")

	// Test 1: layer_exists pointing to existing directory → proceeds
	content1 := `
description: "Rename with existing layer condition"
condition:
  layer_exists:
    - "` + existingDir + `"
operations:
  - type: rename
    layer: "` + layerDir + `"
    renames:
      - from: "module.old"
        to: "module.new"
`
	file1 := filepath.Join(dir, "001_exists.yaml")
	if err := os.WriteFile(file1, []byte(content1), 0o644); err != nil {
		t.Fatal(err)
	}

	engine := New(Config{StateReader: testutil.NewMockStateReader(nil)})
	result, err := engine.ProcessFiles(context.Background(), []string{file1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.OutputFiles) != 1 {
		t.Fatalf("expected 1 file (condition met), got %d", len(result.OutputFiles))
	}

	// Test 2: layer_exists pointing to non-existent directory → skipped
	content2 := `
description: "Rename with missing layer condition"
condition:
  layer_exists:
    - "` + nonExistentDir + `"
operations:
  - type: rename
    layer: "` + layerDir + `"
    renames:
      - from: "module.alpha"
        to: "module.beta"
`
	file2 := filepath.Join(dir, "002_missing.yaml")
	if err := os.WriteFile(file2, []byte(content2), 0o644); err != nil {
		t.Fatal(err)
	}

	engine2 := New(Config{StateReader: testutil.NewMockStateReader(nil)})
	result, err = engine2.ProcessFiles(context.Background(), []string{file2})
	if err != nil {
		t.Fatalf("unexpected error (should skip silently): %v", err)
	}
	if len(result.OutputFiles) != 0 {
		t.Errorf("expected 0 files (condition not met, skipped), got %d", len(result.OutputFiles))
	}
}


func TestEngine_ProcessFiles_LayerNotExistsCondition(t *testing.T) {
	dir := t.TempDir()
	layerDir := filepath.Join(dir, "layers", "net")
	existingDir := filepath.Join(dir, "layers", "still_here")
	if err := os.MkdirAll(layerDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(existingDir, 0o755); err != nil {
		t.Fatal(err)
	}

	nonExistentDir := filepath.Join(dir, "layers", "deleted")

	// Test 1: layer_not_exists pointing to existing directory → skipped
	content1 := `
description: "Rename blocked by existing layer"
condition:
  layer_not_exists:
    - "` + existingDir + `"
operations:
  - type: rename
    layer: "` + layerDir + `"
    renames:
      - from: "module.old"
        to: "module.new"
`
	file1 := filepath.Join(dir, "001_blocked.yaml")
	if err := os.WriteFile(file1, []byte(content1), 0o644); err != nil {
		t.Fatal(err)
	}

	engine := New(Config{StateReader: testutil.NewMockStateReader(nil)})
	result, err := engine.ProcessFiles(context.Background(), []string{file1})
	if err != nil {
		t.Fatalf("unexpected error (should skip silently): %v", err)
	}
	if len(result.OutputFiles) != 0 {
		t.Errorf("expected 0 files (layer exists, condition fails), got %d", len(result.OutputFiles))
	}

	// Test 2: layer_not_exists pointing to non-existent directory → proceeds
	content2 := `
description: "Rename when layer is gone"
condition:
  layer_not_exists:
    - "` + nonExistentDir + `"
operations:
  - type: rename
    layer: "` + layerDir + `"
    renames:
      - from: "module.alpha"
        to: "module.beta"
`
	file2 := filepath.Join(dir, "002_allowed.yaml")
	if err := os.WriteFile(file2, []byte(content2), 0o644); err != nil {
		t.Fatal(err)
	}

	engine2 := New(Config{StateReader: testutil.NewMockStateReader(nil)})
	result, err = engine2.ProcessFiles(context.Background(), []string{file2})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.OutputFiles) != 1 {
		t.Fatalf("expected 1 file (condition met, proceeds), got %d", len(result.OutputFiles))
	}
}



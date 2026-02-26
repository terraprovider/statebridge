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

func TestEngine_ProcessFiles_ModuleMove(t *testing.T) {
	// Move an entire module (module.foo) with 2 resources to a new layer.
	// Should generate: 1 consolidated removed block + 2 import blocks.
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
description: "Move entire module.foo"
operations:
  - type: move
    source_layer: "` + srcLayer + `"
    destination_layer: "` + dstLayer + `"
    resources:
      - from: "module.foo"
`
	migrationFile := filepath.Join(dir, "001_module_move.yaml")
	if err := os.WriteFile(migrationFile, []byte(migrationContent), 0o644); err != nil {
		t.Fatal(err)
	}

	mock := testutil.NewMockStateReader(map[string]*tfjson.State{
		srcLayer: testutil.BuildState(
			testutil.NewResource("module.foo.aws_instance.web", "aws_instance", "web", nil,
				map[string]interface{}{"id": "i-123"}),
			testutil.NewResource("module.foo.aws_s3_bucket.data", "aws_s3_bucket", "data", nil,
				map[string]interface{}{"id": "bucket-123"}),
		),
	})

	engine := New(Config{StateReader: mock})

	result, err := engine.ProcessFiles(context.Background(), []string{migrationFile})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.OutputFiles) != 2 {
		t.Fatalf("expected 2 files (source + destination), got %d: %v", len(result.OutputFiles), result.OutputFiles)
	}

	// Source layer: consolidated removed block for module.foo
	srcContent := readLayerFile(t, result.OutputFiles, srcLayer)
	if !strings.Contains(srcContent, "module.foo") {
		t.Error("expected module.foo in removed block (consolidated)")
	}

	// Destination layer: 2 import blocks
	dstContent := readLayerFile(t, result.OutputFiles, dstLayer)
	if strings.Count(dstContent, "import {") != 2 {
		t.Errorf("expected 2 import blocks, got:\n%s", dstContent)
	}
	if !strings.Contains(dstContent, "module.foo.aws_instance.web") {
		t.Error("expected module.foo.aws_instance.web import")
	}
	if !strings.Contains(dstContent, "module.foo.aws_s3_bucket.data") {
		t.Error("expected module.foo.aws_s3_bucket.data import")
	}
	if !strings.Contains(dstContent, `"i-123"`) {
		t.Error("expected import ID i-123")
	}
	if !strings.Contains(dstContent, `"bucket-123"`) {
		t.Error("expected import ID bucket-123")
	}
}


func TestEngine_ProcessFiles_ModuleMoveWithDestinationAddress(t *testing.T) {
	// Move module.foo to module.bar — prefix swap.
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
description: "Move module.foo to module.bar"
operations:
  - type: move
    source_layer: "` + srcLayer + `"
    destination_layer: "` + dstLayer + `"
    resources:
      - from: "module.foo"
        to: "module.bar"
`
	migrationFile := filepath.Join(dir, "001_module_rename.yaml")
	if err := os.WriteFile(migrationFile, []byte(migrationContent), 0o644); err != nil {
		t.Fatal(err)
	}

	mock := testutil.NewMockStateReader(map[string]*tfjson.State{
		srcLayer: testutil.BuildState(
			testutil.NewResource("module.foo.aws_instance.web", "aws_instance", "web", nil,
				map[string]interface{}{"id": "i-123"}),
		),
	})

	engine := New(Config{StateReader: mock})

	result, err := engine.ProcessFiles(context.Background(), []string{migrationFile})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	dstContent := readLayerFile(t, result.OutputFiles, dstLayer)
	// module.foo.aws_instance.web → module.bar.aws_instance.web
	if !strings.Contains(dstContent, "module.bar.aws_instance.web") {
		t.Errorf("expected module prefix swapped to module.bar, got:\n%s", dstContent)
	}
	if strings.Contains(dstContent, "module.foo") {
		t.Error("should not contain module.foo in destination")
	}
}


func TestEngine_ProcessFiles_ModuleMoveWithAddressPrefix(t *testing.T) {
	// Move with address_prefix: "module.ig" and resource address: "module.foo"
	// Effective source: module.ig.module.foo
	dir := t.TempDir()
	srcLayer := filepath.Join(dir, "layers", "old")
	dstLayer := filepath.Join(dir, "layers", "new")
	if err := os.MkdirAll(srcLayer, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dstLayer, 0o755); err != nil {
		t.Fatal(err)
	}

	migrationContent := `
description: "Module move with address prefix"
operations:
  - type: move
    source_layer: "` + srcLayer + `"
    destination_layer: "` + dstLayer + `"
    address_prefix: "module.ig"
    resources:
      - from: "module.foo"
`
	migrationFile := filepath.Join(dir, "001_prefix_module.yaml")
	if err := os.WriteFile(migrationFile, []byte(migrationContent), 0o644); err != nil {
		t.Fatal(err)
	}

	mock := testutil.NewMockStateReader(map[string]*tfjson.State{
		srcLayer: testutil.BuildState(
			testutil.NewResource("module.ig.module.foo.aws_instance.web", "aws_instance", "web", nil,
				map[string]interface{}{"id": "i-123"}),
		),
	})

	engine := New(Config{StateReader: mock})

	result, err := engine.ProcessFiles(context.Background(), []string{migrationFile})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	dstContent := readLayerFile(t, result.OutputFiles, dstLayer)
	if !strings.Contains(dstContent, "module.ig.module.foo.aws_instance.web") {
		t.Errorf("expected full prefixed address in destination, got:\n%s", dstContent)
	}
}


func TestEngine_ProcessFiles_ModuleMoveNestedModules(t *testing.T) {
	// Move module.foo which contains nested module.foo.module.bar resources.
	dir := t.TempDir()
	srcLayer := filepath.Join(dir, "layers", "old")
	dstLayer := filepath.Join(dir, "layers", "new")
	if err := os.MkdirAll(srcLayer, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dstLayer, 0o755); err != nil {
		t.Fatal(err)
	}

	migrationContent := `
description: "Move module with nested submodules"
operations:
  - type: move
    source_layer: "` + srcLayer + `"
    destination_layer: "` + dstLayer + `"
    resources:
      - from: "module.foo"
`
	migrationFile := filepath.Join(dir, "001_nested.yaml")
	if err := os.WriteFile(migrationFile, []byte(migrationContent), 0o644); err != nil {
		t.Fatal(err)
	}

	mock := testutil.NewMockStateReader(map[string]*tfjson.State{
		srcLayer: testutil.BuildState(
			testutil.NewResource("module.foo.aws_instance.web", "aws_instance", "web", nil,
				map[string]interface{}{"id": "i-123"}),
			testutil.NewResource("module.foo.module.bar.aws_s3_bucket.logs", "aws_s3_bucket", "logs", nil,
				map[string]interface{}{"id": "bucket-456"}),
		),
	})

	engine := New(Config{StateReader: mock})

	result, err := engine.ProcessFiles(context.Background(), []string{migrationFile})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	dstContent := readLayerFile(t, result.OutputFiles, dstLayer)
	if strings.Count(dstContent, "import {") != 2 {
		t.Errorf("expected 2 import blocks for nested module resources, got:\n%s", dstContent)
	}
	if !strings.Contains(dstContent, "module.foo.aws_instance.web") {
		t.Error("expected direct child resource import")
	}
	if !strings.Contains(dstContent, "module.foo.module.bar.aws_s3_bucket.logs") {
		t.Error("expected nested module resource import")
	}

	// Source should have module-level consolidated removed block
	srcContent := readLayerFile(t, result.OutputFiles, srcLayer)
	if strings.Count(srcContent, "removed {") != 1 {
		t.Errorf("expected 1 consolidated removed block, got:\n%s", srcContent)
	}
	if !strings.Contains(srcContent, "module.foo") {
		t.Error("expected module.foo in consolidated removed block")
	}
}


func TestEngine_ProcessFiles_ModuleMoveForEach(t *testing.T) {
	// Move a module containing for_each resources.
	dir := t.TempDir()
	srcLayer := filepath.Join(dir, "layers", "old")
	dstLayer := filepath.Join(dir, "layers", "new")
	if err := os.MkdirAll(srcLayer, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dstLayer, 0o755); err != nil {
		t.Fatal(err)
	}

	migrationContent := `
description: "Move module with for_each resources"
operations:
  - type: move
    source_layer: "` + srcLayer + `"
    destination_layer: "` + dstLayer + `"
    resources:
      - from: "module.foo"
        to: "module.bar"
`
	migrationFile := filepath.Join(dir, "001_foreach.yaml")
	if err := os.WriteFile(migrationFile, []byte(migrationContent), 0o644); err != nil {
		t.Fatal(err)
	}

	mock := testutil.NewMockStateReader(map[string]*tfjson.State{
		srcLayer: testutil.BuildState(
			testutil.NewResource(`module.foo.aws_s3_bucket.data["key-a"]`, "aws_s3_bucket", "data", "key-a",
				map[string]interface{}{"id": "bucket-a"}),
			testutil.NewResource(`module.foo.aws_s3_bucket.data["key-b"]`, "aws_s3_bucket", "data", "key-b",
				map[string]interface{}{"id": "bucket-b"}),
			testutil.NewResource("module.foo.aws_instance.web", "aws_instance", "web", nil,
				map[string]interface{}{"id": "i-123"}),
		),
	})

	engine := New(Config{StateReader: mock})

	result, err := engine.ProcessFiles(context.Background(), []string{migrationFile})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	dstContent := readLayerFile(t, result.OutputFiles, dstLayer)
	if strings.Count(dstContent, "import {") != 3 {
		t.Errorf("expected 3 import blocks, got:\n%s", dstContent)
	}
	// for_each keys should be preserved with new module prefix
	if !strings.Contains(dstContent, `module.bar.aws_s3_bucket.data["key-a"]`) {
		t.Error("expected key-a with module.bar prefix")
	}
	if !strings.Contains(dstContent, `module.bar.aws_s3_bucket.data["key-b"]`) {
		t.Error("expected key-b with module.bar prefix")
	}
	if !strings.Contains(dstContent, "module.bar.aws_instance.web") {
		t.Error("expected module.bar.aws_instance.web import")
	}
}


func TestEngine_ProcessFiles_ModuleMoveNoResources(t *testing.T) {
	// Module with no resources → should fail (and be skipped since it's the only file).
	dir := t.TempDir()
	srcLayer := filepath.Join(dir, "layers", "old")
	dstLayer := filepath.Join(dir, "layers", "new")
	if err := os.MkdirAll(srcLayer, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dstLayer, 0o755); err != nil {
		t.Fatal(err)
	}

	migrationContent := `
description: "Move empty module"
operations:
  - type: move
    source_layer: "` + srcLayer + `"
    destination_layer: "` + dstLayer + `"
    resources:
      - from: "module.empty"
`
	migrationFile := filepath.Join(dir, "001_empty_module.yaml")
	if err := os.WriteFile(migrationFile, []byte(migrationContent), 0o644); err != nil {
		t.Fatal(err)
	}

	mock := testutil.NewMockStateReader(map[string]*tfjson.State{
		srcLayer: testutil.BuildState(), // empty state
	})

	engine := New(Config{StateReader: mock})

	_, err := engine.ProcessFiles(context.Background(), []string{migrationFile})
	if err == nil {
		t.Fatal("expected error when module has no resources")
	}
	if !strings.Contains(err.Error(), "skipped") {
		t.Errorf("expected error mentioning skipped, got: %v", err)
	}
}


func TestEngine_ProcessFiles_ModuleMoveNestedRename(t *testing.T) {
	// Move module.foo to module.bar with nested submodules — prefix swap applies to all.
	dir := t.TempDir()
	srcLayer := filepath.Join(dir, "layers", "old")
	dstLayer := filepath.Join(dir, "layers", "new")
	if err := os.MkdirAll(srcLayer, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dstLayer, 0o755); err != nil {
		t.Fatal(err)
	}

	migrationContent := `
description: "Move module.foo to module.bar with nested modules"
operations:
  - type: move
    source_layer: "` + srcLayer + `"
    destination_layer: "` + dstLayer + `"
    resources:
      - from: "module.foo"
        to: "module.bar"
`
	migrationFile := filepath.Join(dir, "001_nested_rename.yaml")
	if err := os.WriteFile(migrationFile, []byte(migrationContent), 0o644); err != nil {
		t.Fatal(err)
	}

	mock := testutil.NewMockStateReader(map[string]*tfjson.State{
		srcLayer: testutil.BuildState(
			testutil.NewResource("module.foo.aws_instance.web", "aws_instance", "web", nil,
				map[string]interface{}{"id": "i-123"}),
			testutil.NewResource("module.foo.module.baz.aws_s3_bucket.logs", "aws_s3_bucket", "logs", nil,
				map[string]interface{}{"id": "bucket-456"}),
		),
	})

	engine := New(Config{StateReader: mock})

	result, err := engine.ProcessFiles(context.Background(), []string{migrationFile})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.OutputFiles) != 2 {
		t.Fatalf("expected 2 files (source + dest), got %d", len(result.OutputFiles))
	}

	dstContent := readLayerFile(t, result.OutputFiles, dstLayer)

	// module.foo.aws_instance.web → module.bar.aws_instance.web
	if !strings.Contains(dstContent, "module.bar.aws_instance.web") {
		t.Errorf("expected module.bar.aws_instance.web in destination, got:\n%s", dstContent)
	}

	// module.foo.module.baz.aws_s3_bucket.logs → module.bar.module.baz.aws_s3_bucket.logs
	if !strings.Contains(dstContent, "module.bar.module.baz.aws_s3_bucket.logs") {
		t.Errorf("expected module.bar.module.baz.aws_s3_bucket.logs in destination, got:\n%s", dstContent)
	}

	// Original prefix should not appear in destination
	if strings.Contains(dstContent, "module.foo") {
		t.Errorf("destination should not contain module.foo, got:\n%s", dstContent)
	}

	// Source layer should have consolidated removed block for module.foo
	srcContent := readLayerFile(t, result.OutputFiles, srcLayer)
	if !strings.Contains(srcContent, "module.foo") {
		t.Error("expected module.foo in source removed block")
	}
}



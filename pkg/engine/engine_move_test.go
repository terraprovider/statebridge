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

func TestEngine_ProcessFiles_SimpleMove(t *testing.T) {
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
description: "Move web instance"
operations:
  - type: move
    description: "Move to app layer"
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
		t.Fatalf("expected 2 files, got %d: %v", len(result.OutputFiles), result.OutputFiles)
	}

	// Find source and destination files from returned paths
	var srcFile, dstFile string
	for _, f := range result.OutputFiles {
		if strings.HasPrefix(f, srcLayer) {
			srcFile = f
		} else if strings.HasPrefix(f, dstLayer) {
			dstFile = f
		}
	}
	if srcFile == "" || dstFile == "" {
		t.Fatalf("expected one file per layer, got: %v", result.OutputFiles)
	}

	srcContent, err := os.ReadFile(srcFile)
	if err != nil {
		t.Fatalf("reading source migration file: %v", err)
	}
	if !strings.Contains(string(srcContent), "removed {") {
		t.Error("expected removed block in source layer")
	}
	if !strings.Contains(string(srcContent), "aws_instance.web") {
		t.Error("expected resource address in source layer")
	}

	dstContent, err := os.ReadFile(dstFile)
	if err != nil {
		t.Fatalf("reading destination migration file: %v", err)
	}
	if !strings.Contains(string(dstContent), "import {") {
		t.Error("expected import block in destination layer")
	}
	if !strings.Contains(string(dstContent), `"i-0abc123"`) {
		t.Error("expected import ID in destination layer")
	}
}


func TestEngine_ProcessFiles_ForEachMoveAllKeys(t *testing.T) {
	dir := t.TempDir()
	srcLayer := filepath.Join(dir, "layers", "old")
	dstLayer := filepath.Join(dir, "layers", "new")
	if err := os.MkdirAll(srcLayer, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dstLayer, 0o755); err != nil {
		t.Fatal(err)
	}

	// Move a for_each resource without keys map → moves all instances with same keys
	migrationContent := `
description: "Move all instances"
operations:
  - type: move
    source_layer: "` + srcLayer + `"
    destination_layer: "` + dstLayer + `"
    resources:
      - from: "aws_s3_bucket.data"
`
	migrationFile := filepath.Join(dir, "001_all.yaml")
	if err := os.WriteFile(migrationFile, []byte(migrationContent), 0o644); err != nil {
		t.Fatal(err)
	}

	mock := testutil.NewMockStateReader(map[string]*tfjson.State{
		srcLayer: testutil.BuildState(
			testutil.NewResource(
				`aws_s3_bucket.data["key-a"]`, "aws_s3_bucket", "data", "key-a",
				map[string]interface{}{"id": "bucket-a-id"},
			),
			testutil.NewResource(
				`aws_s3_bucket.data["key-b"]`, "aws_s3_bucket", "data", "key-b",
				map[string]interface{}{"id": "bucket-b-id"},
			),
		),
	})

	engine := New(Config{StateReader: mock})

	result, err := engine.ProcessFiles(context.Background(), []string{migrationFile})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.OutputFiles) != 2 {
		t.Fatalf("expected 2 files, got %d: %v", len(result.OutputFiles), result.OutputFiles)
	}

	srcContent := readLayerFile(t, result.OutputFiles, srcLayer)
	if strings.Count(srcContent, "removed {") != 1 {
		t.Errorf("expected 1 removed block, got:\n%s", srcContent)
	}

	dstContent := readLayerFile(t, result.OutputFiles, dstLayer)
	if strings.Count(dstContent, "import {") != 2 {
		t.Errorf("expected 2 import blocks, got:\n%s", dstContent)
	}
	if !strings.Contains(dstContent, `aws_s3_bucket.data["key-a"]`) {
		t.Error("expected key-a in destination")
	}
	if !strings.Contains(dstContent, `aws_s3_bucket.data["key-b"]`) {
		t.Error("expected key-b in destination")
	}
}


func TestEngine_ProcessFiles_MultipleOperationsSameLayer(t *testing.T) {
	dir := t.TempDir()
	layerDir := filepath.Join(dir, "layers", "net")
	if err := os.MkdirAll(layerDir, 0o755); err != nil {
		t.Fatal(err)
	}

	migrationContent := `
description: "Multiple operations in same layer"
operations:
  - type: rename
    layer: "` + layerDir + `"
    renames:
      - from: "module.old_vpc"
        to: "module.new_vpc"
  - type: remove
    layer: "` + layerDir + `"
    entries:
      - address: "aws_security_group.legacy"
  - type: import
    layer: "` + layerDir + `"
    imports:
      - address: "aws_route_table.new"
        id: "rtb-0abc123"
`
	migrationFile := filepath.Join(dir, "001_multi.yaml")
	if err := os.WriteFile(migrationFile, []byte(migrationContent), 0o644); err != nil {
		t.Fatal(err)
	}

	engine := New(Config{StateReader: testutil.NewMockStateReader(nil)})

	result, err := engine.ProcessFiles(context.Background(), []string{migrationFile})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.OutputFiles) != 1 {
		t.Fatalf("expected 1 file, got %d", len(result.OutputFiles))
	}

	content, err := os.ReadFile(result.OutputFiles[0])
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}

	contentStr := string(content)
	if !strings.Contains(contentStr, "moved {") {
		t.Error("expected moved block")
	}
	if !strings.Contains(contentStr, "removed {") {
		t.Error("expected removed block")
	}
	if !strings.Contains(contentStr, "import {") {
		t.Error("expected import block")
	}
}


func TestEngine_ProcessFiles_DestinationAddressOverride(t *testing.T) {
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
description: "Move with destination address override"
operations:
  - type: move
    source_layer: "` + srcLayer + `"
    destination_layer: "` + dstLayer + `"
    resources:
      - from: "module.old.resource.all"
        to: "module.new.resource.all"
        keys:
          key1: key1
`
	migrationFile := filepath.Join(dir, "001_dest_override.yaml")
	if err := os.WriteFile(migrationFile, []byte(migrationContent), 0o644); err != nil {
		t.Fatal(err)
	}

	mock := testutil.NewMockStateReader(map[string]*tfjson.State{
		srcLayer: testutil.BuildState(
			testutil.NewResource(
				`module.old.resource.all["key1"]`, "resource", "all", "key1",
				map[string]interface{}{"id": "id-key1"},
			),
		),
	})

	engine := New(Config{StateReader: mock})

	result, err := engine.ProcessFiles(context.Background(), []string{migrationFile})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	srcContent := readLayerFile(t, result.OutputFiles, srcLayer)
	// With module consolidation, the entire module.old is removed since
	// all managed resources within it are being moved.
	if !strings.Contains(srcContent, "module.old") {
		t.Error("expected module.old in removed block (consolidated)")
	}

	dstContent := readLayerFile(t, result.OutputFiles, dstLayer)
	if !strings.Contains(dstContent, `module.new.resource.all["key1"]`) {
		t.Errorf("expected destination_address override in import, got:\n%s", dstContent)
	}
}



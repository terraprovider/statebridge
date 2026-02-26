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

func TestEngine_ProcessFiles_AllResources(t *testing.T) {
	// Move all resources from one layer to another.
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
description: "Move all resources"
operations:
  - type: move
    source_layer: "` + srcLayer + `"
    destination_layer: "` + dstLayer + `"
    all_resources: true
`
	migrationFile := filepath.Join(dir, "001_all.yaml")
	if err := os.WriteFile(migrationFile, []byte(migrationContent), 0o644); err != nil {
		t.Fatal(err)
	}

	mock := testutil.NewMockStateReader(map[string]*tfjson.State{
		srcLayer: testutil.BuildState(
			testutil.NewResource("aws_instance.web", "aws_instance", "web", nil,
				map[string]interface{}{"id": "i-123"}),
			testutil.NewResource("aws_s3_bucket.data", "aws_s3_bucket", "data", nil,
				map[string]interface{}{"id": "bucket-123"}),
			testutil.NewResource("module.foo.aws_instance.api", "aws_instance", "api", nil,
				map[string]interface{}{"id": "i-456"}),
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

	dstContent := readLayerFile(t, result.OutputFiles, dstLayer)
	if strings.Count(dstContent, "import {") != 3 {
		t.Errorf("expected 3 import blocks, got:\n%s", dstContent)
	}
	if !strings.Contains(dstContent, "aws_instance.web") {
		t.Error("expected aws_instance.web import")
	}
	if !strings.Contains(dstContent, "aws_s3_bucket.data") {
		t.Error("expected aws_s3_bucket.data import")
	}
	if !strings.Contains(dstContent, "module.foo.aws_instance.api") {
		t.Error("expected module.foo.aws_instance.api import")
	}
}


func TestEngine_ProcessFiles_AllResourcesWithOverride(t *testing.T) {
	// Move all resources, but rename one.
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
description: "Move all, rename one"
operations:
  - type: move
    source_layer: "` + srcLayer + `"
    destination_layer: "` + dstLayer + `"
    all_resources: true
    overrides:
      - from: "aws_instance.web"
        to: "aws_instance.api"
`
	migrationFile := filepath.Join(dir, "001_rename.yaml")
	if err := os.WriteFile(migrationFile, []byte(migrationContent), 0o644); err != nil {
		t.Fatal(err)
	}

	mock := testutil.NewMockStateReader(map[string]*tfjson.State{
		srcLayer: testutil.BuildState(
			testutil.NewResource("aws_instance.web", "aws_instance", "web", nil,
				map[string]interface{}{"id": "i-123"}),
			testutil.NewResource("aws_s3_bucket.data", "aws_s3_bucket", "data", nil,
				map[string]interface{}{"id": "bucket-123"}),
		),
	})

	engine := New(Config{StateReader: mock})

	result, err := engine.ProcessFiles(context.Background(), []string{migrationFile})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	dstContent := readLayerFile(t, result.OutputFiles, dstLayer)
	// aws_instance.web should be renamed to aws_instance.api
	if !strings.Contains(dstContent, "aws_instance.api") {
		t.Errorf("expected aws_instance.api (renamed), got:\n%s", dstContent)
	}
	// aws_s3_bucket.data should keep its name
	if !strings.Contains(dstContent, "aws_s3_bucket.data") {
		t.Error("expected aws_s3_bucket.data to keep its address")
	}
	// aws_instance.web should NOT appear in destination (was renamed)
	if strings.Contains(dstContent, "aws_instance.web") {
		t.Error("aws_instance.web should have been renamed to aws_instance.api")
	}
}


func TestEngine_ProcessFiles_AllResourcesWithForEach(t *testing.T) {
	// Move all resources including for_each instances.
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
description: "Move all with for_each"
operations:
  - type: move
    source_layer: "` + srcLayer + `"
    destination_layer: "` + dstLayer + `"
    all_resources: true
`
	migrationFile := filepath.Join(dir, "001_foreach.yaml")
	if err := os.WriteFile(migrationFile, []byte(migrationContent), 0o644); err != nil {
		t.Fatal(err)
	}

	mock := testutil.NewMockStateReader(map[string]*tfjson.State{
		srcLayer: testutil.BuildState(
			testutil.NewResource(`aws_s3_bucket.data["key-a"]`, "aws_s3_bucket", "data", "key-a",
				map[string]interface{}{"id": "bucket-a"}),
			testutil.NewResource(`aws_s3_bucket.data["key-b"]`, "aws_s3_bucket", "data", "key-b",
				map[string]interface{}{"id": "bucket-b"}),
		),
	})

	engine := New(Config{StateReader: mock})

	result, err := engine.ProcessFiles(context.Background(), []string{migrationFile})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	dstContent := readLayerFile(t, result.OutputFiles, dstLayer)
	if strings.Count(dstContent, "import {") != 2 {
		t.Errorf("expected 2 import blocks for for_each instances, got:\n%s", dstContent)
	}
	if !strings.Contains(dstContent, `aws_s3_bucket.data["key-a"]`) {
		t.Error("expected key-a import")
	}
	if !strings.Contains(dstContent, `aws_s3_bucket.data["key-b"]`) {
		t.Error("expected key-b import")
	}

	srcContent := readLayerFile(t, result.OutputFiles, srcLayer)
	if strings.Count(srcContent, "removed {") != 1 {
		t.Errorf("expected 1 removed block (base address), got:\n%s", srcContent)
	}
}


func TestEngine_ProcessFiles_AllResourcesModuleConsolidation(t *testing.T) {
	// All resources include a full module → consolidation should kick in.
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
description: "All resources with module consolidation"
operations:
  - type: move
    source_layer: "` + srcLayer + `"
    destination_layer: "` + dstLayer + `"
    all_resources: true
`
	migrationFile := filepath.Join(dir, "001_consolidate.yaml")
	if err := os.WriteFile(migrationFile, []byte(migrationContent), 0o644); err != nil {
		t.Fatal(err)
	}

	mock := testutil.NewMockStateReader(map[string]*tfjson.State{
		srcLayer: testutil.BuildState(
			testutil.NewResource("aws_instance.standalone", "aws_instance", "standalone", nil,
				map[string]interface{}{"id": "i-000"}),
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

	srcContent := readLayerFile(t, result.OutputFiles, srcLayer)
	// Should have 2 removed blocks: aws_instance.standalone (root) + module.foo (consolidated)
	if strings.Count(srcContent, "removed {") != 2 {
		t.Errorf("expected 2 removed blocks (root + consolidated module), got:\n%s", srcContent)
	}
	if !strings.Contains(srcContent, "aws_instance.standalone") {
		t.Error("expected root resource removed block")
	}
	if !strings.Contains(srcContent, "module.foo") {
		t.Error("expected module.foo consolidated removed block")
	}
}


func TestEngine_ProcessFiles_AllResourcesEmptyState(t *testing.T) {
	// No resources in state → error.
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
description: "Move empty layer"
operations:
  - type: move
    source_layer: "` + srcLayer + `"
    destination_layer: "` + dstLayer + `"
    all_resources: true
`
	migrationFile := filepath.Join(dir, "001_empty.yaml")
	if err := os.WriteFile(migrationFile, []byte(migrationContent), 0o644); err != nil {
		t.Fatal(err)
	}

	mock := testutil.NewMockStateReader(map[string]*tfjson.State{
		srcLayer: testutil.BuildState(), // empty
	})

	engine := New(Config{StateReader: mock})

	_, err := engine.ProcessFiles(context.Background(), []string{migrationFile})
	if err == nil {
		t.Fatal("expected error for empty state")
	}
	if !strings.Contains(err.Error(), "skipped") {
		t.Errorf("expected error mentioning skipped, got: %v", err)
	}
}


func TestEngine_ProcessFiles_AllResourcesWithOmit(t *testing.T) {
	// Move 3 resources, omit 1 → destination gets 2 imports, source gets 3 removed blocks.
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
description: "Move all, omit one"
operations:
  - type: move
    source_layer: "` + srcLayer + `"
    destination_layer: "` + dstLayer + `"
    all_resources: true
    omit:
      - address: "aws_instance.ephemeral"
`
	migrationFile := filepath.Join(dir, "001_omit.yaml")
	if err := os.WriteFile(migrationFile, []byte(migrationContent), 0o644); err != nil {
		t.Fatal(err)
	}

	mock := testutil.NewMockStateReader(map[string]*tfjson.State{
		srcLayer: testutil.BuildState(
			testutil.NewResource("aws_instance.web", "aws_instance", "web", nil,
				map[string]interface{}{"id": "i-123"}),
			testutil.NewResource("aws_s3_bucket.data", "aws_s3_bucket", "data", nil,
				map[string]interface{}{"id": "bucket-123"}),
			testutil.NewResource("aws_instance.ephemeral", "aws_instance", "ephemeral", nil,
				map[string]interface{}{"id": "i-eph"}),
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

	dstContent := readLayerFile(t, result.OutputFiles, dstLayer)
	// Destination should have 2 import blocks (web + data), NOT ephemeral.
	if strings.Count(dstContent, "import {") != 2 {
		t.Errorf("expected 2 import blocks in destination, got:\n%s", dstContent)
	}
	if !strings.Contains(dstContent, "aws_instance.web") {
		t.Error("expected aws_instance.web import")
	}
	if !strings.Contains(dstContent, "aws_s3_bucket.data") {
		t.Error("expected aws_s3_bucket.data import")
	}
	if strings.Contains(dstContent, "aws_instance.ephemeral") {
		t.Error("aws_instance.ephemeral should be omitted from destination imports")
	}

	srcContent := readLayerFile(t, result.OutputFiles, srcLayer)
	// Source should have 3 removed blocks (web, data, ephemeral).
	if strings.Count(srcContent, "removed {") != 3 {
		t.Errorf("expected 3 removed blocks in source, got:\n%s", srcContent)
	}
	if !strings.Contains(srcContent, "aws_instance.ephemeral") {
		t.Error("expected aws_instance.ephemeral in source removed blocks")
	}
}


func TestEngine_ProcessFiles_AllResourcesWithOmitDestroy(t *testing.T) {
	// Omit with destroy=true → verify removed block contains "destroy = true".
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
description: "Move all, omit with destroy"
operations:
  - type: move
    source_layer: "` + srcLayer + `"
    destination_layer: "` + dstLayer + `"
    all_resources: true
    omit:
      - address: "aws_instance.ephemeral"
        destroy: true
`
	migrationFile := filepath.Join(dir, "001_omit_destroy.yaml")
	if err := os.WriteFile(migrationFile, []byte(migrationContent), 0o644); err != nil {
		t.Fatal(err)
	}

	mock := testutil.NewMockStateReader(map[string]*tfjson.State{
		srcLayer: testutil.BuildState(
			testutil.NewResource("aws_instance.web", "aws_instance", "web", nil,
				map[string]interface{}{"id": "i-123"}),
			testutil.NewResource("aws_instance.ephemeral", "aws_instance", "ephemeral", nil,
				map[string]interface{}{"id": "i-eph"}),
		),
	})

	engine := New(Config{StateReader: mock})

	result, err := engine.ProcessFiles(context.Background(), []string{migrationFile})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	srcContent := readLayerFile(t, result.OutputFiles, srcLayer)
	// Should have removed block for ephemeral with destroy = true
	if !strings.Contains(srcContent, "destroy = true") {
		t.Errorf("expected 'destroy = true' for omitted resource, got:\n%s", srcContent)
	}

	dstContent := readLayerFile(t, result.OutputFiles, dstLayer)
	// Only aws_instance.web should have an import block
	if strings.Count(dstContent, "import {") != 1 {
		t.Errorf("expected 1 import block, got:\n%s", dstContent)
	}
	if strings.Contains(dstContent, "aws_instance.ephemeral") {
		t.Error("aws_instance.ephemeral should not have an import block")
	}
}


func TestEngine_ProcessFiles_AllResourcesWithOmitAndOverride(t *testing.T) {
	// Omit one resource, rename another, move the rest normally.
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
description: "Move all, omit one, rename one"
operations:
  - type: move
    source_layer: "` + srcLayer + `"
    destination_layer: "` + dstLayer + `"
    all_resources: true
    overrides:
      - from: "aws_instance.web"
        to: "aws_instance.api"
    omit:
      - address: "aws_instance.ephemeral"
`
	migrationFile := filepath.Join(dir, "001_omit_override.yaml")
	if err := os.WriteFile(migrationFile, []byte(migrationContent), 0o644); err != nil {
		t.Fatal(err)
	}

	mock := testutil.NewMockStateReader(map[string]*tfjson.State{
		srcLayer: testutil.BuildState(
			testutil.NewResource("aws_instance.web", "aws_instance", "web", nil,
				map[string]interface{}{"id": "i-123"}),
			testutil.NewResource("aws_s3_bucket.data", "aws_s3_bucket", "data", nil,
				map[string]interface{}{"id": "bucket-123"}),
			testutil.NewResource("aws_instance.ephemeral", "aws_instance", "ephemeral", nil,
				map[string]interface{}{"id": "i-eph"}),
		),
	})

	engine := New(Config{StateReader: mock})

	result, err := engine.ProcessFiles(context.Background(), []string{migrationFile})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	dstContent := readLayerFile(t, result.OutputFiles, dstLayer)
	// aws_instance.web → aws_instance.api (renamed), aws_s3_bucket.data (unchanged)
	if strings.Count(dstContent, "import {") != 2 {
		t.Errorf("expected 2 import blocks, got:\n%s", dstContent)
	}
	if !strings.Contains(dstContent, "aws_instance.api") {
		t.Error("expected aws_instance.api (renamed from web)")
	}
	if strings.Contains(dstContent, "aws_instance.web") {
		t.Error("aws_instance.web should have been renamed to aws_instance.api")
	}
	if !strings.Contains(dstContent, "aws_s3_bucket.data") {
		t.Error("expected aws_s3_bucket.data import")
	}
	if strings.Contains(dstContent, "aws_instance.ephemeral") {
		t.Error("aws_instance.ephemeral should be omitted from imports")
	}
}


func TestEngine_ProcessFiles_AllResourcesWithImportIDOverride(t *testing.T) {
	// Move all resources, but override import_id for one resource.
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
description: "Move all, override import_id for one"
operations:
  - type: move
    source_layer: "` + srcLayer + `"
    destination_layer: "` + dstLayer + `"
    all_resources: true
    overrides:
      - from: "azuredevops_serviceendpoint_azurerm.key_vault"
        import_id: "{{ .Attributes.project_id }}/{{ .Attributes.id }}"
`
	migrationFile := filepath.Join(dir, "001_import_override.yaml")
	if err := os.WriteFile(migrationFile, []byte(migrationContent), 0o644); err != nil {
		t.Fatal(err)
	}

	mock := testutil.NewMockStateReader(map[string]*tfjson.State{
		srcLayer: testutil.BuildState(
			testutil.NewResource("aws_instance.web", "aws_instance", "web", nil,
				map[string]interface{}{"id": "i-web123"}),
			testutil.NewResource("azuredevops_serviceendpoint_azurerm.key_vault", "azuredevops_serviceendpoint_azurerm", "key_vault", nil,
				map[string]interface{}{"id": "endpoint-id", "project_id": "proj-123"}),
		),
	})

	engine := New(Config{StateReader: mock})

	result, err := engine.ProcessFiles(context.Background(), []string{migrationFile})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	dstContent := readLayerFile(t, result.OutputFiles, dstLayer)
	// Both resources should have import blocks
	if strings.Count(dstContent, "import {") != 2 {
		t.Errorf("expected 2 import blocks, got:\n%s", dstContent)
	}
	// key_vault should use the composite import ID from the template
	if !strings.Contains(dstContent, `id = "proj-123/endpoint-id"`) {
		t.Errorf("expected composite import ID 'proj-123/endpoint-id', got:\n%s", dstContent)
	}
	// web should use the auto-resolved import ID
	if !strings.Contains(dstContent, `id = "i-web123"`) {
		t.Errorf("expected auto-resolved import ID 'i-web123', got:\n%s", dstContent)
	}
}


func TestEngine_ProcessFiles_AllResourcesWithImportIDAndDestinationOverride(t *testing.T) {
	// Move all resources, override both destination_address and import_id for one.
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
description: "Move all, rename and override import_id"
operations:
  - type: move
    source_layer: "` + srcLayer + `"
    destination_layer: "` + dstLayer + `"
    all_resources: true
    overrides:
      - from: "azuredevops_serviceendpoint_azurerm.key_vault"
        to: "azuredevops_serviceendpoint_azurerm.kv"
        import_id: "{{ .Attributes.project_id }}/{{ .Attributes.id }}"
`
	migrationFile := filepath.Join(dir, "001_rename_import.yaml")
	if err := os.WriteFile(migrationFile, []byte(migrationContent), 0o644); err != nil {
		t.Fatal(err)
	}

	mock := testutil.NewMockStateReader(map[string]*tfjson.State{
		srcLayer: testutil.BuildState(
			testutil.NewResource("aws_instance.web", "aws_instance", "web", nil,
				map[string]interface{}{"id": "i-web123"}),
			testutil.NewResource("azuredevops_serviceendpoint_azurerm.key_vault", "azuredevops_serviceendpoint_azurerm", "key_vault", nil,
				map[string]interface{}{"id": "endpoint-id", "project_id": "proj-123"}),
		),
	})

	engine := New(Config{StateReader: mock})

	result, err := engine.ProcessFiles(context.Background(), []string{migrationFile})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	dstContent := readLayerFile(t, result.OutputFiles, dstLayer)
	// key_vault should be renamed to kv and use composite import ID
	if !strings.Contains(dstContent, "azuredevops_serviceendpoint_azurerm.kv") {
		t.Errorf("expected renamed address 'azuredevops_serviceendpoint_azurerm.kv', got:\n%s", dstContent)
	}
	if !strings.Contains(dstContent, `id = "proj-123/endpoint-id"`) {
		t.Errorf("expected composite import ID, got:\n%s", dstContent)
	}
	// Original key_vault address should NOT appear in destination
	if strings.Contains(dstContent, "azuredevops_serviceendpoint_azurerm.key_vault") {
		t.Error("key_vault should have been renamed to kv in destination")
	}
}



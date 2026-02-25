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

// findLayerFile returns the first output file path whose directory matches the
// given layer. Returns empty string if not found.
func findLayerFile(files []string, layer string) string {
	for _, f := range files {
		if strings.HasPrefix(f, layer+string(filepath.Separator)) || strings.HasPrefix(f, layer+"/") {
			return f
		}
	}
	return ""
}

// readLayerFile reads the content of the output file in the given layer.
func readLayerFile(t *testing.T, files []string, layer string) string {
	t.Helper()
	f := findLayerFile(files, layer)
	if f == "" {
		t.Fatalf("no output file found for layer %q in %v", layer, files)
	}
	content, err := os.ReadFile(f)
	if err != nil {
		t.Fatalf("reading output file %q: %v", f, err)
	}
	return string(content)
}

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
      - address: "aws_instance.web"
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

	files, err := engine.ProcessFiles(context.Background(), []string{migrationFile})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d: %v", len(files), files)
	}

	// Find source and destination files from returned paths
	var srcFile, dstFile string
	for _, f := range files {
		if strings.HasPrefix(f, srcLayer) {
			srcFile = f
		} else if strings.HasPrefix(f, dstLayer) {
			dstFile = f
		}
	}
	if srcFile == "" || dstFile == "" {
		t.Fatalf("expected one file per layer, got: %v", files)
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

func TestEngine_ProcessFiles_Rename(t *testing.T) {
	dir := t.TempDir()
	layerDir := filepath.Join(dir, "layers", "net")
	if err := os.MkdirAll(layerDir, 0o755); err != nil {
		t.Fatal(err)
	}

	migrationContent := `
description: "Rename VPC module"
operations:
  - type: rename
    description: "Rename old to new"
    layer: "` + layerDir + `"
    renames:
      - from: "module.old_vpc"
        to: "module.new_vpc"
`
	migrationFile := filepath.Join(dir, "001_rename.yaml")
	if err := os.WriteFile(migrationFile, []byte(migrationContent), 0o644); err != nil {
		t.Fatal(err)
	}

	engine := New(Config{StateReader: testutil.NewMockStateReader(nil)})

	files, err := engine.ProcessFiles(context.Background(), []string{migrationFile})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}

	content, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	if !strings.Contains(string(content), "moved {") {
		t.Error("expected moved block")
	}
	if !strings.Contains(string(content), "module.old_vpc") {
		t.Error("expected from address")
	}
	if !strings.Contains(string(content), "module.new_vpc") {
		t.Error("expected to address")
	}
}

func TestEngine_ProcessFiles_Remove(t *testing.T) {
	dir := t.TempDir()
	layerDir := filepath.Join(dir, "layers", "legacy")
	if err := os.MkdirAll(layerDir, 0o755); err != nil {
		t.Fatal(err)
	}

	migrationContent := `
description: "Remove deprecated resource"
operations:
  - type: remove
    layer: "` + layerDir + `"
    addresses:
      - "aws_iam_role.deprecated"
`
	migrationFile := filepath.Join(dir, "001_remove.yaml")
	if err := os.WriteFile(migrationFile, []byte(migrationContent), 0o644); err != nil {
		t.Fatal(err)
	}

	engine := New(Config{StateReader: testutil.NewMockStateReader(nil)})

	files, err := engine.ProcessFiles(context.Background(), []string{migrationFile})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	if !strings.Contains(string(content), "removed {") {
		t.Error("expected removed block")
	}
	if !strings.Contains(string(content), "destroy = false") {
		t.Error("expected destroy = false")
	}
}

func TestEngine_ProcessFiles_Import(t *testing.T) {
	dir := t.TempDir()
	layerDir := filepath.Join(dir, "layers", "db")
	if err := os.MkdirAll(layerDir, 0o755); err != nil {
		t.Fatal(err)
	}

	migrationContent := `
description: "Import RDS instance"
operations:
  - type: import
    layer: "` + layerDir + `"
    imports:
      - address: "aws_db_instance.primary"
        import_id: "my-database"
        provider: "aws.useast1"
`
	migrationFile := filepath.Join(dir, "001_import.yaml")
	if err := os.WriteFile(migrationFile, []byte(migrationContent), 0o644); err != nil {
		t.Fatal(err)
	}

	engine := New(Config{StateReader: testutil.NewMockStateReader(nil)})

	files, err := engine.ProcessFiles(context.Background(), []string{migrationFile})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	if !strings.Contains(string(content), "import {") {
		t.Error("expected import block")
	}
	if !strings.Contains(string(content), `"my-database"`) {
		t.Error("expected import ID")
	}
	if !strings.Contains(string(content), "provider = aws.useast1") {
		t.Error("expected provider")
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
      - address: "aws_s3_bucket.data"
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

	files, err := engine.ProcessFiles(context.Background(), []string{migrationFile})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d: %v", len(files), files)
	}

	srcContent := readLayerFile(t, files, srcLayer)
	if strings.Count(srcContent, "removed {") != 1 {
		t.Errorf("expected 1 removed block, got:\n%s", srcContent)
	}

	dstContent := readLayerFile(t, files, dstLayer)
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

func TestEngine_ProcessFiles_KeyedMove(t *testing.T) {
	dir := t.TempDir()
	srcLayer := filepath.Join(dir, "layers", "old")
	dstLayer := filepath.Join(dir, "layers", "new")
	if err := os.MkdirAll(srcLayer, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dstLayer, 0o755); err != nil {
		t.Fatal(err)
	}

	// Keyed move: rename exact keys + prefix pattern
	migrationContent := `
description: "Keyed move"
operations:
  - type: move
    source_layer: "` + srcLayer + `"
    destination_layer: "` + dstLayer + `"
    resources:
      - address: "aws_resource.items"
        keys:
          exact_key: new_exact
          "prefix_*": '{{ .Key | trimPrefix "prefix_" }}'
`
	migrationFile := filepath.Join(dir, "001_keyed.yaml")
	if err := os.WriteFile(migrationFile, []byte(migrationContent), 0o644); err != nil {
		t.Fatal(err)
	}

	mock := testutil.NewMockStateReader(map[string]*tfjson.State{
		srcLayer: testutil.BuildState(
			testutil.NewResource(
				`aws_resource.items["exact_key"]`, "aws_resource", "items", "exact_key",
				map[string]interface{}{"id": "id-exact"},
			),
			testutil.NewResource(
				`aws_resource.items["prefix_alpha"]`, "aws_resource", "items", "prefix_alpha",
				map[string]interface{}{"id": "id-alpha"},
			),
			testutil.NewResource(
				`aws_resource.items["prefix_beta"]`, "aws_resource", "items", "prefix_beta",
				map[string]interface{}{"id": "id-beta"},
			),
		),
	})

	engine := New(Config{StateReader: mock})

	files, err := engine.ProcessFiles(context.Background(), []string{migrationFile})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d: %v", len(files), files)
	}

	srcContent := readLayerFile(t, files, srcLayer)
	if strings.Count(srcContent, "removed {") != 1 {
		t.Errorf("expected 1 removed block, got:\n%s", srcContent)
	}

	dstContent := readLayerFile(t, files, dstLayer)
	if strings.Count(dstContent, "import {") != 3 {
		t.Errorf("expected 3 import blocks, got:\n%s", dstContent)
	}
	// exact_key → new_exact
	if !strings.Contains(dstContent, `aws_resource.items["new_exact"]`) {
		t.Error("expected exact key renamed to new_exact")
	}
	// prefix_alpha → alpha
	if !strings.Contains(dstContent, `aws_resource.items["alpha"]`) {
		t.Error("expected prefix_alpha trimmed to alpha")
	}
	// prefix_beta → beta
	if !strings.Contains(dstContent, `aws_resource.items["beta"]`) {
		t.Error("expected prefix_beta trimmed to beta")
	}
}

func TestEngine_ProcessFiles_KeyedMoveWithAddressPrefix(t *testing.T) {
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
description: "Keyed move with address prefix"
operations:
  - type: move
    source_layer: "` + srcLayer + `"
    destination_layer: "` + dstLayer + `"
    address_prefix: "module.ig"
    resources:
      - address: "azuread_access_package_catalog.all"
        keys:
          mrt_customer: customer_approval
          mrt_vaw: vaw
`
	migrationFile := filepath.Join(dir, "001_prefix.yaml")
	if err := os.WriteFile(migrationFile, []byte(migrationContent), 0o644); err != nil {
		t.Fatal(err)
	}

	mock := testutil.NewMockStateReader(map[string]*tfjson.State{
		srcLayer: testutil.BuildState(
			testutil.NewResource(
				`module.ig.azuread_access_package_catalog.all["mrt_customer"]`,
				"azuread_access_package_catalog", "all", "mrt_customer",
				map[string]interface{}{"id": "id-customer"},
			),
			testutil.NewResource(
				`module.ig.azuread_access_package_catalog.all["mrt_vaw"]`,
				"azuread_access_package_catalog", "all", "mrt_vaw",
				map[string]interface{}{"id": "id-vaw"},
			),
		),
	})

	engine := New(Config{StateReader: mock})

	files, err := engine.ProcessFiles(context.Background(), []string{migrationFile})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d: %v", len(files), files)
	}

	dstContent := readLayerFile(t, files, dstLayer)
	// Address prefix should be applied: module.ig.azuread_access_package_catalog.all["customer_approval"]
	if !strings.Contains(dstContent, `module.ig.azuread_access_package_catalog.all["customer_approval"]`) {
		t.Errorf("expected address prefix applied to destination, got:\n%s", dstContent)
	}
	if !strings.Contains(dstContent, `module.ig.azuread_access_package_catalog.all["vaw"]`) {
		t.Errorf("expected address prefix applied to vaw destination, got:\n%s", dstContent)
	}
}

func TestEngine_ProcessFiles_KeyedMoveSplitAcrossOps(t *testing.T) {
	dir := t.TempDir()
	srcLayer := filepath.Join(dir, "layers", "old")
	engLayer := filepath.Join(dir, "layers", "engineering")
	finLayer := filepath.Join(dir, "layers", "finance")
	for _, d := range []string{srcLayer, engLayer, finLayer} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	// Same resource split across two operations with different destination layers
	migrationContent := `
description: "Split by department"
operations:
  - type: move
    source_layer: "` + srcLayer + `"
    destination_layer: "` + engLayer + `"
    resources:
      - address: "aws_resource.items"
        keys:
          "eng_*": '{{ .Key }}'
  - type: move
    source_layer: "` + srcLayer + `"
    destination_layer: "` + finLayer + `"
    resources:
      - address: "aws_resource.items"
        keys:
          "fin_*": '{{ .Key }}'
`
	migrationFile := filepath.Join(dir, "001_split.yaml")
	if err := os.WriteFile(migrationFile, []byte(migrationContent), 0o644); err != nil {
		t.Fatal(err)
	}

	mock := testutil.NewMockStateReader(map[string]*tfjson.State{
		srcLayer: testutil.BuildState(
			testutil.NewResource(
				`aws_resource.items["eng_admin"]`, "aws_resource", "items", "eng_admin",
				map[string]interface{}{"id": "id-eng-admin"},
			),
			testutil.NewResource(
				`aws_resource.items["eng_reader"]`, "aws_resource", "items", "eng_reader",
				map[string]interface{}{"id": "id-eng-reader"},
			),
			testutil.NewResource(
				`aws_resource.items["fin_admin"]`, "aws_resource", "items", "fin_admin",
				map[string]interface{}{"id": "id-fin-admin"},
			),
			testutil.NewResource(
				`aws_resource.items["fin_reader"]`, "aws_resource", "items", "fin_reader",
				map[string]interface{}{"id": "id-fin-reader"},
			),
		),
	})

	engine := New(Config{StateReader: mock})

	files, err := engine.ProcessFiles(context.Background(), []string{migrationFile})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(files) != 3 {
		t.Fatalf("expected 3 files (source + 2 destinations), got %d: %v", len(files), files)
	}

	srcContent := readLayerFile(t, files, srcLayer)
	if strings.Count(srcContent, "removed {") != 1 {
		t.Errorf("expected exactly 1 removed block in source, got:\n%s", srcContent)
	}

	engContent := readLayerFile(t, files, engLayer)
	if strings.Count(engContent, "import {") != 2 {
		t.Errorf("expected 2 import blocks in engineering, got:\n%s", engContent)
	}

	finContent := readLayerFile(t, files, finLayer)
	if strings.Count(finContent, "import {") != 2 {
		t.Errorf("expected 2 import blocks in finance, got:\n%s", finContent)
	}
}

func TestEngine_ProcessFiles_IncompleteCoverage(t *testing.T) {
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
description: "Incomplete coverage"
operations:
  - type: move
    source_layer: "` + srcLayer + `"
    destination_layer: "` + dstLayer + `"
    resources:
      - address: "aws_resource.items"
        keys:
          "eng_*": '{{ .Key }}'
`
	migrationFile := filepath.Join(dir, "001_incomplete.yaml")
	if err := os.WriteFile(migrationFile, []byte(migrationContent), 0o644); err != nil {
		t.Fatal(err)
	}

	mock := testutil.NewMockStateReader(map[string]*tfjson.State{
		srcLayer: testutil.BuildState(
			testutil.NewResource(
				`aws_resource.items["eng_admin"]`, "aws_resource", "items", "eng_admin",
				map[string]interface{}{"id": "id-eng-admin"},
			),
			testutil.NewResource(
				`aws_resource.items["other_admin"]`, "aws_resource", "items", "other_admin",
				map[string]interface{}{"id": "id-other-admin"},
			),
		),
	})

	engine := New(Config{StateReader: mock})

	// Incomplete coverage causes the file to be skipped (not a fatal error).
	// Since it's the only file, ProcessFiles returns "all migration files were skipped".
	_, err := engine.ProcessFiles(context.Background(), []string{migrationFile})
	if err == nil {
		t.Fatal("expected error when all files are skipped")
	}
	if !strings.Contains(err.Error(), "skipped") {
		t.Errorf("expected error to mention files were skipped, got: %v", err)
	}
}

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

	files, err := engine.ProcessFiles(context.Background(), []string{migrationFile})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file path, got %d", len(files))
	}

	// Verify file was NOT written in dry run mode
	if _, statErr := os.Stat(files[0]); !os.IsNotExist(statErr) {
		t.Error("expected file to not exist in dry-run mode")
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
    addresses:
      - "aws_security_group.legacy"
  - type: import
    layer: "` + layerDir + `"
    imports:
      - address: "aws_route_table.new"
        import_id: "rtb-0abc123"
`
	migrationFile := filepath.Join(dir, "001_multi.yaml")
	if err := os.WriteFile(migrationFile, []byte(migrationContent), 0o644); err != nil {
		t.Fatal(err)
	}

	engine := New(Config{StateReader: testutil.NewMockStateReader(nil)})

	files, err := engine.ProcessFiles(context.Background(), []string{migrationFile})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}

	content, err := os.ReadFile(files[0])
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

func TestEngine_ProcessFiles_MultipleRenames(t *testing.T) {
	dir := t.TempDir()
	layerDir := filepath.Join(dir, "layers", "net")
	if err := os.MkdirAll(layerDir, 0o755); err != nil {
		t.Fatal(err)
	}

	migrationContent := `
description: "Multiple renames in one operation"
operations:
  - type: rename
    layer: "` + layerDir + `"
    renames:
      - from: "module.old_vpc"
        to: "module.new_vpc"
      - from: "aws_route_table.old"
        to: "aws_route_table.new"
`
	migrationFile := filepath.Join(dir, "001_multi_renames.yaml")
	if err := os.WriteFile(migrationFile, []byte(migrationContent), 0o644); err != nil {
		t.Fatal(err)
	}

	engine := New(Config{StateReader: testutil.NewMockStateReader(nil)})

	files, err := engine.ProcessFiles(context.Background(), []string{migrationFile})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	if strings.Count(string(content), "moved {") != 2 {
		t.Errorf("expected 2 moved blocks, got:\n%s", content)
	}
}

func TestEngine_ProcessFiles_MultipleRemoveAddresses(t *testing.T) {
	dir := t.TempDir()
	layerDir := filepath.Join(dir, "layers", "iam")
	if err := os.MkdirAll(layerDir, 0o755); err != nil {
		t.Fatal(err)
	}

	migrationContent := `
description: "Multiple removals"
operations:
  - type: remove
    layer: "` + layerDir + `"
    addresses:
      - "aws_iam_role.deprecated"
      - "aws_iam_policy.old"
`
	migrationFile := filepath.Join(dir, "001_multi_removes.yaml")
	if err := os.WriteFile(migrationFile, []byte(migrationContent), 0o644); err != nil {
		t.Fatal(err)
	}

	engine := New(Config{StateReader: testutil.NewMockStateReader(nil)})

	files, err := engine.ProcessFiles(context.Background(), []string{migrationFile})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	if strings.Count(string(content), "removed {") != 2 {
		t.Errorf("expected 2 removed blocks, got:\n%s", content)
	}
}

func TestEngine_ProcessFiles_RenameWithAddressPrefix(t *testing.T) {
	dir := t.TempDir()
	layerDir := filepath.Join(dir, "layers", "net")
	if err := os.MkdirAll(layerDir, 0o755); err != nil {
		t.Fatal(err)
	}

	migrationContent := `
description: "Rename with address prefix"
operations:
  - type: rename
    layer: "` + layerDir + `"
    address_prefix: "module.vpc"
    renames:
      - from: "aws_subnet.old"
        to: "aws_subnet.new"
`
	migrationFile := filepath.Join(dir, "001_prefix_rename.yaml")
	if err := os.WriteFile(migrationFile, []byte(migrationContent), 0o644); err != nil {
		t.Fatal(err)
	}

	engine := New(Config{StateReader: testutil.NewMockStateReader(nil)})

	files, err := engine.ProcessFiles(context.Background(), []string{migrationFile})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	if !strings.Contains(string(content), "module.vpc.aws_subnet.old") {
		t.Errorf("expected address prefix applied to from, got:\n%s", content)
	}
	if !strings.Contains(string(content), "module.vpc.aws_subnet.new") {
		t.Errorf("expected address prefix applied to to, got:\n%s", content)
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
      - address: "module.old.resource.all"
        destination_address: "module.new.resource.all"
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

	files, err := engine.ProcessFiles(context.Background(), []string{migrationFile})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	srcContent := readLayerFile(t, files, srcLayer)
	// With module consolidation, the entire module.old is removed since
	// all managed resources within it are being moved.
	if !strings.Contains(srcContent, "module.old") {
		t.Error("expected module.old in removed block (consolidated)")
	}

	dstContent := readLayerFile(t, files, dstLayer)
	if !strings.Contains(dstContent, `module.new.resource.all["key1"]`) {
		t.Errorf("expected destination_address override in import, got:\n%s", dstContent)
	}
}

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
      - address: "aws_instance.web"
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

	files, err := engine.ProcessFiles(context.Background(), []string{migrationFile})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(files) != 2 {
		t.Fatalf("expected 2 files (condition met, migration proceeds), got %d", len(files))
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
      - address: "aws_instance.web"
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

	files, err := engine.ProcessFiles(context.Background(), []string{migrationFile})
	if err != nil {
		t.Fatalf("unexpected error (should silently skip): %v", err)
	}

	if len(files) != 0 {
		t.Errorf("expected 0 files (condition not met, migration skipped), got %d", len(files))
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
      - address: "aws_instance.web"
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

	files, err := engine.ProcessFiles(context.Background(), []string{migrationFile})
	if err != nil {
		t.Fatalf("unexpected error (should silently skip): %v", err)
	}

	if len(files) != 0 {
		t.Errorf("expected 0 files (resource already exists in destination), got %d", len(files))
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
      - address: "aws_instance.web"
        import_id: "i-0abc123"
`
	migrationFile := filepath.Join(dir, "001_state_err.yaml")
	if err := os.WriteFile(migrationFile, []byte(migrationContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// MockStateReader returns error for unknown layers
	mock := testutil.NewMockStateReader(nil)
	engine := New(Config{StateReader: mock})

	// State read errors during condition evaluation cause the file to be skipped.
	// Since it's the only file, ProcessFiles returns "all migration files were skipped".
	_, err := engine.ProcessFiles(context.Background(), []string{migrationFile})
	if err == nil {
		t.Fatal("expected error when all files are skipped due to state read failure")
	}
	if !strings.Contains(err.Error(), "skipped") {
		t.Errorf("expected error to mention files were skipped, got: %v", err)
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

	files, err := engine.ProcessFiles(context.Background(), []string{migrationFile})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file (no condition, proceeds normally), got %d", len(files))
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
      - address: "aws_instance.gone"
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

	files, err := engine.ProcessFiles(context.Background(), []string{moveFile, renameFile})
	if err != nil {
		t.Fatalf("expected partial success (first skipped, second succeeds), got error: %v", err)
	}

	if len(files) != 1 {
		t.Fatalf("expected 1 output file from the rename, got %d: %v", len(files), files)
	}

	content, err := os.ReadFile(files[0])
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
      - address: "aws_instance.missing` + fmt.Sprintf("%d", i) + `"
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
      - address: "aws_instance.web"
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

	files, err := engine.ProcessFiles(context.Background(), []string{condFile, renameFile})
	if err != nil {
		t.Fatalf("expected partial success, got error: %v", err)
	}

	if len(files) != 1 {
		t.Fatalf("expected 1 output file from the rename, got %d: %v", len(files), files)
	}

	content, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	if !strings.Contains(string(content), "moved {") {
		t.Error("expected moved block from the rename operation")
	}
}

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
      - address: "module.foo"
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

	files, err := engine.ProcessFiles(context.Background(), []string{migrationFile})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(files) != 2 {
		t.Fatalf("expected 2 files (source + destination), got %d: %v", len(files), files)
	}

	// Source layer: consolidated removed block for module.foo
	srcContent := readLayerFile(t, files, srcLayer)
	if !strings.Contains(srcContent, "module.foo") {
		t.Error("expected module.foo in removed block (consolidated)")
	}

	// Destination layer: 2 import blocks
	dstContent := readLayerFile(t, files, dstLayer)
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
      - address: "module.foo"
        destination_address: "module.bar"
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

	files, err := engine.ProcessFiles(context.Background(), []string{migrationFile})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	dstContent := readLayerFile(t, files, dstLayer)
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
      - address: "module.foo"
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

	files, err := engine.ProcessFiles(context.Background(), []string{migrationFile})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	dstContent := readLayerFile(t, files, dstLayer)
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
      - address: "module.foo"
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

	files, err := engine.ProcessFiles(context.Background(), []string{migrationFile})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	dstContent := readLayerFile(t, files, dstLayer)
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
	srcContent := readLayerFile(t, files, srcLayer)
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
      - address: "module.foo"
        destination_address: "module.bar"
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

	files, err := engine.ProcessFiles(context.Background(), []string{migrationFile})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	dstContent := readLayerFile(t, files, dstLayer)
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
      - address: "module.empty"
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

	files, err := engine.ProcessFiles(context.Background(), []string{migrationFile})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(files) != 2 {
		t.Fatalf("expected 2 files (source + destination), got %d: %v", len(files), files)
	}

	dstContent := readLayerFile(t, files, dstLayer)
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
    resources:
      - address: "aws_instance.web"
        destination_address: "aws_instance.api"
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

	files, err := engine.ProcessFiles(context.Background(), []string{migrationFile})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	dstContent := readLayerFile(t, files, dstLayer)
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

	files, err := engine.ProcessFiles(context.Background(), []string{migrationFile})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	dstContent := readLayerFile(t, files, dstLayer)
	if strings.Count(dstContent, "import {") != 2 {
		t.Errorf("expected 2 import blocks for for_each instances, got:\n%s", dstContent)
	}
	if !strings.Contains(dstContent, `aws_s3_bucket.data["key-a"]`) {
		t.Error("expected key-a import")
	}
	if !strings.Contains(dstContent, `aws_s3_bucket.data["key-b"]`) {
		t.Error("expected key-b import")
	}

	srcContent := readLayerFile(t, files, srcLayer)
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

	files, err := engine.ProcessFiles(context.Background(), []string{migrationFile})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	srcContent := readLayerFile(t, files, srcLayer)
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

	files, err := engine.ProcessFiles(context.Background(), []string{migrationFile})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(files) != 2 {
		t.Fatalf("expected 2 files (source + destination), got %d: %v", len(files), files)
	}

	dstContent := readLayerFile(t, files, dstLayer)
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

	srcContent := readLayerFile(t, files, srcLayer)
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

	files, err := engine.ProcessFiles(context.Background(), []string{migrationFile})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	srcContent := readLayerFile(t, files, srcLayer)
	// Should have removed block for ephemeral with destroy = true
	if !strings.Contains(srcContent, "destroy = true") {
		t.Errorf("expected 'destroy = true' for omitted resource, got:\n%s", srcContent)
	}

	dstContent := readLayerFile(t, files, dstLayer)
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
    resources:
      - address: "aws_instance.web"
        destination_address: "aws_instance.api"
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

	files, err := engine.ProcessFiles(context.Background(), []string{migrationFile})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	dstContent := readLayerFile(t, files, dstLayer)
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
    resources:
      - address: "azuredevops_serviceendpoint_azurerm.key_vault"
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

	files, err := engine.ProcessFiles(context.Background(), []string{migrationFile})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	dstContent := readLayerFile(t, files, dstLayer)
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
    resources:
      - address: "azuredevops_serviceendpoint_azurerm.key_vault"
        destination_address: "azuredevops_serviceendpoint_azurerm.kv"
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

	files, err := engine.ProcessFiles(context.Background(), []string{migrationFile})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	dstContent := readLayerFile(t, files, dstLayer)
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

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

	srcContent, err := os.ReadFile(filepath.Join(srcLayer, "migrations.tf"))
	if err != nil {
		t.Fatalf("reading source migration file: %v", err)
	}
	if !strings.Contains(string(srcContent), "removed {") {
		t.Error("expected removed block in source layer")
	}
	if !strings.Contains(string(srcContent), "aws_instance.web") {
		t.Error("expected resource address in source layer")
	}

	dstContent, err := os.ReadFile(filepath.Join(dstLayer, "migrations.tf"))
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

	srcContent, err := os.ReadFile(filepath.Join(srcLayer, "migrations.tf"))
	if err != nil {
		t.Fatalf("reading source: %v", err)
	}
	if strings.Count(string(srcContent), "removed {") != 1 {
		t.Errorf("expected 1 removed block, got:\n%s", srcContent)
	}

	dstContent, err := os.ReadFile(filepath.Join(dstLayer, "migrations.tf"))
	if err != nil {
		t.Fatalf("reading destination: %v", err)
	}
	if strings.Count(string(dstContent), "import {") != 2 {
		t.Errorf("expected 2 import blocks, got:\n%s", dstContent)
	}
	if !strings.Contains(string(dstContent), `aws_s3_bucket.data["key-a"]`) {
		t.Error("expected key-a in destination")
	}
	if !strings.Contains(string(dstContent), `aws_s3_bucket.data["key-b"]`) {
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

	srcContent, err := os.ReadFile(filepath.Join(srcLayer, "migrations.tf"))
	if err != nil {
		t.Fatalf("reading source: %v", err)
	}
	if strings.Count(string(srcContent), "removed {") != 1 {
		t.Errorf("expected 1 removed block, got:\n%s", srcContent)
	}

	dstContent, err := os.ReadFile(filepath.Join(dstLayer, "migrations.tf"))
	if err != nil {
		t.Fatalf("reading destination: %v", err)
	}
	if strings.Count(string(dstContent), "import {") != 3 {
		t.Errorf("expected 3 import blocks, got:\n%s", dstContent)
	}
	// exact_key → new_exact
	if !strings.Contains(string(dstContent), `aws_resource.items["new_exact"]`) {
		t.Error("expected exact key renamed to new_exact")
	}
	// prefix_alpha → alpha
	if !strings.Contains(string(dstContent), `aws_resource.items["alpha"]`) {
		t.Error("expected prefix_alpha trimmed to alpha")
	}
	// prefix_beta → beta
	if !strings.Contains(string(dstContent), `aws_resource.items["beta"]`) {
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

	dstContent, err := os.ReadFile(filepath.Join(dstLayer, "migrations.tf"))
	if err != nil {
		t.Fatalf("reading destination: %v", err)
	}
	// Address prefix should be applied: module.ig.azuread_access_package_catalog.all["customer_approval"]
	if !strings.Contains(string(dstContent), `module.ig.azuread_access_package_catalog.all["customer_approval"]`) {
		t.Errorf("expected address prefix applied to destination, got:\n%s", dstContent)
	}
	if !strings.Contains(string(dstContent), `module.ig.azuread_access_package_catalog.all["vaw"]`) {
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

	srcContent, err := os.ReadFile(filepath.Join(srcLayer, "migrations.tf"))
	if err != nil {
		t.Fatalf("reading source: %v", err)
	}
	if strings.Count(string(srcContent), "removed {") != 1 {
		t.Errorf("expected exactly 1 removed block in source, got:\n%s", srcContent)
	}

	engContent, err := os.ReadFile(filepath.Join(engLayer, "migrations.tf"))
	if err != nil {
		t.Fatalf("reading engineering layer: %v", err)
	}
	if strings.Count(string(engContent), "import {") != 2 {
		t.Errorf("expected 2 import blocks in engineering, got:\n%s", engContent)
	}

	finContent, err := os.ReadFile(filepath.Join(finLayer, "migrations.tf"))
	if err != nil {
		t.Fatalf("reading finance layer: %v", err)
	}
	if strings.Count(string(finContent), "import {") != 2 {
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

	_, err := engine.ProcessFiles(context.Background(), []string{migrationFile})
	if err == nil {
		t.Fatal("expected completeness error for uncovered key")
	}
	if !strings.Contains(err.Error(), "other_admin") {
		t.Errorf("expected error to mention uncovered key 'other_admin', got: %v", err)
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

	_, err := engine.ProcessFiles(context.Background(), []string{migrationFile})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	srcContent, err := os.ReadFile(filepath.Join(srcLayer, "migrations.tf"))
	if err != nil {
		t.Fatalf("reading source: %v", err)
	}
	if !strings.Contains(string(srcContent), "module.old.resource.all") {
		t.Error("expected source address in removed block")
	}

	dstContent, err := os.ReadFile(filepath.Join(dstLayer, "migrations.tf"))
	if err != nil {
		t.Fatalf("reading destination: %v", err)
	}
	if !strings.Contains(string(dstContent), `module.new.resource.all["key1"]`) {
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

	_, err := engine.ProcessFiles(context.Background(), []string{migrationFile})
	if err == nil {
		t.Fatal("expected error when state read fails during condition check")
	}
	if !strings.Contains(err.Error(), "condition") {
		t.Errorf("expected error to mention condition, got: %v", err)
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

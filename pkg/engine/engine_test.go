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

func TestEngine_ProcessFiles_SimpleMoveWithExplicitID(t *testing.T) {
	dir := t.TempDir()
	srcLayer := filepath.Join(dir, "layers", "compute")
	dstLayer := filepath.Join(dir, "layers", "app")
	if err := os.MkdirAll(srcLayer, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dstLayer, 0o755); err != nil {
		t.Fatal(err)
	}

	// Write migration file
	migrationContent := `
description: "Move web instance"
operations:
  - type: move
    description: "Move to app layer"
    source:
      layer: "` + srcLayer + `"
      address: "aws_instance.web"
    destination:
      layer: "` + dstLayer + `"
      address: "aws_instance.web"
    import_id: "i-0abc123"
`
	migrationFile := filepath.Join(dir, "001_move.yaml")
	if err := os.WriteFile(migrationFile, []byte(migrationContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// No state reader needed for explicit import_id
	mock := testutil.NewMockStateReader(map[string]*tfjson.State{
		srcLayer: testutil.BuildState(
			testutil.NewResource("aws_instance.web", "aws_instance", "web", nil, map[string]interface{}{
				"id": "i-0abc123",
			}),
		),
	})

	engine := New(Config{
		StateReader: mock,
	})

	files, err := engine.ProcessFiles(context.Background(), []string{migrationFile})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d: %v", len(files), files)
	}

	// Check source layer file
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

	// Check destination layer file
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
    from: "module.old_vpc"
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
    address: "aws_iam_role.deprecated"
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
    address: "aws_db_instance.primary"
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

func TestEngine_ProcessFiles_WildcardMove(t *testing.T) {
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
description: "Wildcard move with key mutation"
operations:
  - type: move
    source:
      layer: "` + srcLayer + `"
      address: "aws_s3_bucket.data[*]"
    destination:
      layer: "` + dstLayer + `"
      address: 'aws_s3_bucket.data["{{ .Attributes.bucket }}"]'
    import_id: "{{ .Attributes.id }}"
`
	migrationFile := filepath.Join(dir, "001_wildcard.yaml")
	if err := os.WriteFile(migrationFile, []byte(migrationContent), 0o644); err != nil {
		t.Fatal(err)
	}

	mock := testutil.NewMockStateReader(map[string]*tfjson.State{
		srcLayer: testutil.BuildState(
			testutil.NewResource(
				`aws_s3_bucket.data["key-a"]`, "aws_s3_bucket", "data", "key-a",
				map[string]interface{}{"id": "bucket-a-id", "bucket": "bucket-a"},
			),
			testutil.NewResource(
				`aws_s3_bucket.data["key-b"]`, "aws_s3_bucket", "data", "key-b",
				map[string]interface{}{"id": "bucket-b-id", "bucket": "bucket-b"},
			),
		),
	})

	engine := New(Config{StateReader: mock})

	files, err := engine.ProcessFiles(context.Background(), []string{migrationFile})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(files))
	}

	srcContent, err := os.ReadFile(filepath.Join(srcLayer, "migrations.tf"))
	if err != nil {
		t.Fatalf("reading source: %v", err)
	}
	if strings.Count(string(srcContent), "removed {") != 2 {
		t.Errorf("expected 2 removed blocks in source, got:\n%s", srcContent)
	}

	dstContent, err := os.ReadFile(filepath.Join(dstLayer, "migrations.tf"))
	if err != nil {
		t.Fatalf("reading destination: %v", err)
	}
	if strings.Count(string(dstContent), "import {") != 2 {
		t.Errorf("expected 2 import blocks in destination, got:\n%s", dstContent)
	}
	if !strings.Contains(string(dstContent), `aws_s3_bucket.data["bucket-a"]`) {
		t.Error("expected re-keyed address bucket-a in destination")
	}
	if !strings.Contains(string(dstContent), `aws_s3_bucket.data["bucket-b"]`) {
		t.Error("expected re-keyed address bucket-b in destination")
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
    from: "module.old"
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
    from: "module.old_vpc"
    to: "module.new_vpc"
  - type: remove
    layer: "` + layerDir + `"
    address: "aws_security_group.legacy"
  - type: import
    layer: "` + layerDir + `"
    address: "aws_route_table.new"
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

	// All operations target the same layer, so only 1 file
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

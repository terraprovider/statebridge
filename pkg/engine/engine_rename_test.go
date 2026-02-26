package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/redtenant/tfmigrate/internal/testutil"
)

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

	result, err := engine.ProcessFiles(context.Background(), []string{migrationFile})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content, err := os.ReadFile(result.OutputFiles[0])
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	if strings.Count(string(content), "moved {") != 2 {
		t.Errorf("expected 2 moved blocks, got:\n%s", content)
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

	result, err := engine.ProcessFiles(context.Background(), []string{migrationFile})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content, err := os.ReadFile(result.OutputFiles[0])
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



package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/redtenant/tfmigrate/internal/testutil"
)

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
        id: "my-database"
        provider: "aws.useast1"
`
	migrationFile := filepath.Join(dir, "001_import.yaml")
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


func TestEngine_ProcessFiles_ImportOperationLevelProvider(t *testing.T) {
	// Test that operation-level provider is used as default, and entry-level provider overrides it.
	dir := t.TempDir()
	layerDir := filepath.Join(dir, "layers", "db")
	if err := os.MkdirAll(layerDir, 0o755); err != nil {
		t.Fatal(err)
	}

	migrationContent := `
description: "Import with operation-level and entry-level provider"
operations:
  - type: import
    layer: "` + layerDir + `"
    provider: "aws.useast1"
    imports:
      - address: "aws_db_instance.primary"
        id: "db-primary-id"
      - address: "aws_db_instance.replica"
        id: "db-replica-id"
        provider: "aws.uswest2"
      - address: "aws_db_instance.analytics"
        id: "db-analytics-id"
`
	migrationFile := filepath.Join(dir, "001_import_provider.yaml")
	if err := os.WriteFile(migrationFile, []byte(migrationContent), 0o644); err != nil {
		t.Fatal(err)
	}

	engine := New(Config{StateReader: testutil.NewMockStateReader(nil)})

	result, err := engine.ProcessFiles(context.Background(), []string{migrationFile})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.OutputFiles) != 1 {
		t.Fatalf("expected 1 output file, got %d", len(result.OutputFiles))
	}

	content, err := os.ReadFile(result.OutputFiles[0])
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	s := string(content)

	// Count import blocks
	if strings.Count(s, "import {") != 3 {
		t.Errorf("expected 3 import blocks, got:\n%s", s)
	}

	// primary: should use operation-level provider aws.useast1
	if !strings.Contains(s, "aws_db_instance.primary") {
		t.Error("expected aws_db_instance.primary")
	}

	// replica: should use entry-level override aws.uswest2
	if !strings.Contains(s, "provider = aws.uswest2") {
		t.Error("expected provider = aws.uswest2 (entry-level override)")
	}

	// analytics: should use operation-level provider aws.useast1
	// Both primary and analytics use aws.useast1, so it should appear at least twice
	if strings.Count(s, "provider = aws.useast1") < 2 {
		t.Errorf("expected at least 2 occurrences of 'provider = aws.useast1', got:\n%s", s)
	}
}



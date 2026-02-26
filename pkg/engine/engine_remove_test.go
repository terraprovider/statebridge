package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/redtenant/tfmigrate/internal/testutil"
)

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
    entries:
      - address: "aws_iam_role.deprecated"
`
	migrationFile := filepath.Join(dir, "001_remove.yaml")
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
	if !strings.Contains(string(content), "removed {") {
		t.Error("expected removed block")
	}
	if !strings.Contains(string(content), "destroy = false") {
		t.Error("expected destroy = false")
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
    entries:
      - address: "aws_iam_role.deprecated"
      - address: "aws_iam_policy.old"
`
	migrationFile := filepath.Join(dir, "001_multi_removes.yaml")
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
	if strings.Count(string(content), "removed {") != 2 {
		t.Errorf("expected 2 removed blocks, got:\n%s", content)
	}
}


func TestEngine_ProcessFiles_RemoveDestroyOverrides(t *testing.T) {
	// Test operation-level destroy=true with entry-level override to false.
	dir := t.TempDir()
	layerDir := filepath.Join(dir, "layers", "cleanup")
	if err := os.MkdirAll(layerDir, 0o755); err != nil {
		t.Fatal(err)
	}

	migrationContent := `
description: "Remove with destroy overrides"
operations:
  - type: remove
    layer: "` + layerDir + `"
    destroy: true
    entries:
      - address: "aws_iam_role.deprecated"
      - address: "aws_iam_policy.keep_infra"
        destroy: false
      - address: "aws_iam_policy.also_destroy"
`
	migrationFile := filepath.Join(dir, "001_remove_destroy.yaml")
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

	// Should have 3 removed blocks
	if strings.Count(s, "removed {") != 3 {
		t.Errorf("expected 3 removed blocks, got:\n%s", s)
	}

	// aws_iam_role.deprecated → destroy = true (from operation level)
	// aws_iam_policy.keep_infra → destroy = false (entry-level override)
	// aws_iam_policy.also_destroy → destroy = true (from operation level)
	if strings.Count(s, "destroy = true") != 2 {
		t.Errorf("expected 2 'destroy = true' blocks, got:\n%s", s)
	}
	if strings.Count(s, "destroy = false") != 1 {
		t.Errorf("expected 1 'destroy = false' block, got:\n%s", s)
	}
}



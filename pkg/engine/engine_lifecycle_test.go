package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tfjson "github.com/hashicorp/terraform-json"

	"github.com/redtenant/tfmigrate/internal/testutil"
)

func TestEngine_ProcessFiles_ValidationError(t *testing.T) {
	dir := t.TempDir()
	migrationFile := testutil.WriteMigration(t, dir, "001_bad.yaml", `
description: "Invalid migration"
operations:
  - type: move
`)

	err := runEngineExpectError(t, Config{StateReader: testutil.NewMockStateReader(nil)}, []string{migrationFile})
	if !strings.Contains(err.Error(), "validation") {
		t.Errorf("expected validation error message, got: %v", err)
	}
}

func TestEngine_ProcessFiles_DryRun(t *testing.T) {
	dir, layers := testutil.SetupLayers(t, "net")
	layerDir := layers["net"]

	yaml := strings.ReplaceAll(`
description: "Rename VPC"
operations:
  - type: rename
    layer: "LAYER"
    renames:
      - from: "module.old"
        to: "module.new"
`, "LAYER", layerDir)
	migrationFile := testutil.WriteMigration(t, dir, "001.yaml", yaml)

	result := runEngine(t, Config{
		StateReader: testutil.NewMockStateReader(nil),
		DryRun:      true,
	}, []string{migrationFile})

	testutil.RequireOutputCount(t, result.OutputFiles, 1)

	// Verify file was NOT written in dry run mode
	if _, statErr := os.Stat(result.OutputFiles[0]); !os.IsNotExist(statErr) {
		t.Error("expected file to not exist in dry-run mode")
	}
}

func TestEngine_ProcessFiles_StatusRetired(t *testing.T) {
	dir, layers := testutil.SetupLayers(t, "net")
	layerDir := layers["net"]

	retiredYaml := strings.ReplaceAll(`
description: "Old migration"
status: retired
operations:
  - type: rename
    layer: "LAYER"
    renames:
      - from: "module.old"
        to: "module.new"
`, "LAYER", layerDir)
	retiredFile := testutil.WriteMigration(t, dir, "001_retired.yaml", retiredYaml)

	// Retired-only: should return empty results with no error
	result := runEngine(t, Config{StateReader: testutil.NewMockStateReader(nil)}, []string{retiredFile})
	testutil.RequireOutputCount(t, result.OutputFiles, 0)
	if len(result.SkippedFiles) != 1 {
		t.Fatalf("expected 1 skipped file, got %d", len(result.SkippedFiles))
	}
	if result.SkippedFiles[0].Reason != SkipRetired {
		t.Errorf("expected SkipRetired reason, got %v", result.SkippedFiles[0].Reason)
	}

	// Second file: active rename → should produce output
	activeYaml := strings.ReplaceAll(`
description: "Active rename"
operations:
  - type: rename
    layer: "LAYER"
    renames:
      - from: "module.alpha"
        to: "module.beta"
`, "LAYER", layerDir)
	activeFile := testutil.WriteMigration(t, dir, "002_active.yaml", activeYaml)

	result = runEngine(t, Config{StateReader: testutil.NewMockStateReader(nil)}, []string{retiredFile, activeFile})
	testutil.RequireOutputCount(t, result.OutputFiles, 1)

	content := testutil.ReadFirstOutput(t, result.OutputFiles)
	testutil.AssertContains(t, content, "module.alpha")
	testutil.AssertContains(t, content, "module.beta")
}

func TestEngine_ProcessFiles_LayerAutoSkip(t *testing.T) {
	dir, layers := testutil.SetupLayers(t, "app")
	dstLayer := layers["app"]
	nonExistentLayer := filepath.Join(dir, "layers", "nonexistent")

	yaml := strings.ReplaceAll(`
description: "Move from nonexistent layer"
operations:
  - type: move
    source_layer: "SRC"
    destination_layer: "DST"
    resources:
      - from: "aws_instance.web"
        import_id: "i-0abc123"
`, "SRC", nonExistentLayer)
	yaml = strings.ReplaceAll(yaml, "DST", dstLayer)
	migrationFile := testutil.WriteMigration(t, dir, "001.yaml", yaml)

	result := runEngine(t, Config{StateReader: testutil.NewMockStateReader(nil)}, []string{migrationFile})
	testutil.RequireOutputCount(t, result.OutputFiles, 0)
	if len(result.SkippedFiles) != 1 {
		t.Fatalf("expected 1 skipped file, got %d", len(result.SkippedFiles))
	}
	if result.SkippedFiles[0].Reason != SkipLayerMissing {
		t.Errorf("expected SkipLayerMissing reason, got %v", result.SkippedFiles[0].Reason)
	}
}

func TestEngine_ProcessFiles_AutoSkipByType(t *testing.T) {
	tests := []struct {
		name        string
		yaml        string
		checkStrict bool
	}{
		{
			name: "rename",
			yaml: `
description: "Rename in missing layer"
operations:
  - type: rename
    layer: "LAYER"
    renames:
      - from: "module.old"
        to: "module.new"
`,
			checkStrict: true,
		},
		{
			name: "remove",
			yaml: `
description: "Remove from missing layer"
operations:
  - type: remove
    layer: "LAYER"
    entries:
      - address: "aws_iam_role.old"
`,
		},
		{
			name: "import",
			yaml: `
description: "Import to missing layer"
operations:
  - type: import
    layer: "LAYER"
    imports:
      - address: "aws_db_instance.primary"
        id: "db-123"
`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			nonExistentLayer := filepath.Join(dir, "layers", "gone")

			yaml := strings.ReplaceAll(tc.yaml, "LAYER", nonExistentLayer)
			migrationFile := testutil.WriteMigration(t, dir, "001.yaml", yaml)

			result := runEngine(t, Config{StateReader: testutil.NewMockStateReader(nil)}, []string{migrationFile})
			testutil.RequireOutputCount(t, result.OutputFiles, 0)
			if len(result.SkippedFiles) != 1 {
				t.Fatalf("expected 1 skipped file, got %d", len(result.SkippedFiles))
			}
			if result.SkippedFiles[0].Reason != SkipLayerMissing {
				t.Errorf("expected SkipLayerMissing, got %v", result.SkippedFiles[0].Reason)
			}

			if tc.checkStrict {
				_ = runEngineExpectError(t, Config{
					StateReader: testutil.NewMockStateReader(nil),
					Strict:      true,
				}, []string{migrationFile})
			}
		})
	}
}

func TestEngine_ProcessFiles_AutoSkipMixedFile(t *testing.T) {
	dir, layers := testutil.SetupLayers(t, "net")
	existingLayer := layers["net"]
	nonExistentLayer := filepath.Join(dir, "layers", "gone")

	yaml := strings.ReplaceAll(`
description: "Mixed operations with missing layer"
operations:
  - type: rename
    layer: "LAYER"
    renames:
      - from: "module.old"
        to: "module.new"
  - type: move
    source_layer: "GONE"
    destination_layer: "LAYER"
    resources:
      - from: "aws_instance.web"
        import_id: "i-123"
`, "LAYER", existingLayer)
	yaml = strings.ReplaceAll(yaml, "GONE", nonExistentLayer)
	migrationFile := testutil.WriteMigration(t, dir, "001.yaml", yaml)

	result := runEngine(t, Config{StateReader: testutil.NewMockStateReader(nil)}, []string{migrationFile})
	testutil.RequireOutputCount(t, result.OutputFiles, 0)
	if len(result.SkippedFiles) != 1 {
		t.Fatalf("expected 1 skipped file, got %d", len(result.SkippedFiles))
	}
	if result.SkippedFiles[0].Reason != SkipLayerMissing {
		t.Errorf("expected SkipLayerMissing, got %v", result.SkippedFiles[0].Reason)
	}
}

func TestEngine_ProcessFiles_DryRunContent(t *testing.T) {
	dir, layers := testutil.SetupLayers(t, "compute", "app")
	srcLayer := layers["compute"]
	dstLayer := layers["app"]

	yaml := strings.ReplaceAll(`
description: "Move in dry-run"
operations:
  - type: move
    source_layer: "SRC"
    destination_layer: "DST"
    resources:
      - from: "aws_instance.web"
        import_id: "i-0abc123"
`, "SRC", srcLayer)
	yaml = strings.ReplaceAll(yaml, "DST", dstLayer)
	migrationFile := testutil.WriteMigration(t, dir, "001.yaml", yaml)

	mock := testutil.NewMockStateReader(map[string]*tfjson.State{
		srcLayer: testutil.BuildState(
			testutil.NewResource("aws_instance.web", "aws_instance", "web", nil,
				map[string]interface{}{"id": "i-0abc123"}),
		),
	})

	result := runEngine(t, Config{StateReader: mock, DryRun: true}, []string{migrationFile})
	testutil.RequireOutputCount(t, result.OutputFiles, 2)

	// Verify NO files were actually written
	for _, f := range result.OutputFiles {
		if _, err := os.Stat(f); !os.IsNotExist(err) {
			t.Errorf("expected file %q to NOT exist in dry-run mode", f)
		}
	}

	// Verify output paths are in the expected layer directories
	srcFile := testutil.FindLayerFile(result.OutputFiles, srcLayer)
	dstFile := testutil.FindLayerFile(result.OutputFiles, dstLayer)
	if srcFile == "" {
		t.Error("expected output file in source layer directory")
	}
	if dstFile == "" {
		t.Error("expected output file in destination layer directory")
	}

	if len(result.SkippedFiles) != 0 {
		t.Errorf("expected 0 skipped files, got %d", len(result.SkippedFiles))
	}
}

func TestEngine_ProcessFiles_StrictWithValidLayers(t *testing.T) {
	dir, layers := testutil.SetupLayers(t, "net")
	layerDir := layers["net"]

	yaml := strings.ReplaceAll(`
description: "Rename in strict mode"
operations:
  - type: rename
    layer: "LAYER"
    renames:
      - from: "module.old_vpc"
        to: "module.new_vpc"
`, "LAYER", layerDir)
	migrationFile := testutil.WriteMigration(t, dir, "001.yaml", yaml)

	result := runEngine(t, Config{
		StateReader: testutil.NewMockStateReader(nil),
		Strict:      true,
	}, []string{migrationFile})

	testutil.RequireOutputCount(t, result.OutputFiles, 1)

	content := testutil.ReadFirstOutput(t, result.OutputFiles)
	testutil.AssertContains(t, content, "module.old_vpc")
	testutil.AssertContains(t, content, "module.new_vpc")
}

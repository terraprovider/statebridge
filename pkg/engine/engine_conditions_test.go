package engine

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	tfjson "github.com/hashicorp/terraform-json"

	"github.com/redtenant/tfmigrate/internal/testutil"
)

func TestEngine_ProcessFiles_Conditions(t *testing.T) {
	tests := []struct {
		name         string
		yaml         string
		srcResources []*tfjson.StateResource
		dstResources []*tfjson.StateResource
		outputCount  int
	}{
		{
			name: "condition met",
			yaml: `
description: "Move with met condition"
condition:
  resources_exist:
    - layer: "SRC"
      addresses:
        - "aws_instance.web"
operations:
  - type: move
    source_layer: "SRC"
    destination_layer: "DST"
    resources:
      - from: "aws_instance.web"
        import_id: "i-0abc123"
`,
			srcResources: []*tfjson.StateResource{
				testutil.NewResource("aws_instance.web", "aws_instance", "web", nil,
					map[string]interface{}{"id": "i-0abc123"}),
			},
			outputCount: 2,
		},
		{
			name: "resources_exist fails",
			yaml: `
description: "Move with failing condition"
condition:
  resources_exist:
    - layer: "SRC"
      addresses:
        - "aws_instance.web"
        - "aws_instance.missing"
operations:
  - type: move
    source_layer: "SRC"
    destination_layer: "DST"
    resources:
      - from: "aws_instance.web"
        import_id: "i-0abc123"
`,
			srcResources: []*tfjson.StateResource{
				testutil.NewResource("aws_instance.web", "aws_instance", "web", nil,
					map[string]interface{}{"id": "i-0abc123"}),
			},
			outputCount: 0,
		},
		{
			name: "resources_not_exist fails",
			yaml: `
description: "Move blocked by destination check"
condition:
  resources_not_exist:
    - layer: "DST"
      addresses:
        - "aws_instance.web"
operations:
  - type: move
    source_layer: "SRC"
    destination_layer: "DST"
    resources:
      - from: "aws_instance.web"
        import_id: "i-0abc123"
`,
			srcResources: []*tfjson.StateResource{
				testutil.NewResource("aws_instance.web", "aws_instance", "web", nil,
					map[string]interface{}{"id": "i-0abc123"}),
			},
			dstResources: []*tfjson.StateResource{
				testutil.NewResource("aws_instance.web", "aws_instance", "web", nil,
					map[string]interface{}{"id": "i-0abc123"}),
			},
			outputCount: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir, layers := testutil.SetupLayers(t, "compute", "app")
			srcLayer := layers["compute"]
			dstLayer := layers["app"]

			yaml := strings.ReplaceAll(tc.yaml, "SRC", srcLayer)
			yaml = strings.ReplaceAll(yaml, "DST", dstLayer)
			migrationFile := testutil.WriteMigration(t, dir, "001.yaml", yaml)

			stateMap := map[string]*tfjson.State{}
			if len(tc.srcResources) > 0 {
				stateMap[srcLayer] = testutil.BuildState(tc.srcResources...)
			}
			if len(tc.dstResources) > 0 {
				stateMap[dstLayer] = testutil.BuildState(tc.dstResources...)
			}

			mock := testutil.NewMockStateReader(stateMap)
			result := runEngine(t, Config{StateReader: mock}, []string{migrationFile})
			testutil.RequireOutputCount(t, result.OutputFiles, tc.outputCount)
		})
	}
}

func TestEngine_ProcessFiles_ConditionStateError(t *testing.T) {
	dir := t.TempDir()
	migrationFile := testutil.WriteMigration(t, dir, "001.yaml", `
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
      - from: "aws_instance.web"
        import_id: "i-0abc123"
`)

	mock := testutil.NewMockStateReader(nil)

	// Strict mode: missing layers cause hard errors.
	err := runEngineExpectError(t, Config{StateReader: mock, Strict: true}, []string{migrationFile})
	if !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("expected error to mention layer does not exist, got: %v", err)
	}

	// Non-strict mode: missing layers are gracefully auto-skipped.
	result := runEngine(t, Config{StateReader: mock}, []string{migrationFile})
	testutil.RequireOutputCount(t, result.OutputFiles, 0)
	if len(result.SkippedFiles) != 1 {
		t.Fatalf("expected 1 skipped file, got %d", len(result.SkippedFiles))
	}
	if result.SkippedFiles[0].Reason != SkipLayerMissing {
		t.Errorf("expected SkipLayerMissing reason, got %v", result.SkippedFiles[0].Reason)
	}
}

func TestEngine_ProcessFiles_NoConditionUnchanged(t *testing.T) {
	dir, layers := testutil.SetupLayers(t, "net")
	layerDir := layers["net"]

	yaml := strings.ReplaceAll(`
description: "Rename without condition"
operations:
  - type: rename
    layer: "LAYER"
    renames:
      - from: "module.old"
        to: "module.new"
`, "LAYER", layerDir)
	migrationFile := testutil.WriteMigration(t, dir, "001.yaml", yaml)

	result := runEngine(t, Config{StateReader: testutil.NewMockStateReader(nil)}, []string{migrationFile})
	testutil.RequireOutputCount(t, result.OutputFiles, 1)
}

func TestEngine_ProcessFiles_PartialSkip(t *testing.T) {
	dir, layers := testutil.SetupLayers(t, "compute", "app", "net")
	srcLayer := layers["compute"]
	dstLayer := layers["app"]
	renameLayer := layers["net"]

	// First file: move with missing resource → will be skipped
	moveYaml := strings.ReplaceAll(`
description: "Move missing resource"
operations:
  - type: move
    source_layer: "SRC"
    destination_layer: "DST"
    resources:
      - from: "aws_instance.gone"
        import_id: "i-gone"
`, "SRC", srcLayer)
	moveYaml = strings.ReplaceAll(moveYaml, "DST", dstLayer)
	moveFile := testutil.WriteMigration(t, dir, "001_move.yaml", moveYaml)

	// Second file: simple rename → should succeed
	renameYaml := strings.ReplaceAll(`
description: "Rename VPC"
operations:
  - type: rename
    layer: "LAYER"
    renames:
      - from: "module.old"
        to: "module.new"
`, "LAYER", renameLayer)
	renameFile := testutil.WriteMigration(t, dir, "002_rename.yaml", renameYaml)

	mock := testutil.NewMockStateReader(map[string]*tfjson.State{
		srcLayer: testutil.BuildState(),
	})

	result := runEngine(t, Config{StateReader: mock}, []string{moveFile, renameFile})
	testutil.RequireOutputCount(t, result.OutputFiles, 1)

	content := testutil.ReadFirstOutput(t, result.OutputFiles)
	testutil.AssertContains(t, content, "moved {")
}

func TestEngine_ProcessFiles_AllSkipped(t *testing.T) {
	dir, layers := testutil.SetupLayers(t, "compute", "app")
	srcLayer := layers["compute"]
	dstLayer := layers["app"]

	// Both files reference missing resources
	var files []string
	for i, name := range []string{"001_move_a.yaml", "002_move_b.yaml"} {
		yaml := strings.ReplaceAll(`
description: "Move missing resource NAME"
operations:
  - type: move
    source_layer: "SRC"
    destination_layer: "DST"
    resources:
      - from: "aws_instance.missingIDX"
        import_id: "i-missing"
`, "SRC", srcLayer)
		yaml = strings.ReplaceAll(yaml, "DST", dstLayer)
		yaml = strings.ReplaceAll(yaml, "NAME", name)
		yaml = strings.ReplaceAll(yaml, "IDX", fmt.Sprintf("%d", i))
		files = append(files, testutil.WriteMigration(t, dir, name, yaml))
	}

	mock := testutil.NewMockStateReader(map[string]*tfjson.State{
		srcLayer: testutil.BuildState(),
	})

	err := runEngineExpectError(t, Config{StateReader: mock}, files)
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
	dir, layers := testutil.SetupLayers(t, "net")
	layerDir := layers["net"]

	// First file: condition references nonexistent layer → state error → skip
	condFile := testutil.WriteMigration(t, dir, "001_cond_err.yaml", `
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
      - from: "aws_instance.web"
        import_id: "i-0abc123"
`)

	// Second file: simple rename → succeeds
	renameYaml := strings.ReplaceAll(`
description: "Simple rename"
operations:
  - type: rename
    layer: "LAYER"
    renames:
      - from: "module.old"
        to: "module.new"
`, "LAYER", layerDir)
	renameFile := testutil.WriteMigration(t, dir, "002_rename.yaml", renameYaml)

	mock := testutil.NewMockStateReader(nil)

	result := runEngine(t, Config{StateReader: mock}, []string{condFile, renameFile})
	testutil.RequireOutputCount(t, result.OutputFiles, 1)

	content := testutil.ReadFirstOutput(t, result.OutputFiles)
	testutil.AssertContains(t, content, "moved {")
}

func TestEngine_ProcessFiles_LayerExistsCondition(t *testing.T) {
	dir, layers := testutil.SetupLayers(t, "net", "source")
	layerDir := layers["net"]
	existingDir := layers["source"]
	nonExistentDir := filepath.Join(dir, "layers", "gone")

	// layer_exists pointing to existing directory → proceeds
	yaml1 := strings.ReplaceAll(`
description: "Rename with existing layer condition"
condition:
  layer_exists:
    - "EXISTS"
operations:
  - type: rename
    layer: "LAYER"
    renames:
      - from: "module.old"
        to: "module.new"
`, "LAYER", layerDir)
	yaml1 = strings.ReplaceAll(yaml1, "EXISTS", existingDir)
	file1 := testutil.WriteMigration(t, dir, "001_exists.yaml", yaml1)

	result := runEngine(t, Config{StateReader: testutil.NewMockStateReader(nil)}, []string{file1})
	testutil.RequireOutputCount(t, result.OutputFiles, 1)

	// layer_exists pointing to non-existent directory → skipped
	yaml2 := strings.ReplaceAll(`
description: "Rename with missing layer condition"
condition:
  layer_exists:
    - "GONE"
operations:
  - type: rename
    layer: "LAYER"
    renames:
      - from: "module.alpha"
        to: "module.beta"
`, "LAYER", layerDir)
	yaml2 = strings.ReplaceAll(yaml2, "GONE", nonExistentDir)
	file2 := testutil.WriteMigration(t, dir, "002_missing.yaml", yaml2)

	result = runEngine(t, Config{StateReader: testutil.NewMockStateReader(nil)}, []string{file2})
	testutil.RequireOutputCount(t, result.OutputFiles, 0)
}

func TestEngine_ProcessFiles_LayerNotExistsCondition(t *testing.T) {
	dir, layers := testutil.SetupLayers(t, "net", "still_here")
	layerDir := layers["net"]
	existingDir := layers["still_here"]
	nonExistentDir := filepath.Join(dir, "layers", "deleted")

	// layer_not_exists pointing to existing directory → skipped
	yaml1 := strings.ReplaceAll(`
description: "Rename blocked by existing layer"
condition:
  layer_not_exists:
    - "EXISTS"
operations:
  - type: rename
    layer: "LAYER"
    renames:
      - from: "module.old"
        to: "module.new"
`, "LAYER", layerDir)
	yaml1 = strings.ReplaceAll(yaml1, "EXISTS", existingDir)
	file1 := testutil.WriteMigration(t, dir, "001_blocked.yaml", yaml1)

	result := runEngine(t, Config{StateReader: testutil.NewMockStateReader(nil)}, []string{file1})
	testutil.RequireOutputCount(t, result.OutputFiles, 0)

	// layer_not_exists pointing to non-existent directory → proceeds
	yaml2 := strings.ReplaceAll(`
description: "Rename when layer is gone"
condition:
  layer_not_exists:
    - "GONE"
operations:
  - type: rename
    layer: "LAYER"
    renames:
      - from: "module.alpha"
        to: "module.beta"
`, "LAYER", layerDir)
	yaml2 = strings.ReplaceAll(yaml2, "GONE", nonExistentDir)
	file2 := testutil.WriteMigration(t, dir, "002_allowed.yaml", yaml2)

	result = runEngine(t, Config{StateReader: testutil.NewMockStateReader(nil)}, []string{file2})
	testutil.RequireOutputCount(t, result.OutputFiles, 1)
}

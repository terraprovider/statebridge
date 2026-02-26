package engine

import (
	"strings"
	"testing"

	"github.com/redtenant/tfmigrate/internal/testutil"
)

func TestEngine_ProcessFiles_Remove(t *testing.T) {
	tests := []struct {
		name              string
		yaml              string
		removedBlockCount int
		destroyTrueCount  int
		destroyFalseCount int
	}{
		{
			name: "single remove",
			yaml: `
description: "Remove deprecated resource"
operations:
  - type: remove
    layer: "LAYER"
    entries:
      - address: "aws_iam_role.deprecated"
`,
			removedBlockCount: 1,
			destroyTrueCount:  0,
			destroyFalseCount: 1,
		},
		{
			name: "multiple addresses",
			yaml: `
description: "Multiple removals"
operations:
  - type: remove
    layer: "LAYER"
    entries:
      - address: "aws_iam_role.deprecated"
      - address: "aws_iam_policy.old"
`,
			removedBlockCount: 2,
			destroyTrueCount:  0,
			destroyFalseCount: 2,
		},
		{
			name: "destroy overrides",
			yaml: `
description: "Remove with destroy overrides"
operations:
  - type: remove
    layer: "LAYER"
    destroy: true
    entries:
      - address: "aws_iam_role.deprecated"
      - address: "aws_iam_policy.keep_infra"
        destroy: false
      - address: "aws_iam_policy.also_destroy"
`,
			removedBlockCount: 3,
			destroyTrueCount:  2,
			destroyFalseCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, layers := testutil.SetupLayers(t, "layer")
			layerDir := layers["layer"]

			yaml := strings.ReplaceAll(tt.yaml, "LAYER", layerDir)
			dir := t.TempDir()
			migrationFile := testutil.WriteMigration(t, dir, "001_remove.yaml", yaml)

			cfg := Config{StateReader: testutil.NewMockStateReader(nil)}
			result := runEngine(t, cfg, []string{migrationFile})

			testutil.RequireOutputCount(t, result.OutputFiles, 1)
			content := testutil.ReadFirstOutput(t, result.OutputFiles)

			testutil.AssertBlockCount(t, content, "removed {", tt.removedBlockCount)
			if tt.destroyTrueCount > 0 {
				testutil.AssertBlockCount(t, content, "destroy = true", tt.destroyTrueCount)
			}
			if tt.destroyFalseCount > 0 {
				testutil.AssertBlockCount(t, content, "destroy = false", tt.destroyFalseCount)
			}
		})
	}
}

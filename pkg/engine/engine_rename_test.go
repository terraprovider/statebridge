package engine

import (
	"strings"
	"testing"

	"github.com/terraprovider/statebridge/internal/testutil"
)

func TestEngine_ProcessFiles_Rename(t *testing.T) {
	tests := []struct {
		name             string
		yaml             string
		expectedContains []string
		movedBlockCount  int
	}{
		{
			name: "single rename",
			yaml: `
description: "Rename VPC module"
operations:
  - type: rename
    description: "Rename old to new"
    layer: "LAYER"
    renames:
      - from: "module.old_vpc"
        to: "module.new_vpc"
`,
			expectedContains: []string{"moved {", "module.old_vpc", "module.new_vpc"},
			movedBlockCount:  1,
		},
		{
			name: "multiple renames",
			yaml: `
description: "Multiple renames in one operation"
operations:
  - type: rename
    layer: "LAYER"
    renames:
      - from: "module.old_vpc"
        to: "module.new_vpc"
      - from: "aws_route_table.old"
        to: "aws_route_table.new"
`,
			expectedContains: []string{"moved {"},
			movedBlockCount:  2,
		},
		{
			name: "rename with address prefix",
			yaml: `
description: "Rename with address prefix"
operations:
  - type: rename
    layer: "LAYER"
    address_prefix: "module.vpc"
    renames:
      - from: "aws_subnet.old"
        to: "aws_subnet.new"
`,
			expectedContains: []string{"module.vpc.aws_subnet.old", "module.vpc.aws_subnet.new"},
			movedBlockCount:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, layers := testutil.SetupLayers(t, "net")
			layerDir := layers["net"]

			yaml := strings.ReplaceAll(tt.yaml, "LAYER", layerDir)
			dir := t.TempDir()
			migrationFile := testutil.WriteMigration(t, dir, "001_rename.yaml", yaml)

			cfg := Config{StateReader: testutil.NewMockStateReader(nil)}
			result := runEngine(t, cfg, []string{migrationFile})

			testutil.RequireOutputCount(t, result.OutputFiles, 1)
			content := testutil.ReadFirstOutput(t, result.OutputFiles)

			testutil.AssertBlockCount(t, content, "moved {", tt.movedBlockCount)
			for _, s := range tt.expectedContains {
				testutil.AssertContains(t, content, s)
			}
		})
	}
}

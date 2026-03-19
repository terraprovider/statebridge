package engine

import (
	"strings"
	"testing"

	"github.com/terraprovider/statebridge/internal/testutil"
)

func TestEngine_ProcessFiles_Import(t *testing.T) {
	tests := []struct {
		name             string
		yaml             string
		importBlockCount int
		expectedContains []string
		minCounts        map[string]int // substring → minimum occurrence count
	}{
		{
			name: "basic import with provider",
			yaml: `
description: "Import RDS instance"
operations:
  - type: import
    layer: "LAYER"
    imports:
      - address: "aws_db_instance.primary"
        id: "my-database"
        provider: "aws.useast1"
`,
			importBlockCount: 1,
			expectedContains: []string{"import {", `"my-database"`, "provider = aws.useast1"},
		},
		{
			name: "operation-level provider with entry override",
			yaml: `
description: "Import with operation-level and entry-level provider"
operations:
  - type: import
    layer: "LAYER"
    provider: "aws.useast1"
    imports:
      - address: "aws_db_instance.primary"
        id: "db-primary-id"
      - address: "aws_db_instance.replica"
        id: "db-replica-id"
        provider: "aws.uswest2"
      - address: "aws_db_instance.analytics"
        id: "db-analytics-id"
`,
			importBlockCount: 3,
			expectedContains: []string{"aws_db_instance.primary", "provider = aws.uswest2"},
			minCounts:        map[string]int{"provider = aws.useast1": 2},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, layers := testutil.SetupLayers(t, "db")
			layerDir := layers["db"]

			yaml := strings.ReplaceAll(tt.yaml, "LAYER", layerDir)
			dir := t.TempDir()
			migrationFile := testutil.WriteMigration(t, dir, "001_import.yaml", yaml)

			cfg := Config{StateReader: testutil.NewMockStateReader(nil)}
			result := runEngine(t, cfg, []string{migrationFile})

			testutil.RequireOutputCount(t, result.OutputFiles, 1)
			content := testutil.ReadFirstOutput(t, result.OutputFiles)

			testutil.AssertBlockCount(t, content, "import {", tt.importBlockCount)
			for _, s := range tt.expectedContains {
				testutil.AssertContains(t, content, s)
			}
			for substr, minCount := range tt.minCounts {
				if strings.Count(content, substr) < minCount {
					t.Errorf("expected at least %d occurrences of %q, got:\n%s", minCount, substr, content)
				}
			}
		})
	}
}

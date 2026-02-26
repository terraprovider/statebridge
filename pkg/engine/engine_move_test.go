package engine

import (
	"strings"
	"testing"

	tfjson "github.com/hashicorp/terraform-json"

	"github.com/redtenant/tfmigrate/internal/testutil"
)

func TestEngine_ProcessFiles_Move(t *testing.T) {
	tests := []struct {
		name                string
		yaml                string
		stateResources      []*tfjson.StateResource
		srcContains         []string
		dstContains         []string
		dstNotContains      []string
		srcRemovedCount     int
		dstImportCount      int
	}{
		{
			name: "simple move",
			yaml: `
description: "Move web instance"
operations:
  - type: move
    description: "Move to app layer"
    source_layer: "SRC"
    destination_layer: "DST"
    resources:
      - from: "aws_instance.web"
        import_id: "i-0abc123"
`,
			stateResources: []*tfjson.StateResource{
				testutil.NewResource("aws_instance.web", "aws_instance", "web", nil, map[string]interface{}{
					"id": "i-0abc123",
				}),
			},
			srcContains:     []string{"removed {", "aws_instance.web"},
			dstContains:     []string{"import {", `"i-0abc123"`},
			srcRemovedCount: 1,
			dstImportCount:  1,
		},
		{
			name: "for_each move all keys",
			yaml: `
description: "Move all instances"
operations:
  - type: move
    source_layer: "SRC"
    destination_layer: "DST"
    resources:
      - from: "aws_s3_bucket.data"
`,
			stateResources: []*tfjson.StateResource{
				testutil.NewResource(`aws_s3_bucket.data["key-a"]`, "aws_s3_bucket", "data", "key-a",
					map[string]interface{}{"id": "bucket-a-id"}),
				testutil.NewResource(`aws_s3_bucket.data["key-b"]`, "aws_s3_bucket", "data", "key-b",
					map[string]interface{}{"id": "bucket-b-id"}),
			},
			srcRemovedCount: 1,
			dstImportCount:  2,
			dstContains:     []string{`aws_s3_bucket.data["key-a"]`, `aws_s3_bucket.data["key-b"]`},
		},
		{
			name: "destination address override",
			yaml: `
description: "Move with destination address override"
operations:
  - type: move
    source_layer: "SRC"
    destination_layer: "DST"
    resources:
      - from: "module.old.resource.all"
        to: "module.new.resource.all"
        keys:
          key1: key1
`,
			stateResources: []*tfjson.StateResource{
				testutil.NewResource(`module.old.resource.all["key1"]`, "resource", "all", "key1",
					map[string]interface{}{"id": "id-key1"}),
			},
			srcContains:    []string{"module.old"},
			dstContains:    []string{`module.new.resource.all["key1"]`},
			srcRemovedCount: 1,
			dstImportCount:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, layers := testutil.SetupLayers(t, "src", "dst")
			srcLayer := layers["src"]
			dstLayer := layers["dst"]

			yaml := strings.ReplaceAll(strings.ReplaceAll(tt.yaml, "SRC", srcLayer), "DST", dstLayer)
			dir := t.TempDir()
			migrationFile := testutil.WriteMigration(t, dir, "001_move.yaml", yaml)

			mock := testutil.NewMockStateReader(map[string]*tfjson.State{
				srcLayer: testutil.BuildState(tt.stateResources...),
			})
			result := runEngine(t, Config{StateReader: mock}, []string{migrationFile})

			testutil.RequireOutputCount(t, result.OutputFiles, 2)

			srcContent := readLayerFile(t, result.OutputFiles, srcLayer)
			testutil.AssertBlockCount(t, srcContent, "removed {", tt.srcRemovedCount)
			for _, s := range tt.srcContains {
				testutil.AssertContains(t, srcContent, s)
			}

			dstContent := readLayerFile(t, result.OutputFiles, dstLayer)
			testutil.AssertBlockCount(t, dstContent, "import {", tt.dstImportCount)
			for _, s := range tt.dstContains {
				testutil.AssertContains(t, dstContent, s)
			}
			for _, s := range tt.dstNotContains {
				testutil.AssertNotContains(t, dstContent, s)
			}
		})
	}
}


func TestEngine_ProcessFiles_MultipleOperationsSameLayer(t *testing.T) {
	_, layers := testutil.SetupLayers(t, "net")
	layerDir := layers["net"]

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
    entries:
      - address: "aws_security_group.legacy"
  - type: import
    layer: "` + layerDir + `"
    imports:
      - address: "aws_route_table.new"
        id: "rtb-0abc123"
`
	dir := t.TempDir()
	migrationFile := testutil.WriteMigration(t, dir, "001_multi.yaml", migrationContent)

	cfg := Config{StateReader: testutil.NewMockStateReader(nil)}
	result := runEngine(t, cfg, []string{migrationFile})

	testutil.RequireOutputCount(t, result.OutputFiles, 1)
	content := testutil.ReadFirstOutput(t, result.OutputFiles)

	testutil.AssertContains(t, content, "moved {")
	testutil.AssertContains(t, content, "removed {")
	testutil.AssertContains(t, content, "import {")
}

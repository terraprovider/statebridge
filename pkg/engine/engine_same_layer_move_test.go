package engine

import (
	"strings"
	"testing"

	tfjson "github.com/hashicorp/terraform-json"

	"github.com/terraprovider/statebridge/internal/testutil"
)

func TestEngine_ProcessFiles_SameLayerMove(t *testing.T) {
	tests := []struct {
		name           string
		yaml           string
		stateResources []*tfjson.StateResource
		contains       []string
		notContains    []string
		movedCount     int
		removedCount   int
		importCount    int
		outputCount    int
	}{
		{
			name: "simple same-layer move generates moved block",
			yaml: `
description: "Same-layer rename via move"
operations:
  - type: move
    source_layer: "LAYER"
    destination_layer: "LAYER"
    resources:
      - from: "aws_instance.old"
        to: "aws_instance.new"
`,
			stateResources: []*tfjson.StateResource{
				testutil.NewResource("aws_instance.old", "aws_instance", "old", nil, map[string]interface{}{
					"id": "i-0abc123",
				}),
			},
			contains:     []string{"moved {", "aws_instance.old", "aws_instance.new"},
			notContains:  []string{"removed {", "import {"},
			movedCount:   1,
			removedCount: 0,
			importCount:  0,
			outputCount:  1,
		},
		{
			name: "same-layer identity move produces no blocks",
			yaml: `
description: "Identity same-layer move"
operations:
  - type: move
    source_layer: "LAYER"
    destination_layer: "LAYER"
    resources:
      - from: "aws_instance.web"
`,
			stateResources: []*tfjson.StateResource{
				testutil.NewResource("aws_instance.web", "aws_instance", "web", nil, map[string]interface{}{
					"id": "i-0abc123",
				}),
			},
			contains:     []string{},
			notContains:  []string{"moved {", "removed {", "import {"},
			movedCount:   0,
			removedCount: 0,
			importCount:  0,
			outputCount:  0,
		},
		{
			name: "same-layer for_each move generates moved blocks",
			yaml: `
description: "Same-layer for_each rename"
operations:
  - type: move
    source_layer: "LAYER"
    destination_layer: "LAYER"
    resources:
      - from: "aws_s3_bucket.data"
        to: "aws_s3_bucket.archive"
`,
			stateResources: []*tfjson.StateResource{
				testutil.NewResource(`aws_s3_bucket.data["key-a"]`, "aws_s3_bucket", "data", "key-a",
					map[string]interface{}{"id": "bucket-a"}),
				testutil.NewResource(`aws_s3_bucket.data["key-b"]`, "aws_s3_bucket", "data", "key-b",
					map[string]interface{}{"id": "bucket-b"}),
			},
			contains:     []string{"moved {", `aws_s3_bucket.data["key-a"]`, `aws_s3_bucket.archive["key-a"]`},
			notContains:  []string{"removed {", "import {"},
			movedCount:   2,
			removedCount: 0,
			importCount:  0,
			outputCount:  1,
		},
		{
			name: "same-layer keyed move with key remapping",
			yaml: `
description: "Same-layer keyed rekey"
operations:
  - type: move
    source_layer: "LAYER"
    destination_layer: "LAYER"
    resources:
      - from: "aws_resource.items"
        keys:
          old_key: new_key
          keep_key: keep_key
`,
			stateResources: []*tfjson.StateResource{
				testutil.NewResource(`aws_resource.items["old_key"]`, "aws_resource", "items", "old_key",
					map[string]interface{}{"id": "id-old"}),
				testutil.NewResource(`aws_resource.items["keep_key"]`, "aws_resource", "items", "keep_key",
					map[string]interface{}{"id": "id-keep"}),
			},
			contains:     []string{"moved {", `aws_resource.items["old_key"]`, `aws_resource.items["new_key"]`},
			notContains:  []string{"removed {", "import {"},
			movedCount:   1, // only old_key→new_key; keep_key→keep_key is identity (skipped)
			removedCount: 0,
			importCount:  0,
			outputCount:  1,
		},
		{
			name: "same-layer keyed move with prefix pattern",
			yaml: `
description: "Same-layer prefix rekey"
operations:
  - type: move
    source_layer: "LAYER"
    destination_layer: "LAYER"
    resources:
      - from: "aws_resource.items"
        keys:
          "prefix_*": '{{ .Key | trimPrefix "prefix_" }}'
`,
			stateResources: []*tfjson.StateResource{
				testutil.NewResource(`aws_resource.items["prefix_alpha"]`, "aws_resource", "items", "prefix_alpha",
					map[string]interface{}{"id": "id-alpha"}),
				testutil.NewResource(`aws_resource.items["prefix_beta"]`, "aws_resource", "items", "prefix_beta",
					map[string]interface{}{"id": "id-beta"}),
			},
			contains:     []string{"moved {", `aws_resource.items["alpha"]`, `aws_resource.items["beta"]`},
			notContains:  []string{"removed {", "import {"},
			movedCount:   2,
			removedCount: 0,
			importCount:  0,
			outputCount:  1,
		},
		{
			name: "same-layer module rename",
			yaml: `
description: "Same-layer module rename"
operations:
  - type: move
    source_layer: "LAYER"
    destination_layer: "LAYER"
    resources:
      - from: "module.old_name"
        to: "module.new_name"
`,
			stateResources: []*tfjson.StateResource{
				testutil.NewResource("module.old_name.aws_instance.web", "aws_instance", "web", nil,
					map[string]interface{}{"id": "i-123"}),
			},
			contains:     []string{"moved {", "module.old_name", "module.new_name"},
			notContains:  []string{"removed {", "import {"},
			movedCount:   1,
			removedCount: 0,
			importCount:  0,
			outputCount:  1,
		},
		{
			name: "same-layer module no rename is no-op",
			yaml: `
description: "Same-layer module identity"
operations:
  - type: move
    source_layer: "LAYER"
    destination_layer: "LAYER"
    resources:
      - from: "module.same"
`,
			stateResources: []*tfjson.StateResource{
				testutil.NewResource("module.same.aws_instance.web", "aws_instance", "web", nil,
					map[string]interface{}{"id": "i-123"}),
			},
			contains:     []string{},
			notContains:  []string{"moved {", "removed {", "import {"},
			movedCount:   0,
			removedCount: 0,
			importCount:  0,
			outputCount:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, layers := testutil.SetupLayers(t, "layer")
			layerDir := layers["layer"]

			yaml := strings.ReplaceAll(tt.yaml, "LAYER", layerDir)
			dir := t.TempDir()
			migrationFile := testutil.WriteMigration(t, dir, "001_move.yaml", yaml)

			mock := testutil.NewMockStateReader(map[string]*tfjson.State{
				layerDir: testutil.BuildState(tt.stateResources...),
			})
			result := runEngine(t, Config{StateReader: mock}, []string{migrationFile})

			testutil.RequireOutputCount(t, result.OutputFiles, tt.outputCount)

			if tt.outputCount > 0 {
				content := readLayerFile(t, result.OutputFiles, layerDir)
				testutil.AssertBlockCount(t, content, "moved {", tt.movedCount)
				testutil.AssertBlockCount(t, content, "removed {", tt.removedCount)
				testutil.AssertBlockCount(t, content, "import {", tt.importCount)
				for _, s := range tt.contains {
					testutil.AssertContains(t, content, s)
				}
				for _, s := range tt.notContains {
					testutil.AssertNotContains(t, content, s)
				}
			}
		})
	}
}

func TestEngine_ProcessFiles_SameLayerMove_MergeDuplicates(t *testing.T) {
	_, layers := testutil.SetupLayers(t, "layer")
	layerDir := layers["layer"]

	// Two source resources with keyed moves that produce the same destination address.
	// merge_duplicates: true should deduplicate the moved blocks.
	yaml := `
description: "Same-layer merge duplicates"
operations:
  - type: move
    source_layer: "` + layerDir + `"
    destination_layer: "` + layerDir + `"
    resources:
      - from: "aws_resource.policy_active"
        to: "aws_resource.policy"
        merge_duplicates: true
        keys:
          key_a: shared_key
          key_b: unique_active
      - from: "aws_resource.policy_eligible"
        to: "aws_resource.policy"
        merge_duplicates: true
        keys:
          key_x: shared_key
          key_y: unique_eligible
`
	dir := t.TempDir()
	migrationFile := testutil.WriteMigration(t, dir, "001_merge.yaml", yaml)

	mock := testutil.NewMockStateReader(map[string]*tfjson.State{
		layerDir: testutil.BuildState(
			// policy_active instances
			testutil.NewResource(`aws_resource.policy_active["key_a"]`, "aws_resource", "policy_active", "key_a",
				map[string]interface{}{"id": "id-active-a"}),
			testutil.NewResource(`aws_resource.policy_active["key_b"]`, "aws_resource", "policy_active", "key_b",
				map[string]interface{}{"id": "id-active-b"}),
			// policy_eligible instances
			testutil.NewResource(`aws_resource.policy_eligible["key_x"]`, "aws_resource", "policy_eligible", "key_x",
				map[string]interface{}{"id": "id-eligible-x"}),
			testutil.NewResource(`aws_resource.policy_eligible["key_y"]`, "aws_resource", "policy_eligible", "key_y",
				map[string]interface{}{"id": "id-eligible-y"}),
		),
	})

	result := runEngine(t, Config{StateReader: mock}, []string{migrationFile})

	testutil.RequireOutputCount(t, result.OutputFiles, 1)

	// Should have 3 moved blocks (not 4): shared_key is deduplicated
	content := readLayerFile(t, result.OutputFiles, layerDir)
	testutil.AssertBlockCount(t, content, "moved {", 3)
	testutil.AssertBlockCount(t, content, "removed {", 0)
	testutil.AssertBlockCount(t, content, "import {", 0)
	testutil.AssertContains(t, content, `aws_resource.policy["shared_key"]`)
	testutil.AssertContains(t, content, `aws_resource.policy["unique_active"]`)
	testutil.AssertContains(t, content, `aws_resource.policy["unique_eligible"]`)
}

func TestEngine_ProcessFiles_SourceDestinationPrefix(t *testing.T) {
	tests := []struct {
		name            string
		yaml            string
		stateResources  []*tfjson.StateResource
		srcContains     []string
		dstContains     []string
		srcRemovedCount int
		dstImportCount  int
	}{
		{
			name: "source and destination prefix",
			yaml: `
description: "Move with source/destination prefix"
operations:
  - type: move
    source_layer: "SRC"
    destination_layer: "DST"
    source_prefix: "module.old"
    destination_prefix: "module.new"
    resources:
      - from: "aws_instance.web"
`,
			stateResources: []*tfjson.StateResource{
				testutil.NewResource("module.old.aws_instance.web", "aws_instance", "web", nil,
					map[string]interface{}{"id": "i-0abc123"}),
				// Extra resource under module.old to prevent module-level consolidation.
				testutil.NewResource("module.old.aws_security_group.extra", "aws_security_group", "extra", nil,
					map[string]interface{}{"id": "sg-dummy"}),
			},
			srcContains:     []string{"removed {", "module.old.aws_instance.web"},
			dstContains:     []string{"import {", "module.new.aws_instance.web"},
			srcRemovedCount: 1,
			dstImportCount:  1,
		},
		{
			name: "source prefix only",
			yaml: `
description: "Move with only source prefix"
operations:
  - type: move
    source_layer: "SRC"
    destination_layer: "DST"
    source_prefix: "module.old"
    resources:
      - from: "aws_instance.web"
`,
			stateResources: []*tfjson.StateResource{
				testutil.NewResource("module.old.aws_instance.web", "aws_instance", "web", nil,
					map[string]interface{}{"id": "i-0abc123"}),
				testutil.NewResource("module.old.aws_security_group.extra", "aws_security_group", "extra", nil,
					map[string]interface{}{"id": "sg-dummy"}),
			},
			srcContains:     []string{"removed {", "module.old.aws_instance.web"},
			dstContains:     []string{"import {", "aws_instance.web"},
			srcRemovedCount: 1,
			dstImportCount:  1,
		},
		{
			name: "destination prefix only",
			yaml: `
description: "Move with only destination prefix"
operations:
  - type: move
    source_layer: "SRC"
    destination_layer: "DST"
    destination_prefix: "module.new"
    resources:
      - from: "aws_instance.web"
`,
			stateResources: []*tfjson.StateResource{
				testutil.NewResource("aws_instance.web", "aws_instance", "web", nil,
					map[string]interface{}{"id": "i-0abc123"}),
			},
			srcContains:     []string{"removed {", "aws_instance.web"},
			dstContains:     []string{"import {", "module.new.aws_instance.web"},
			srcRemovedCount: 1,
			dstImportCount:  1,
		},
		{
			name: "address_prefix backward compatibility",
			yaml: `
description: "Move with old address_prefix"
operations:
  - type: move
    source_layer: "SRC"
    destination_layer: "DST"
    address_prefix: "module.shared"
    resources:
      - from: "aws_instance.web"
`,
			stateResources: []*tfjson.StateResource{
				testutil.NewResource("module.shared.aws_instance.web", "aws_instance", "web", nil,
					map[string]interface{}{"id": "i-0abc123"}),
				testutil.NewResource("module.shared.aws_security_group.extra", "aws_security_group", "extra", nil,
					map[string]interface{}{"id": "sg-dummy"}),
			},
			srcContains:     []string{"removed {", "module.shared.aws_instance.web"},
			dstContains:     []string{"import {", "module.shared.aws_instance.web"},
			srcRemovedCount: 1,
			dstImportCount:  1,
		},
		{
			name: "source and destination prefix with keyed move",
			yaml: `
description: "Keyed move with source/destination prefix"
operations:
  - type: move
    source_layer: "SRC"
    destination_layer: "DST"
    source_prefix: "module.old"
    destination_prefix: "module.new"
    resources:
      - from: "aws_resource.items"
        keys:
          key_a: key_a
          key_b: renamed_b
`,
			stateResources: []*tfjson.StateResource{
				testutil.NewResource(`module.old.aws_resource.items["key_a"]`, "aws_resource", "items", "key_a",
					map[string]interface{}{"id": "id-a"}),
				testutil.NewResource(`module.old.aws_resource.items["key_b"]`, "aws_resource", "items", "key_b",
					map[string]interface{}{"id": "id-b"}),
				testutil.NewResource("module.old.aws_security_group.extra", "aws_security_group", "extra", nil,
					map[string]interface{}{"id": "sg-dummy"}),
			},
			srcContains:     []string{"removed {", "module.old.aws_resource.items"},
			dstContains:     []string{"import {", `module.new.aws_resource.items["key_a"]`, `module.new.aws_resource.items["renamed_b"]`},
			srcRemovedCount: 1,
			dstImportCount:  2,
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
		})
	}
}

func TestEngine_ProcessFiles_SameLayerWithSourceDestPrefix(t *testing.T) {
	_, layers := testutil.SetupLayers(t, "layer")
	layerDir := layers["layer"]

	yaml := `
description: "Same-layer move with different prefixes"
operations:
  - type: move
    source_layer: "` + layerDir + `"
    destination_layer: "` + layerDir + `"
    source_prefix: "module.old"
    destination_prefix: "module.new"
    resources:
      - from: "aws_instance.web"
`
	dir := t.TempDir()
	migrationFile := testutil.WriteMigration(t, dir, "001_move.yaml", yaml)

	mock := testutil.NewMockStateReader(map[string]*tfjson.State{
		layerDir: testutil.BuildState(
			testutil.NewResource("module.old.aws_instance.web", "aws_instance", "web", nil,
				map[string]interface{}{"id": "i-0abc123"}),
		),
	})
	result := runEngine(t, Config{StateReader: mock}, []string{migrationFile})

	testutil.RequireOutputCount(t, result.OutputFiles, 1)
	content := readLayerFile(t, result.OutputFiles, layerDir)
	testutil.AssertBlockCount(t, content, "moved {", 1)
	testutil.AssertBlockCount(t, content, "removed {", 0)
	testutil.AssertBlockCount(t, content, "import {", 0)
	testutil.AssertContains(t, content, "module.old.aws_instance.web")
	testutil.AssertContains(t, content, "module.new.aws_instance.web")
}

func TestEngine_ProcessFiles_SameLayerMove_UseMovedBlocksFalse(t *testing.T) {
	tests := []struct {
		name           string
		yaml           string
		stateResources []*tfjson.StateResource
		removedCount   int
		importCount    int
		movedCount     int
		contains       []string
	}{
		{
			name: "operation-level use_moved_blocks false generates removed+import",
			yaml: `
description: "Same-layer with use_moved_blocks false"
operations:
  - type: move
    source_layer: "LAYER"
    destination_layer: "LAYER"
    use_moved_blocks: false
    resources:
      - from: "aws_instance.web"
        to: "aws_instance.api"
`,
			stateResources: []*tfjson.StateResource{
				testutil.NewResource("aws_instance.web", "aws_instance", "web", nil,
					map[string]interface{}{"id": "i-0abc123"}),
			},
			removedCount: 1,
			importCount:  1,
			movedCount:   0,
			contains:     []string{"aws_instance.web", "aws_instance.api", `"i-0abc123"`},
		},
		{
			name: "per-resource use_moved_blocks false overrides operation default",
			yaml: `
description: "Per-resource override"
operations:
  - type: move
    source_layer: "LAYER"
    destination_layer: "LAYER"
    resources:
      - from: "aws_instance.web"
        to: "aws_instance.api"
        use_moved_blocks: false
      - from: "aws_instance.db"
        to: "aws_instance.database"
`,
			stateResources: []*tfjson.StateResource{
				testutil.NewResource("aws_instance.web", "aws_instance", "web", nil,
					map[string]interface{}{"id": "i-0abc123"}),
				testutil.NewResource("aws_instance.db", "aws_instance", "db", nil,
					map[string]interface{}{"id": "i-0def456"}),
			},
			removedCount: 1,
			importCount:  1,
			movedCount:   1,
			contains:     []string{"aws_instance.web", "aws_instance.api", "aws_instance.database"},
		},
		{
			name: "per-resource use_moved_blocks true overrides operation false",
			yaml: `
description: "Per-resource true overrides op false"
operations:
  - type: move
    source_layer: "LAYER"
    destination_layer: "LAYER"
    use_moved_blocks: false
    resources:
      - from: "aws_instance.web"
        to: "aws_instance.api"
      - from: "aws_instance.db"
        to: "aws_instance.database"
        use_moved_blocks: true
`,
			stateResources: []*tfjson.StateResource{
				testutil.NewResource("aws_instance.web", "aws_instance", "web", nil,
					map[string]interface{}{"id": "i-0abc123"}),
				testutil.NewResource("aws_instance.db", "aws_instance", "db", nil,
					map[string]interface{}{"id": "i-0def456"}),
			},
			removedCount: 1,
			importCount:  1,
			movedCount:   1,
			contains:     []string{"aws_instance.web", "aws_instance.api", "aws_instance.database"},
		},
		{
			name: "for_each resource with use_moved_blocks false",
			yaml: `
description: "For_each with use_moved_blocks false"
operations:
  - type: move
    source_layer: "LAYER"
    destination_layer: "LAYER"
    use_moved_blocks: false
    resources:
      - from: "aws_instance.web"
        to: "aws_instance.api"
`,
			stateResources: []*tfjson.StateResource{
				testutil.NewResource(`aws_instance.web["a"]`, "aws_instance", "web", "a",
					map[string]interface{}{"id": "i-aaa"}),
				testutil.NewResource(`aws_instance.web["b"]`, "aws_instance", "web", "b",
					map[string]interface{}{"id": "i-bbb"}),
			},
			removedCount: 1,
			importCount:  2,
			movedCount:   0,
			contains:     []string{`aws_instance.web`, `aws_instance.api["a"]`, `aws_instance.api["b"]`},
		},
		{
			name: "keyed move with use_moved_blocks false",
			yaml: `
description: "Keyed move with use_moved_blocks false"
operations:
  - type: move
    source_layer: "LAYER"
    destination_layer: "LAYER"
    use_moved_blocks: false
    resources:
      - from: "aws_resource.items"
        keys:
          old_key: new_key
`,
			stateResources: []*tfjson.StateResource{
				testutil.NewResource(`aws_resource.items["old_key"]`, "aws_resource", "items", "old_key",
					map[string]interface{}{"id": "id-old"}),
			},
			removedCount: 1,
			importCount:  1,
			movedCount:   0,
			contains:     []string{`aws_resource.items`, `aws_resource.items["new_key"]`, `"id-old"`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, layers := testutil.SetupLayers(t, "layer")
			layerDir := layers["layer"]

			yaml := strings.ReplaceAll(tt.yaml, "LAYER", layerDir)
			dir := t.TempDir()
			migrationFile := testutil.WriteMigration(t, dir, "001_move.yaml", yaml)

			mock := testutil.NewMockStateReader(map[string]*tfjson.State{
				layerDir: testutil.BuildState(tt.stateResources...),
			})
			result := runEngine(t, Config{StateReader: mock}, []string{migrationFile})

			testutil.RequireOutputCount(t, result.OutputFiles, 1)
			content := readLayerFile(t, result.OutputFiles, layerDir)
			testutil.AssertBlockCount(t, content, "removed {", tt.removedCount)
			testutil.AssertBlockCount(t, content, "import {", tt.importCount)
			testutil.AssertBlockCount(t, content, "moved {", tt.movedCount)
			for _, s := range tt.contains {
				testutil.AssertContains(t, content, s)
			}
		})
	}
}

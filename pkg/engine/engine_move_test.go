package engine

import (
	"encoding/json"
	"strings"
	"testing"

	tfjson "github.com/hashicorp/terraform-json"

	"github.com/terraprovider/statebridge/internal/testutil"
)

func TestEngine_ProcessFiles_Move(t *testing.T) {
	tests := []struct {
		name            string
		yaml            string
		stateResources  []*tfjson.StateResource
		srcContains     []string
		dstContains     []string
		dstNotContains  []string
		srcRemovedCount int
		dstImportCount  int
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
			srcContains:     []string{"module.old"},
			dstContains:     []string{`module.new.resource.all["key1"]`},
			srcRemovedCount: 1,
			dstImportCount:  1,
		},
		{
			// A count-indexed resource: the instance keys are integers and must
			// stay bare ([0], [1]) in generated addresses — never quoted as
			// for_each keys (["0"]), which would not match the resource in state.
			name: "count-indexed resource move keeps bare integer keys",
			yaml: `
description: "Move a count resource"
operations:
  - type: move
    source_layer: "SRC"
    destination_layer: "DST"
    resources:
      - from: "aws_instance.web"
`,
			// Indices are json.Number to mirror how terraform-exec's Show
			// decodes real state (it enables UseJSONNumber).
			stateResources: []*tfjson.StateResource{
				testutil.NewResource("aws_instance.web[0]", "aws_instance", "web", json.Number("0"),
					map[string]interface{}{"id": "i-0"}),
				testutil.NewResource("aws_instance.web[1]", "aws_instance", "web", json.Number("1"),
					map[string]interface{}{"id": "i-1"}),
			},
			srcRemovedCount: 1,
			dstImportCount:  2,
			dstContains:     []string{"aws_instance.web[0]", "aws_instance.web[1]"},
			dstNotContains:  []string{`aws_instance.web["0"]`, `aws_instance.web["1"]`},
		},
		{
			// Reproduces the reported bug: a count-indexed resource inside an
			// indexed module. The trailing resource count index must render as
			// [0], not ["0"].
			name: "count-indexed resource inside indexed module",
			yaml: `
description: "Move ca_pilot_all_users"
operations:
  - type: move
    source_layer: "SRC"
    destination_layer: "DST"
    resources:
      - from: "module.conditional_access[0].azuread_group_without_members.ca_pilot_all_users"
`,
			stateResources: []*tfjson.StateResource{
				testutil.NewResource(
					"module.conditional_access[0].azuread_group_without_members.ca_pilot_all_users[0]",
					"azuread_group_without_members", "ca_pilot_all_users", json.Number("0"),
					map[string]interface{}{"id": "grp-pilot"}),
				// Sibling keeps the module instance non-empty so the removed block
				// stays resource-level (avoids the multi-instance guard path).
				testutil.NewResource(
					"module.conditional_access[0].azuread_application.app",
					"azuread_application", "app", nil,
					map[string]interface{}{"id": "app-1"}),
			},
			srcRemovedCount: 1,
			dstImportCount:  1,
			dstContains: []string{
				"module.conditional_access[0].azuread_group_without_members.ca_pilot_all_users[0]",
			},
			dstNotContains: []string{
				`ca_pilot_all_users["0"]`,
			},
		},
		{
			name: "for_each move under indexed module",
			yaml: `
description: "Move a for_each resource out of an indexed module instance"
operations:
  - type: move
    source_layer: "SRC"
    destination_layer: "DST"
    resources:
      - from: "module.configuration_policies[0].azuread_group_without_members.all"
`,
			stateResources: []*tfjson.StateResource{
				testutil.NewResource(
					`module.configuration_policies[0].azuread_group_without_members.all["cfg_intune_rdp_access_allowed"]`,
					"azuread_group_without_members", "all", "cfg_intune_rdp_access_allowed",
					map[string]interface{}{"id": "grp-rdp"}),
				testutil.NewResource(
					`module.configuration_policies[0].azuread_group_without_members.all["cfg_other"]`,
					"azuread_group_without_members", "all", "cfg_other",
					map[string]interface{}{"id": "grp-other"}),
				// A sibling resource that is NOT moved, so the module instance is
				// not fully emptied and the removed block stays resource-level.
				testutil.NewResource(
					"module.configuration_policies[0].azuread_application.app",
					"azuread_application", "app", nil,
					map[string]interface{}{"id": "app-1"}),
			},
			srcContains: []string{
				"removed {",
				// Module-instance index is stripped for the removed block (the only
				// form OpenTofu accepts); import blocks keep the full indexed key.
				"from = module.configuration_policies.azuread_group_without_members.all",
			},
			dstContains: []string{
				`module.configuration_policies[0].azuread_group_without_members.all["cfg_intune_rdp_access_allowed"]`,
				`module.configuration_policies[0].azuread_group_without_members.all["cfg_other"]`,
				`"grp-rdp"`,
				`"grp-other"`,
			},
			dstNotContains:  []string{"azuread_application"},
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
			for _, s := range tt.dstNotContains {
				testutil.AssertNotContains(t, dstContent, s)
			}
		})
	}
}

// TestEngine_ProcessFiles_Move_IndexedModuleMultiInstanceError verifies the
// guard that refuses a cross-layer move of a resource out of one instance of a
// multi-instance (count/for_each) module. Because the source removed block must
// use a module-index-stripped address that OpenTofu applies to ALL module
// instances, allowing this would orphan the other instances' state.
func TestEngine_ProcessFiles_Move_IndexedModuleMultiInstanceError(t *testing.T) {
	dir, layers := testutil.SetupLayers(t, "src", "dst")
	srcLayer := layers["src"]
	dstLayer := layers["dst"]

	yaml := strings.ReplaceAll(strings.ReplaceAll(`
description: "Move a for_each resource out of one instance of a count=2 module"
operations:
  - type: move
    source_layer: "SRC"
    destination_layer: "DST"
    resources:
      - from: "module.configuration_policies[0].random_id.items"
`, "SRC", srcLayer), "DST", dstLayer)
	migrationFile := testutil.WriteMigration(t, dir, "001_move.yaml", yaml)

	// State holds the same resource under two module instances ([0] and [1]).
	mock := testutil.NewMockStateReader(map[string]*tfjson.State{
		srcLayer: testutil.BuildState(
			testutil.NewResource(`module.configuration_policies[0].random_id.items["a"]`,
				"random_id", "items", "a", map[string]interface{}{"id": "id-0a"}),
			testutil.NewResource(`module.configuration_policies[1].random_id.items["a"]`,
				"random_id", "items", "a", map[string]interface{}{"id": "id-1a"}),
		),
	})

	err := runEngineExpectError(t, Config{StateReader: mock}, []string{migrationFile})
	for _, want := range []string{
		"multi-instance module",
		"module.configuration_policies[0].random_id.items",
		"module.configuration_policies[1].random_id.items",
		"module.configuration_policies.random_id.items",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("expected error to contain %q, got: %v", want, err)
		}
	}
}

// TestEngine_ProcessFiles_Move_IndexedModuleSingleInstanceOK verifies the guard
// does NOT trigger for a single-instance (count = 1) module — the common
// conditional-module pattern — which is safe to move.
func TestEngine_ProcessFiles_Move_IndexedModuleSingleInstanceOK(t *testing.T) {
	dir, layers := testutil.SetupLayers(t, "src", "dst")
	srcLayer := layers["src"]
	dstLayer := layers["dst"]

	yaml := strings.ReplaceAll(strings.ReplaceAll(`
description: "Move a for_each resource out of a single-instance module"
operations:
  - type: move
    source_layer: "SRC"
    destination_layer: "DST"
    resources:
      - from: "module.configuration_policies[0].random_id.items"
`, "SRC", srcLayer), "DST", dstLayer)
	migrationFile := testutil.WriteMigration(t, dir, "001_move.yaml", yaml)

	mock := testutil.NewMockStateReader(map[string]*tfjson.State{
		srcLayer: testutil.BuildState(
			testutil.NewResource(`module.configuration_policies[0].random_id.items["a"]`,
				"random_id", "items", "a", map[string]interface{}{"id": "id-0a"}),
			testutil.NewResource(`module.configuration_policies[0].random_id.items["b"]`,
				"random_id", "items", "b", map[string]interface{}{"id": "id-0b"}),
		),
	})

	result := runEngine(t, Config{StateReader: mock}, []string{migrationFile})

	srcContent := readLayerFile(t, result.OutputFiles, srcLayer)
	// Removed block uses the module-index-stripped config address.
	testutil.AssertContains(t, srcContent, "from = module.configuration_policies.random_id.items")

	dstContent := readLayerFile(t, result.OutputFiles, dstLayer)
	testutil.AssertBlockCount(t, dstContent, "import {", 2)
	testutil.AssertContains(t, dstContent, `module.configuration_policies[0].random_id.items["a"]`)
	testutil.AssertContains(t, dstContent, `module.configuration_policies[0].random_id.items["b"]`)
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

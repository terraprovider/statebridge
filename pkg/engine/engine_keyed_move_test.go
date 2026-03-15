package engine

import (
	"strings"
	"testing"

	tfjson "github.com/hashicorp/terraform-json"

	"github.com/redtenant/tfmigrate/internal/testutil"
)

func TestEngine_ProcessFiles_KeyedMove(t *testing.T) {
	tests := []struct {
		name            string
		yaml            string
		stateResources  []*tfjson.StateResource
		dstContains     []string
		srcRemovedCount int
		dstImportCount  int
	}{
		{
			name: "exact keys and prefix pattern",
			yaml: `
description: "Keyed move"
operations:
  - type: move
    source_layer: "SRC"
    destination_layer: "DST"
    resources:
      - from: "aws_resource.items"
        keys:
          exact_key: new_exact
          "prefix_*": '{{ .Key | trimPrefix "prefix_" }}'
`,
			stateResources: []*tfjson.StateResource{
				testutil.NewResource(`aws_resource.items["exact_key"]`, "aws_resource", "items", "exact_key",
					map[string]interface{}{"id": "id-exact"}),
				testutil.NewResource(`aws_resource.items["prefix_alpha"]`, "aws_resource", "items", "prefix_alpha",
					map[string]interface{}{"id": "id-alpha"}),
				testutil.NewResource(`aws_resource.items["prefix_beta"]`, "aws_resource", "items", "prefix_beta",
					map[string]interface{}{"id": "id-beta"}),
			},
			dstContains:     []string{`aws_resource.items["new_exact"]`, `aws_resource.items["alpha"]`, `aws_resource.items["beta"]`},
			srcRemovedCount: 1,
			dstImportCount:  3,
		},
		{
			name: "with address prefix",
			yaml: `
description: "Keyed move with address prefix"
operations:
  - type: move
    source_layer: "SRC"
    destination_layer: "DST"
    address_prefix: "module.ig"
    resources:
      - from: "azuread_access_package_catalog.all"
        keys:
          mrt_customer: customer_approval
          mrt_vaw: vaw
`,
			stateResources: []*tfjson.StateResource{
				testutil.NewResource(
					`module.ig.azuread_access_package_catalog.all["mrt_customer"]`,
					"azuread_access_package_catalog", "all", "mrt_customer",
					map[string]interface{}{"id": "id-customer"}),
				testutil.NewResource(
					`module.ig.azuread_access_package_catalog.all["mrt_vaw"]`,
					"azuread_access_package_catalog", "all", "mrt_vaw",
					map[string]interface{}{"id": "id-vaw"}),
			},
			dstContains: []string{
				`module.ig.azuread_access_package_catalog.all["customer_approval"]`,
				`module.ig.azuread_access_package_catalog.all["vaw"]`,
			},
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
			migrationFile := testutil.WriteMigration(t, dir, "001_keyed.yaml", yaml)

			mock := testutil.NewMockStateReader(map[string]*tfjson.State{
				srcLayer: testutil.BuildState(tt.stateResources...),
			})
			result := runEngine(t, Config{StateReader: mock}, []string{migrationFile})

			testutil.RequireOutputCount(t, result.OutputFiles, 2)

			srcContent := readLayerFile(t, result.OutputFiles, srcLayer)
			testutil.AssertBlockCount(t, srcContent, "removed {", tt.srcRemovedCount)

			dstContent := readLayerFile(t, result.OutputFiles, dstLayer)
			testutil.AssertBlockCount(t, dstContent, "import {", tt.dstImportCount)
			for _, s := range tt.dstContains {
				testutil.AssertContains(t, dstContent, s)
			}
		})
	}
}

func TestEngine_ProcessFiles_KeyedMoveSplitAcrossOps(t *testing.T) {
	_, layers := testutil.SetupLayers(t, "old", "engineering", "finance")
	srcLayer := layers["old"]
	engLayer := layers["engineering"]
	finLayer := layers["finance"]

	migrationContent := `
description: "Split by department"
operations:
  - type: move
    source_layer: "` + srcLayer + `"
    destination_layer: "` + engLayer + `"
    resources:
      - from: "aws_resource.items"
        keys:
          "eng_*": '{{ .Key }}'
  - type: move
    source_layer: "` + srcLayer + `"
    destination_layer: "` + finLayer + `"
    resources:
      - from: "aws_resource.items"
        keys:
          "fin_*": '{{ .Key }}'
`
	dir := t.TempDir()
	migrationFile := testutil.WriteMigration(t, dir, "001_split.yaml", migrationContent)

	mock := testutil.NewMockStateReader(map[string]*tfjson.State{
		srcLayer: testutil.BuildState(
			testutil.NewResource(`aws_resource.items["eng_admin"]`, "aws_resource", "items", "eng_admin",
				map[string]interface{}{"id": "id-eng-admin"}),
			testutil.NewResource(`aws_resource.items["eng_reader"]`, "aws_resource", "items", "eng_reader",
				map[string]interface{}{"id": "id-eng-reader"}),
			testutil.NewResource(`aws_resource.items["fin_admin"]`, "aws_resource", "items", "fin_admin",
				map[string]interface{}{"id": "id-fin-admin"}),
			testutil.NewResource(`aws_resource.items["fin_reader"]`, "aws_resource", "items", "fin_reader",
				map[string]interface{}{"id": "id-fin-reader"}),
		),
	})

	result := runEngine(t, Config{StateReader: mock}, []string{migrationFile})

	testutil.RequireOutputCount(t, result.OutputFiles, 3)

	srcContent := readLayerFile(t, result.OutputFiles, srcLayer)
	testutil.AssertBlockCount(t, srcContent, "removed {", 1)

	engContent := readLayerFile(t, result.OutputFiles, engLayer)
	testutil.AssertBlockCount(t, engContent, "import {", 2)

	finContent := readLayerFile(t, result.OutputFiles, finLayer)
	testutil.AssertBlockCount(t, finContent, "import {", 2)
}

func TestEngine_ProcessFiles_IncompleteCoverage(t *testing.T) {
	_, layers := testutil.SetupLayers(t, "old", "new")
	srcLayer := layers["old"]
	dstLayer := layers["new"]

	migrationContent := `
description: "Incomplete coverage"
operations:
  - type: move
    source_layer: "` + srcLayer + `"
    destination_layer: "` + dstLayer + `"
    resources:
      - from: "aws_resource.items"
        keys:
          "eng_*": '{{ .Key }}'
`
	dir := t.TempDir()
	migrationFile := testutil.WriteMigration(t, dir, "001_incomplete.yaml", migrationContent)

	mock := testutil.NewMockStateReader(map[string]*tfjson.State{
		srcLayer: testutil.BuildState(
			testutil.NewResource(`aws_resource.items["eng_admin"]`, "aws_resource", "items", "eng_admin",
				map[string]interface{}{"id": "id-eng-admin"}),
			testutil.NewResource(`aws_resource.items["other_admin"]`, "aws_resource", "items", "other_admin",
				map[string]interface{}{"id": "id-other-admin"}),
		),
	})

	// Incomplete coverage causes the file to be skipped (not a fatal error).
	// Since it's the only file, ProcessFiles returns "all migration files were skipped".
	err := runEngineExpectError(t, Config{StateReader: mock}, []string{migrationFile})
	if !strings.Contains(err.Error(), "skipped") {
		t.Errorf("expected error to mention files were skipped, got: %v", err)
	}
}

func TestEngine_ProcessFiles_MergeDuplicates(t *testing.T) {
	_, layers := testutil.SetupLayers(t, "src", "dst")
	srcLayer := layers["src"]
	dstLayer := layers["dst"]

	// Two source resources with keyed moves that produce the same destination address.
	// merge_duplicates: true should deduplicate the import blocks.
	yaml := `
description: "Merge duplicates"
operations:
  - type: move
    source_layer: "` + srcLayer + `"
    destination_layer: "` + dstLayer + `"
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
		srcLayer: testutil.BuildState(
			// policy_active instances
			testutil.NewResource(`aws_resource.policy_active["key_a"]`, "aws_resource", "policy_active", "key_a",
				map[string]interface{}{"id": "shared-id"}),
			testutil.NewResource(`aws_resource.policy_active["key_b"]`, "aws_resource", "policy_active", "key_b",
				map[string]interface{}{"id": "id-active-b"}),
			// policy_eligible instances
			testutil.NewResource(`aws_resource.policy_eligible["key_x"]`, "aws_resource", "policy_eligible", "key_x",
				map[string]interface{}{"id": "shared-id"}),
			testutil.NewResource(`aws_resource.policy_eligible["key_y"]`, "aws_resource", "policy_eligible", "key_y",
				map[string]interface{}{"id": "id-eligible-y"}),
		),
	})

	result := runEngine(t, Config{StateReader: mock}, []string{migrationFile})

	testutil.RequireOutputCount(t, result.OutputFiles, 2)

	// Source should have two removed blocks (one per source resource)
	srcContent := readLayerFile(t, result.OutputFiles, srcLayer)
	testutil.AssertBlockCount(t, srcContent, "removed {", 2)

	// Destination should have 3 import blocks (not 4): shared_key is deduplicated
	dstContent := readLayerFile(t, result.OutputFiles, dstLayer)
	testutil.AssertBlockCount(t, dstContent, "import {", 3)
	testutil.AssertContains(t, dstContent, `aws_resource.policy["shared_key"]`)
	testutil.AssertContains(t, dstContent, `aws_resource.policy["unique_active"]`)
	testutil.AssertContains(t, dstContent, `aws_resource.policy["unique_eligible"]`)
}

func TestEngine_ProcessFiles_MergeDuplicates_SameResource(t *testing.T) {
	_, layers := testutil.SetupLayers(t, "src", "dst")
	srcLayer := layers["src"]
	dstLayer := layers["dst"]

	// Two source resources mapping different keys to the same dest address on the same resource
	yaml := `
description: "Merge duplicates same dest resource"
operations:
  - type: move
    source_layer: "` + srcLayer + `"
    destination_layer: "` + dstLayer + `"
    resources:
      - from: "aws_resource.policy_active"
        merge_duplicates: true
        to: "aws_resource.policy"
        keys:
          key_a: merged
      - from: "aws_resource.policy_eligible"
        merge_duplicates: true
        to: "aws_resource.policy"
        keys:
          key_x: merged
          key_y: only_eligible
`
	dir := t.TempDir()
	migrationFile := testutil.WriteMigration(t, dir, "001_merge_same.yaml", yaml)

	mock := testutil.NewMockStateReader(map[string]*tfjson.State{
		srcLayer: testutil.BuildState(
			testutil.NewResource(`aws_resource.policy_active["key_a"]`, "aws_resource", "policy_active", "key_a",
				map[string]interface{}{"id": "shared-id"}),
			testutil.NewResource(`aws_resource.policy_eligible["key_x"]`, "aws_resource", "policy_eligible", "key_x",
				map[string]interface{}{"id": "shared-id"}),
			testutil.NewResource(`aws_resource.policy_eligible["key_y"]`, "aws_resource", "policy_eligible", "key_y",
				map[string]interface{}{"id": "id-eligible-y"}),
		),
	})

	result := runEngine(t, Config{StateReader: mock}, []string{migrationFile})

	testutil.RequireOutputCount(t, result.OutputFiles, 2)

	dstContent := readLayerFile(t, result.OutputFiles, dstLayer)
	// Should have 2 imports: "merged" (deduplicated) and "only_eligible"
	testutil.AssertBlockCount(t, dstContent, "import {", 2)
	testutil.AssertContains(t, dstContent, `aws_resource.policy["merged"]`)
	testutil.AssertContains(t, dstContent, `aws_resource.policy["only_eligible"]`)
}

func TestEngine_ProcessFiles_DuplicateDestWithoutMerge(t *testing.T) {
	_, layers := testutil.SetupLayers(t, "src", "dst")
	srcLayer := layers["src"]
	dstLayer := layers["dst"]

	// Two source resources producing same destination without merge_duplicates → error
	yaml := `
description: "Duplicate without merge"
operations:
  - type: move
    source_layer: "` + srcLayer + `"
    destination_layer: "` + dstLayer + `"
    resources:
      - from: "aws_resource.policy_active"
        to: "aws_resource.policy"
        keys:
          key_a: merged
      - from: "aws_resource.policy_eligible"
        to: "aws_resource.policy"
        keys:
          key_x: merged
`
	dir := t.TempDir()
	migrationFile := testutil.WriteMigration(t, dir, "001_no_merge.yaml", yaml)

	mock := testutil.NewMockStateReader(map[string]*tfjson.State{
		srcLayer: testutil.BuildState(
			testutil.NewResource(`aws_resource.policy_active["key_a"]`, "aws_resource", "policy_active", "key_a",
				map[string]interface{}{"id": "shared-id"}),
			testutil.NewResource(`aws_resource.policy_eligible["key_x"]`, "aws_resource", "policy_eligible", "key_x",
				map[string]interface{}{"id": "shared-id"}),
		),
	})

	err := runEngineExpectError(t, Config{StateReader: mock}, []string{migrationFile})
	if !strings.Contains(err.Error(), "skipped") {
		t.Errorf("expected error mentioning 'skipped', got: %v", err)
	}
}

func TestEngine_ProcessFiles_MergeDuplicates_ConflictingIDs(t *testing.T) {
	_, layers := testutil.SetupLayers(t, "src", "dst")
	srcLayer := layers["src"]
	dstLayer := layers["dst"]

	// merge_duplicates is true but import IDs differ → error
	yaml := `
description: "Merge duplicates conflicting IDs"
operations:
  - type: move
    source_layer: "` + srcLayer + `"
    destination_layer: "` + dstLayer + `"
    resources:
      - from: "aws_resource.policy_active"
        merge_duplicates: true
        to: "aws_resource.policy"
        keys:
          key_a: merged
      - from: "aws_resource.policy_eligible"
        merge_duplicates: true
        to: "aws_resource.policy"
        keys:
          key_x: merged
`
	dir := t.TempDir()
	migrationFile := testutil.WriteMigration(t, dir, "001_conflict.yaml", yaml)

	mock := testutil.NewMockStateReader(map[string]*tfjson.State{
		srcLayer: testutil.BuildState(
			testutil.NewResource(`aws_resource.policy_active["key_a"]`, "aws_resource", "policy_active", "key_a",
				map[string]interface{}{"id": "id-ALPHA"}),
			testutil.NewResource(`aws_resource.policy_eligible["key_x"]`, "aws_resource", "policy_eligible", "key_x",
				map[string]interface{}{"id": "id-BETA"}),
		),
	})

	err := runEngineExpectError(t, Config{StateReader: mock}, []string{migrationFile})
	if !strings.Contains(err.Error(), "skipped") {
		t.Errorf("expected error mentioning 'skipped', got: %v", err)
	}
}

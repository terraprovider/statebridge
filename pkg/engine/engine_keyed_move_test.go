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

package engine

import (
	"strings"
	"testing"

	tfjson "github.com/hashicorp/terraform-json"

	"github.com/terraprovider/statebridge/internal/testutil"
)

func TestEngine_ProcessFiles_ModuleMove(t *testing.T) {
	tests := []struct {
		name            string
		yaml            string
		stateResources  []*tfjson.StateResource
		dstImportCount  int
		dstContains     []string
		dstNotContains  []string
		srcRemovedCount int
		srcContains     []string
		srcNotContains  []string
	}{
		{
			name: "basic module move",
			yaml: `
description: "Move entire module.foo"
operations:
  - type: move
    source_layer: "SRC"
    destination_layer: "DST"
    resources:
      - from: "module.foo"
`,
			stateResources: []*tfjson.StateResource{
				testutil.NewResource("module.foo.aws_instance.web", "aws_instance", "web", nil,
					map[string]interface{}{"id": "i-123"}),
				testutil.NewResource("module.foo.aws_s3_bucket.data", "aws_s3_bucket", "data", nil,
					map[string]interface{}{"id": "bucket-123"}),
			},
			dstImportCount: 2,
			dstContains:    []string{"module.foo.aws_instance.web", "module.foo.aws_s3_bucket.data", `"i-123"`, `"bucket-123"`},
			srcContains:    []string{"module.foo"},
		},
		{
			name: "with destination address",
			yaml: `
description: "Move module.foo to module.bar"
operations:
  - type: move
    source_layer: "SRC"
    destination_layer: "DST"
    resources:
      - from: "module.foo"
        to: "module.bar"
`,
			stateResources: []*tfjson.StateResource{
				testutil.NewResource("module.foo.aws_instance.web", "aws_instance", "web", nil,
					map[string]interface{}{"id": "i-123"}),
			},
			dstContains:    []string{"module.bar.aws_instance.web"},
			dstNotContains: []string{"module.foo"},
		},
		{
			name: "with address prefix",
			yaml: `
description: "Module move with address prefix"
operations:
  - type: move
    source_layer: "SRC"
    destination_layer: "DST"
    address_prefix: "module.ig"
    resources:
      - from: "module.foo"
`,
			stateResources: []*tfjson.StateResource{
				testutil.NewResource("module.ig.module.foo.aws_instance.web", "aws_instance", "web", nil,
					map[string]interface{}{"id": "i-123"}),
			},
			dstContains: []string{"module.ig.module.foo.aws_instance.web"},
		},
		{
			name: "nested modules",
			yaml: `
description: "Move module with nested submodules"
operations:
  - type: move
    source_layer: "SRC"
    destination_layer: "DST"
    resources:
      - from: "module.foo"
`,
			stateResources: []*tfjson.StateResource{
				testutil.NewResource("module.foo.aws_instance.web", "aws_instance", "web", nil,
					map[string]interface{}{"id": "i-123"}),
				testutil.NewResource("module.foo.module.bar.aws_s3_bucket.logs", "aws_s3_bucket", "logs", nil,
					map[string]interface{}{"id": "bucket-456"}),
			},
			dstImportCount:  2,
			dstContains:     []string{"module.foo.aws_instance.web", "module.foo.module.bar.aws_s3_bucket.logs"},
			srcRemovedCount: 1,
			srcContains:     []string{"module.foo"},
		},
		{
			name: "for_each with prefix swap",
			yaml: `
description: "Move module with for_each resources"
operations:
  - type: move
    source_layer: "SRC"
    destination_layer: "DST"
    resources:
      - from: "module.foo"
        to: "module.bar"
`,
			stateResources: []*tfjson.StateResource{
				testutil.NewResource(`module.foo.aws_s3_bucket.data["key-a"]`, "aws_s3_bucket", "data", "key-a",
					map[string]interface{}{"id": "bucket-a"}),
				testutil.NewResource(`module.foo.aws_s3_bucket.data["key-b"]`, "aws_s3_bucket", "data", "key-b",
					map[string]interface{}{"id": "bucket-b"}),
				testutil.NewResource("module.foo.aws_instance.web", "aws_instance", "web", nil,
					map[string]interface{}{"id": "i-123"}),
			},
			dstImportCount: 3,
			dstContains:    []string{`module.bar.aws_s3_bucket.data["key-a"]`, `module.bar.aws_s3_bucket.data["key-b"]`, "module.bar.aws_instance.web"},
		},
		{
			name: "nested rename",
			yaml: `
description: "Move module.foo to module.bar with nested modules"
operations:
  - type: move
    source_layer: "SRC"
    destination_layer: "DST"
    resources:
      - from: "module.foo"
        to: "module.bar"
`,
			stateResources: []*tfjson.StateResource{
				testutil.NewResource("module.foo.aws_instance.web", "aws_instance", "web", nil,
					map[string]interface{}{"id": "i-123"}),
				testutil.NewResource("module.foo.module.baz.aws_s3_bucket.logs", "aws_s3_bucket", "logs", nil,
					map[string]interface{}{"id": "bucket-456"}),
			},
			dstContains:    []string{"module.bar.aws_instance.web", "module.bar.module.baz.aws_s3_bucket.logs"},
			dstNotContains: []string{"module.foo"},
			srcContains:    []string{"module.foo"},
		},
		{
			name: "indexed sub-module single instance",
			yaml: `
description: "Move module.foo containing an indexed sub-module instance"
operations:
  - type: move
    source_layer: "SRC"
    destination_layer: "DST"
    resources:
      - from: "module.foo"
        to: "module.bar"
`,
			stateResources: []*tfjson.StateResource{
				testutil.NewResource(`module.foo.module.sub[0].aws_s3_bucket.data["k1"]`, "aws_s3_bucket", "data", "k1",
					map[string]interface{}{"id": "b-k1"}),
				testutil.NewResource(`module.foo.module.sub[0].aws_s3_bucket.data["k2"]`, "aws_s3_bucket", "data", "k2",
					map[string]interface{}{"id": "b-k2"}),
			},
			dstImportCount: 2,
			// Import blocks keep the full indexed, keyed destination addresses.
			dstContains: []string{
				`module.bar.module.sub[0].aws_s3_bucket.data["k1"]`,
				`module.bar.module.sub[0].aws_s3_bucket.data["k2"]`,
			},
			// Removed block strips the module index (OpenTofu forbids it).
			srcRemovedCount: 1,
			srcContains:     []string{"from = module.foo.module.sub.aws_s3_bucket.data"},
			srcNotContains:  []string{"module.foo.module.sub[0]"},
		},
		{
			name: "indexed sub-module multiple instances deduplicated",
			yaml: `
description: "Move module.foo with a count=2 sub-module"
operations:
  - type: move
    source_layer: "SRC"
    destination_layer: "DST"
    resources:
      - from: "module.foo"
`,
			stateResources: []*tfjson.StateResource{
				testutil.NewResource(`module.foo.module.sub[0].aws_s3_bucket.data["k1"]`, "aws_s3_bucket", "data", "k1",
					map[string]interface{}{"id": "b0-k1"}),
				testutil.NewResource(`module.foo.module.sub[1].aws_s3_bucket.data["k1"]`, "aws_s3_bucket", "data", "k1",
					map[string]interface{}{"id": "b1-k1"}),
			},
			// Both module instances collapse to one config address → a single
			// deduplicated removed block, but both instances are re-imported.
			dstImportCount: 2,
			dstContains: []string{
				`module.foo.module.sub[0].aws_s3_bucket.data["k1"]`,
				`module.foo.module.sub[1].aws_s3_bucket.data["k1"]`,
			},
			srcRemovedCount: 1,
			srcContains:     []string{"from = module.foo.module.sub.aws_s3_bucket.data"},
			srcNotContains:  []string{"module.foo.module.sub[0]", "module.foo.module.sub[1]"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir, layers := testutil.SetupLayers(t, "src", "dst")
			srcLayer := layers["src"]
			dstLayer := layers["dst"]

			yaml := strings.ReplaceAll(tc.yaml, "SRC", srcLayer)
			yaml = strings.ReplaceAll(yaml, "DST", dstLayer)
			migrationFile := testutil.WriteMigration(t, dir, "001.yaml", yaml)

			mock := testutil.NewMockStateReader(map[string]*tfjson.State{
				srcLayer: testutil.BuildState(tc.stateResources...),
			})

			result := runEngine(t, Config{StateReader: mock}, []string{migrationFile})
			testutil.RequireOutputCount(t, result.OutputFiles, 2)

			if tc.dstImportCount > 0 || len(tc.dstContains) > 0 || len(tc.dstNotContains) > 0 {
				dstContent := testutil.ReadLayerFile(t, result.OutputFiles, dstLayer)
				if tc.dstImportCount > 0 {
					testutil.AssertBlockCount(t, dstContent, "import {", tc.dstImportCount)
				}
				for _, s := range tc.dstContains {
					testutil.AssertContains(t, dstContent, s)
				}
				for _, s := range tc.dstNotContains {
					testutil.AssertNotContains(t, dstContent, s)
				}
			}

			if tc.srcRemovedCount > 0 || len(tc.srcContains) > 0 {
				srcContent := testutil.ReadLayerFile(t, result.OutputFiles, srcLayer)
				if tc.srcRemovedCount > 0 {
					testutil.AssertBlockCount(t, srcContent, "removed {", tc.srcRemovedCount)
				}
				for _, s := range tc.srcContains {
					testutil.AssertContains(t, srcContent, s)
				}
				for _, s := range tc.srcNotContains {
					testutil.AssertNotContains(t, srcContent, s)
				}
			}
		})
	}
}

func TestEngine_ProcessFiles_ModuleMoveNoResources(t *testing.T) {
	dir, layers := testutil.SetupLayers(t, "old", "new")
	srcLayer := layers["old"]
	dstLayer := layers["new"]

	yaml := strings.ReplaceAll(`
description: "Move empty module"
operations:
  - type: move
    source_layer: "SRC"
    destination_layer: "DST"
    resources:
      - from: "module.empty"
`, "SRC", srcLayer)
	yaml = strings.ReplaceAll(yaml, "DST", dstLayer)
	migrationFile := testutil.WriteMigration(t, dir, "001.yaml", yaml)

	mock := testutil.NewMockStateReader(map[string]*tfjson.State{
		srcLayer: testutil.BuildState(),
	})

	result := runEngine(t, Config{StateReader: mock}, []string{migrationFile})
	assertAllSkippedWithError(t, result, migrationFile)
}

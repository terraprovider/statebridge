package engine

import (
	"strings"
	"testing"

	tfjson "github.com/hashicorp/terraform-json"

	"github.com/terraprovider/statebridge/internal/testutil"
)

func TestEngine_ProcessFiles_AllResources(t *testing.T) {
	tests := []struct {
		name            string
		yaml            string
		stateResources  []*tfjson.StateResource
		dstImportCount  int
		dstContains     []string
		dstNotContains  []string
		srcRemovedCount int
		srcContains     []string
	}{
		{
			name: "basic all resources",
			yaml: `
description: "Move all resources"
operations:
  - type: move
    source_layer: "SRC"
    destination_layer: "DST"
    all_resources: true
`,
			stateResources: []*tfjson.StateResource{
				testutil.NewResource("aws_instance.web", "aws_instance", "web", nil,
					map[string]interface{}{"id": "i-123"}),
				testutil.NewResource("aws_s3_bucket.data", "aws_s3_bucket", "data", nil,
					map[string]interface{}{"id": "bucket-123"}),
				testutil.NewResource("module.foo.aws_instance.api", "aws_instance", "api", nil,
					map[string]interface{}{"id": "i-456"}),
			},
			dstImportCount: 3,
			dstContains:    []string{"aws_instance.web", "aws_s3_bucket.data", "module.foo.aws_instance.api"},
		},
		{
			name: "with override rename",
			yaml: `
description: "Move all, rename one"
operations:
  - type: move
    source_layer: "SRC"
    destination_layer: "DST"
    all_resources: true
    overrides:
      - from: "aws_instance.web"
        to: "aws_instance.api"
`,
			stateResources: []*tfjson.StateResource{
				testutil.NewResource("aws_instance.web", "aws_instance", "web", nil,
					map[string]interface{}{"id": "i-123"}),
				testutil.NewResource("aws_s3_bucket.data", "aws_s3_bucket", "data", nil,
					map[string]interface{}{"id": "bucket-123"}),
			},
			dstContains:    []string{"aws_instance.api", "aws_s3_bucket.data"},
			dstNotContains: []string{"aws_instance.web"},
		},
		{
			name: "with for_each",
			yaml: `
description: "Move all with for_each"
operations:
  - type: move
    source_layer: "SRC"
    destination_layer: "DST"
    all_resources: true
`,
			stateResources: []*tfjson.StateResource{
				testutil.NewResource(`aws_s3_bucket.data["key-a"]`, "aws_s3_bucket", "data", "key-a",
					map[string]interface{}{"id": "bucket-a"}),
				testutil.NewResource(`aws_s3_bucket.data["key-b"]`, "aws_s3_bucket", "data", "key-b",
					map[string]interface{}{"id": "bucket-b"}),
			},
			dstImportCount:  2,
			dstContains:     []string{`aws_s3_bucket.data["key-a"]`, `aws_s3_bucket.data["key-b"]`},
			srcRemovedCount: 1,
		},
		{
			name: "module consolidation",
			yaml: `
description: "All resources with module consolidation"
operations:
  - type: move
    source_layer: "SRC"
    destination_layer: "DST"
    all_resources: true
`,
			stateResources: []*tfjson.StateResource{
				testutil.NewResource("aws_instance.standalone", "aws_instance", "standalone", nil,
					map[string]interface{}{"id": "i-000"}),
				testutil.NewResource("module.foo.aws_instance.web", "aws_instance", "web", nil,
					map[string]interface{}{"id": "i-123"}),
				testutil.NewResource("module.foo.aws_s3_bucket.data", "aws_s3_bucket", "data", nil,
					map[string]interface{}{"id": "bucket-123"}),
			},
			srcRemovedCount: 2,
			srcContains:     []string{"aws_instance.standalone", "module.foo"},
		},
		{
			name: "with omit",
			yaml: `
description: "Move all, omit one"
operations:
  - type: move
    source_layer: "SRC"
    destination_layer: "DST"
    all_resources: true
    omit:
      - address: "aws_instance.ephemeral"
`,
			stateResources: []*tfjson.StateResource{
				testutil.NewResource("aws_instance.web", "aws_instance", "web", nil,
					map[string]interface{}{"id": "i-123"}),
				testutil.NewResource("aws_s3_bucket.data", "aws_s3_bucket", "data", nil,
					map[string]interface{}{"id": "bucket-123"}),
				testutil.NewResource("aws_instance.ephemeral", "aws_instance", "ephemeral", nil,
					map[string]interface{}{"id": "i-eph"}),
			},
			dstImportCount:  2,
			dstContains:     []string{"aws_instance.web", "aws_s3_bucket.data"},
			dstNotContains:  []string{"aws_instance.ephemeral"},
			srcRemovedCount: 3,
			srcContains:     []string{"aws_instance.ephemeral"},
		},
		{
			name: "omit with destroy",
			yaml: `
description: "Move all, omit with destroy"
operations:
  - type: move
    source_layer: "SRC"
    destination_layer: "DST"
    all_resources: true
    omit:
      - address: "aws_instance.ephemeral"
        destroy: true
`,
			stateResources: []*tfjson.StateResource{
				testutil.NewResource("aws_instance.web", "aws_instance", "web", nil,
					map[string]interface{}{"id": "i-123"}),
				testutil.NewResource("aws_instance.ephemeral", "aws_instance", "ephemeral", nil,
					map[string]interface{}{"id": "i-eph"}),
			},
			dstImportCount: 1,
			dstNotContains: []string{"aws_instance.ephemeral"},
			srcContains:    []string{"destroy = true"},
		},
		{
			name: "omit and override",
			yaml: `
description: "Move all, omit one, rename one"
operations:
  - type: move
    source_layer: "SRC"
    destination_layer: "DST"
    all_resources: true
    overrides:
      - from: "aws_instance.web"
        to: "aws_instance.api"
    omit:
      - address: "aws_instance.ephemeral"
`,
			stateResources: []*tfjson.StateResource{
				testutil.NewResource("aws_instance.web", "aws_instance", "web", nil,
					map[string]interface{}{"id": "i-123"}),
				testutil.NewResource("aws_s3_bucket.data", "aws_s3_bucket", "data", nil,
					map[string]interface{}{"id": "bucket-123"}),
				testutil.NewResource("aws_instance.ephemeral", "aws_instance", "ephemeral", nil,
					map[string]interface{}{"id": "i-eph"}),
			},
			dstImportCount: 2,
			dstContains:    []string{"aws_instance.api", "aws_s3_bucket.data"},
			dstNotContains: []string{"aws_instance.web", "aws_instance.ephemeral"},
		},
		{
			name: "import ID override",
			yaml: `
description: "Move all, override import_id for one"
operations:
  - type: move
    source_layer: "SRC"
    destination_layer: "DST"
    all_resources: true
    overrides:
      - from: "azuredevops_serviceendpoint_azurerm.key_vault"
        import_id: "{{ .Attributes.project_id }}/{{ .Attributes.id }}"
`,
			stateResources: []*tfjson.StateResource{
				testutil.NewResource("aws_instance.web", "aws_instance", "web", nil,
					map[string]interface{}{"id": "i-web123"}),
				testutil.NewResource("azuredevops_serviceendpoint_azurerm.key_vault", "azuredevops_serviceendpoint_azurerm", "key_vault", nil,
					map[string]interface{}{"id": "endpoint-id", "project_id": "proj-123"}),
			},
			dstImportCount: 2,
			dstContains:    []string{`id = "proj-123/endpoint-id"`, `id = "i-web123"`},
		},
		{
			name: "import ID and destination override",
			yaml: `
description: "Move all, rename and override import_id"
operations:
  - type: move
    source_layer: "SRC"
    destination_layer: "DST"
    all_resources: true
    overrides:
      - from: "azuredevops_serviceendpoint_azurerm.key_vault"
        to: "azuredevops_serviceendpoint_azurerm.kv"
        import_id: "{{ .Attributes.project_id }}/{{ .Attributes.id }}"
`,
			stateResources: []*tfjson.StateResource{
				testutil.NewResource("aws_instance.web", "aws_instance", "web", nil,
					map[string]interface{}{"id": "i-web123"}),
				testutil.NewResource("azuredevops_serviceendpoint_azurerm.key_vault", "azuredevops_serviceendpoint_azurerm", "key_vault", nil,
					map[string]interface{}{"id": "endpoint-id", "project_id": "proj-123"}),
			},
			dstContains:    []string{"azuredevops_serviceendpoint_azurerm.kv", `id = "proj-123/endpoint-id"`},
			dstNotContains: []string{"azuredevops_serviceendpoint_azurerm.key_vault"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir, layers := testutil.SetupLayers(t, "old", "new")
			srcLayer := layers["old"]
			dstLayer := layers["new"]

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
			}
		})
	}
}

func TestEngine_ProcessFiles_AllResourcesEmptyState(t *testing.T) {
	dir, layers := testutil.SetupLayers(t, "old", "new")
	srcLayer := layers["old"]
	dstLayer := layers["new"]

	yaml := strings.ReplaceAll(`
description: "Move empty layer"
operations:
  - type: move
    source_layer: "SRC"
    destination_layer: "DST"
    all_resources: true
`, "SRC", srcLayer)
	yaml = strings.ReplaceAll(yaml, "DST", dstLayer)
	migrationFile := testutil.WriteMigration(t, dir, "001.yaml", yaml)

	mock := testutil.NewMockStateReader(map[string]*tfjson.State{
		srcLayer: testutil.BuildState(),
	})

	err := runEngineExpectError(t, Config{StateReader: mock}, []string{migrationFile})
	if !strings.Contains(err.Error(), "skipped") {
		t.Errorf("expected error mentioning skipped, got: %v", err)
	}
}

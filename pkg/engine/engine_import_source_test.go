package engine

import (
	"strings"
	"testing"

	tfjson "github.com/hashicorp/terraform-json"
	"github.com/terraprovider/statebridge/internal/testutil"
)

// TestEngine_ProcessFiles_ImportFromSource tests source-based imports
// where import IDs are derived from another resource's state attributes.
func TestEngine_ProcessFiles_ImportFromSource(t *testing.T) {
	tests := []struct {
		name             string
		yaml             string
		states           map[string]*tfjson.State
		importBlockCount int
		expectedContains []string
		notContains      []string
	}{
		{
			name: "source without expand - single instance",
			yaml: `
description: "Import from source (single instance)"
operations:
  - type: import
    layer: "LAYER_APP"
    imports:
      - address: "azuread_application_registration.main"
        id: '{{ .Attributes.id }}'
        source:
          layer: "LAYER_APP"
          address: "azuread_application.main"
`,
			states: map[string]*tfjson.State{
				"LAYER_APP": testutil.BuildState(
					testutil.NewResource(
						"azuread_application.main",
						"azuread_application", "main", nil,
						map[string]interface{}{
							"id": "app-guid-123",
						},
					),
				),
			},
			importBlockCount: 1,
			expectedContains: []string{
				"import {",
				"azuread_application_registration.main",
				`"app-guid-123"`,
			},
		},
		{
			name: "source without expand - for_each instances",
			yaml: `
description: "Import from source (for_each)"
operations:
  - type: import
    layer: "LAYER_APP"
    imports:
      - address: "azuread_application_registration.all"
        id: '{{ .Attributes.id }}'
        source:
          layer: "LAYER_APP"
          address: "azuread_application.all"
`,
			states: map[string]*tfjson.State{
				"LAYER_APP": testutil.BuildState(
					testutil.NewResource(
						`azuread_application.all["app1"]`,
						"azuread_application", "all", "app1",
						map[string]interface{}{"id": "guid-1"},
					),
					testutil.NewResource(
						`azuread_application.all["app2"]`,
						"azuread_application", "all", "app2",
						map[string]interface{}{"id": "guid-2"},
					),
				),
			},
			importBlockCount: 2,
			expectedContains: []string{
				`azuread_application_registration.all["app1"]`,
				`azuread_application_registration.all["app2"]`,
				`"guid-1"`,
				`"guid-2"`,
			},
		},
		{
			name: "source with expand - single instance, two list items",
			yaml: `
description: "Import with expand"
operations:
  - type: import
    layer: "LAYER_APP"
    imports:
      - address: "azuread_api_access.main"
        id: '{{ .Attributes.id }}/apiAccess/{{ .Item.resource_app_id }}'
        key: '{{ .Item.resource_app_id }}'
        source:
          layer: "LAYER_APP"
          address: "azuread_application.main"
          expand: "required_resource_access"
`,
			states: map[string]*tfjson.State{
				"LAYER_APP": testutil.BuildState(
					testutil.NewResource(
						"azuread_application.main",
						"azuread_application", "main", nil,
						map[string]interface{}{
							"id": "app-123",
							"required_resource_access": []interface{}{
								map[string]interface{}{
									"resource_app_id": "graph-api-id",
								},
								map[string]interface{}{
									"resource_app_id": "sharepoint-api-id",
								},
							},
						},
					),
				),
			},
			importBlockCount: 2,
			expectedContains: []string{
				`azuread_api_access.main["graph-api-id"]`,
				`azuread_api_access.main["sharepoint-api-id"]`,
				`"app-123/apiAccess/graph-api-id"`,
				`"app-123/apiAccess/sharepoint-api-id"`,
			},
		},
		{
			name: "source with expand - for_each source, multiple list items",
			yaml: `
description: "Import expand with for_each source"
operations:
  - type: import
    layer: "LAYER_APP"
    imports:
      - address: "azuread_api_access.all"
        id: '{{ .Attributes.id }}/apiAccess/{{ .Item.resource_app_id }}'
        key: '{{ .Key }}_{{ .Item.resource_app_id }}'
        source:
          layer: "LAYER_APP"
          address: "azuread_application.all"
          expand: "required_resource_access"
`,
			states: map[string]*tfjson.State{
				"LAYER_APP": testutil.BuildState(
					testutil.NewResource(
						`azuread_application.all["app1"]`,
						"azuread_application", "all", "app1",
						map[string]interface{}{
							"id": "guid-app1",
							"required_resource_access": []interface{}{
								map[string]interface{}{"resource_app_id": "api-a"},
								map[string]interface{}{"resource_app_id": "api-b"},
							},
						},
					),
					testutil.NewResource(
						`azuread_application.all["app2"]`,
						"azuread_application", "all", "app2",
						map[string]interface{}{
							"id": "guid-app2",
							"required_resource_access": []interface{}{
								map[string]interface{}{"resource_app_id": "api-c"},
							},
						},
					),
				),
			},
			importBlockCount: 3, // app1 has 2 items, app2 has 1
			expectedContains: []string{
				`azuread_api_access.all["app1_api-a"]`,
				`azuread_api_access.all["app1_api-b"]`,
				`azuread_api_access.all["app2_api-c"]`,
				`"guid-app1/apiAccess/api-a"`,
				`"guid-app1/apiAccess/api-b"`,
				`"guid-app2/apiAccess/api-c"`,
			},
		},
		{
			name: "source with expand - empty list produces zero imports",
			yaml: `
description: "Import expand with empty list"
operations:
  - type: import
    layer: "LAYER_APP"
    imports:
      - address: "azuread_api_access.main"
        id: '{{ .Attributes.id }}/{{ .Item.x }}'
        key: '{{ .Item.x }}'
        source:
          layer: "LAYER_APP"
          address: "azuread_application.main"
          expand: "required_resource_access"
`,
			states: map[string]*tfjson.State{
				"LAYER_APP": testutil.BuildState(
					testutil.NewResource(
						"azuread_application.main",
						"azuread_application", "main", nil,
						map[string]interface{}{
							"id":                       "app-empty",
							"required_resource_access": []interface{}{},
						},
					),
				),
			},
			importBlockCount: 0,
			notContains:      []string{"import {"},
		},
		{
			name: "source with custom key template (no expand)",
			yaml: `
description: "Source with custom key"
operations:
  - type: import
    layer: "LAYER_APP"
    imports:
      - address: "azuread_application_registration.all"
        id: '{{ .Attributes.id }}'
        key: '{{ .Key | replace "-" "_" }}'
        source:
          layer: "LAYER_APP"
          address: "azuread_application.all"
`,
			states: map[string]*tfjson.State{
				"LAYER_APP": testutil.BuildState(
					testutil.NewResource(
						`azuread_application.all["my-app"]`,
						"azuread_application", "all", "my-app",
						map[string]interface{}{"id": "guid-myapp"},
					),
				),
			},
			importBlockCount: 1,
			expectedContains: []string{
				`azuread_application_registration.all["my_app"]`,
				`"guid-myapp"`,
			},
			notContains: []string{`["my-app"]`},
		},
		{
			name: "mixed static and source imports in one operation",
			yaml: `
description: "Mixed imports"
operations:
  - type: import
    layer: "LAYER_APP"
    imports:
      - address: "aws_instance.web"
        id: "i-12345"
      - address: "azuread_application_registration.all"
        id: '{{ .Attributes.id }}'
        source:
          layer: "LAYER_APP"
          address: "azuread_application.all"
`,
			states: map[string]*tfjson.State{
				"LAYER_APP": testutil.BuildState(
					testutil.NewResource(
						`azuread_application.all["app1"]`,
						"azuread_application", "all", "app1",
						map[string]interface{}{"id": "guid-1"},
					),
				),
			},
			importBlockCount: 2,
			expectedContains: []string{
				"aws_instance.web",
				`"i-12345"`,
				`azuread_application_registration.all["app1"]`,
				`"guid-1"`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, layers := testutil.SetupLayers(t, "app")
			layerDir := layers["app"]

			yaml := strings.ReplaceAll(tt.yaml, "LAYER_APP", layerDir)

			// Replace layer paths in states map.
			resolvedStates := make(map[string]*tfjson.State)
			for k, v := range tt.states {
				key := strings.ReplaceAll(k, "LAYER_APP", layerDir)
				resolvedStates[key] = v
			}

			dir := t.TempDir()
			migrationFile := testutil.WriteMigration(t, dir, "001_import.yaml", yaml)

			cfg := Config{StateReader: testutil.NewMockStateReader(resolvedStates)}
			result := runEngine(t, cfg, []string{migrationFile})

			if tt.importBlockCount == 0 {
				// Expect no output files when all expansions are empty.
				if len(result.OutputFiles) > 0 {
					content := testutil.ReadFirstOutput(t, result.OutputFiles)
					for _, s := range tt.notContains {
						testutil.AssertNotContains(t, content, s)
					}
				}
				return
			}

			testutil.RequireOutputCount(t, result.OutputFiles, 1)
			content := testutil.ReadFirstOutput(t, result.OutputFiles)

			testutil.AssertBlockCount(t, content, "import {", tt.importBlockCount)
			for _, s := range tt.expectedContains {
				testutil.AssertContains(t, content, s)
			}
			for _, s := range tt.notContains {
				testutil.AssertNotContains(t, content, s)
			}
		})
	}
}

// TestEngine_ProcessFiles_ImportFromSource_NonListExpand tests that expanding
// a non-list attribute produces a skip (resilient processing).
func TestEngine_ProcessFiles_ImportFromSource_NonListExpand(t *testing.T) {
	_, layers := testutil.SetupLayers(t, "app")
	layerDir := layers["app"]

	yaml := strings.ReplaceAll(`
description: "Expand non-list attribute"
operations:
  - type: import
    layer: "LAYER_APP"
    imports:
      - address: "azuread_api_access.main"
        id: '{{ .Item.x }}'
        key: '{{ .Item.x }}'
        source:
          layer: "LAYER_APP"
          address: "azuread_application.main"
          expand: "display_name"
`, "LAYER_APP", layerDir)

	dir := t.TempDir()
	migrationFile := testutil.WriteMigration(t, dir, "001_import.yaml", yaml)

	states := map[string]*tfjson.State{
		layerDir: testutil.BuildState(
			testutil.NewResource(
				"azuread_application.main",
				"azuread_application", "main", nil,
				map[string]interface{}{
					"id":           "app-123",
					"display_name": "My App", // string, not a list
				},
			),
		),
	}

	cfg := Config{StateReader: testutil.NewMockStateReader(states)}
	result := runEngine(t, cfg, []string{migrationFile})

	// The processing error is reported as a per-file skip (resilient
	// processing), not a fatal error: the whole run still succeeds with a
	// warning printed to stderr since no output was generated. The original
	// "not a list" message is printed to stderr as well.
	assertAllSkippedWithError(t, result, migrationFile)
}

// TestEngine_ProcessFiles_ImportFromSource_MissingExpandAttribute tests that
// expanding a missing attribute produces a skip (resilient processing).
func TestEngine_ProcessFiles_ImportFromSource_MissingExpandAttribute(t *testing.T) {
	_, layers := testutil.SetupLayers(t, "app")
	layerDir := layers["app"]

	yaml := strings.ReplaceAll(`
description: "Expand missing attribute"
operations:
  - type: import
    layer: "LAYER_APP"
    imports:
      - address: "azuread_api_access.main"
        id: '{{ .Item.x }}'
        key: '{{ .Item.x }}'
        source:
          layer: "LAYER_APP"
          address: "azuread_application.main"
          expand: "nonexistent_attribute"
`, "LAYER_APP", layerDir)

	dir := t.TempDir()
	migrationFile := testutil.WriteMigration(t, dir, "001_import.yaml", yaml)

	states := map[string]*tfjson.State{
		layerDir: testutil.BuildState(
			testutil.NewResource(
				"azuread_application.main",
				"azuread_application", "main", nil,
				map[string]interface{}{
					"id": "app-123",
				},
			),
		),
	}

	cfg := Config{StateReader: testutil.NewMockStateReader(states)}
	result := runEngine(t, cfg, []string{migrationFile})

	// The processing error is reported as a per-file skip (resilient
	// processing), not a fatal error: the whole run still succeeds with a
	// warning printed to stderr since no output was generated. The original
	// "no attribute" message is printed to stderr as well.
	assertAllSkippedWithError(t, result, migrationFile)
}

// TestEngine_ProcessFiles_ImportFromSource_WithAddressPrefix tests that
// address_prefix is applied correctly to source-based imports.
func TestEngine_ProcessFiles_ImportFromSource_WithAddressPrefix(t *testing.T) {
	_, layers := testutil.SetupLayers(t, "app")
	layerDir := layers["app"]

	yaml := strings.ReplaceAll(`
description: "Import with address prefix"
operations:
  - type: import
    layer: "LAYER_APP"
    address_prefix: "module.identity"
    imports:
      - address: "azuread_application_registration.all"
        id: '{{ .Attributes.id }}'
        source:
          layer: "LAYER_APP"
          address: "azuread_application.all"
`, "LAYER_APP", layerDir)

	dir := t.TempDir()
	migrationFile := testutil.WriteMigration(t, dir, "001_import.yaml", yaml)

	states := map[string]*tfjson.State{
		layerDir: testutil.BuildState(
			testutil.NewResource(
				`azuread_application.all["app1"]`,
				"azuread_application", "all", "app1",
				map[string]interface{}{"id": "guid-1"},
			),
		),
	}

	cfg := Config{StateReader: testutil.NewMockStateReader(states)}
	result := runEngine(t, cfg, []string{migrationFile})

	testutil.RequireOutputCount(t, result.OutputFiles, 1)
	content := testutil.ReadFirstOutput(t, result.OutputFiles)

	testutil.AssertContains(t, content, `module.identity.azuread_application_registration.all["app1"]`)
	testutil.AssertContains(t, content, `"guid-1"`)
}

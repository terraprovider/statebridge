//go:build e2e_fast

package e2e

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Table-driven move tests
// ---------------------------------------------------------------------------

// layerAssertion pairs a layer name (relative to rootDir/layers/) with a
// terraform resource address for state assertions.
type layerAssertion struct {
	layerName string // "shared", "app", "networking"
	addr      string // e.g. "random_id.moved"
}

// fileAssertion asserts that a generated file in the given layer contains a
// substring.
type fileAssertion struct {
	layerName string
	substr    string
}

// moveTestCase defines a parameterised 2-layer move test.
type moveTestCase struct {
	name string

	// destLayerName is the destination layer directory name under rootDir/layers/
	// (e.g. "app" or "networking"). Source is always "shared".
	destLayerName string

	// sourceTF is the resource definitions appended after randomProviderHCL for
	// the source layer.
	sourceTF string

	// sourcePostMigrateTF is set on the source layer after writing the
	// migration. If empty, the source is set to just randomProviderHCL (no
	// resources).
	sourcePostMigrateTF string

	// destTF is the resource definitions appended after randomProviderHCL for
	// the destination layer.
	destTF string

	// migrationYAMLTemplate is the YAML with two %s placeholders: first for
	// sourceDir, second for destDir.
	migrationYAMLTemplate string

	// preAssertExist are checked before the migration runs.
	preAssertExist []layerAssertion

	// postAssertExist are checked after the migration is applied.
	postAssertExist []layerAssertion

	// postAssertNotExist are checked after the migration is applied.
	postAssertNotExist []layerAssertion

	// extraFileAssertions are checked on the generated files (optional).
	extraFileAssertions []fileAssertion
}

// runMoveTest executes the standard 2-layer move test skeleton.
func runMoveTest(t *testing.T, tc moveTestCase) {
	t.Helper()
	t.Parallel()

	rootDir, _, vars := setupFastProject(t)

	sourceDir := filepath.Join(rootDir, "layers", "shared")
	destDir := filepath.Join(rootDir, "layers", tc.destLayerName)

	// Setup source layer
	updateTfFile(t, sourceDir, "main.tf", randomProviderHCL+tc.sourceTF)
	tofuInit(t, sourceDir)
	tofuApply(t, sourceDir, vars)
	t.Cleanup(func() {
		tofuDestroy(t, destDir, vars)
		tofuDestroy(t, sourceDir, vars)
	})

	// Pre-assertions
	for _, a := range tc.preAssertExist {
		dir := filepath.Join(rootDir, "layers", a.layerName)
		assertResourceInState(t, dir, a.addr)
	}

	// Write migration
	yaml := fmt.Sprintf(tc.migrationYAMLTemplate, sourceDir, destDir)
	migDir := writeMigration(t, rootDir, "001_move.yaml", yaml)

	// Update destination layer
	updateTfFile(t, destDir, "main.tf", randomProviderHCL+tc.destTF)

	// Update source layer (remove moved resources)
	sourcePost := tc.sourcePostMigrateTF
	if sourcePost == "" {
		sourcePost = randomProviderHCL
	}
	updateTfFile(t, sourceDir, "main.tf", sourcePost)

	// Generate
	files := requireGenerate(t, migDir)

	// Extra file assertions (e.g. checking generated HCL content)
	for _, fa := range tc.extraFileAssertions {
		layerDir := filepath.Join(rootDir, "layers", fa.layerName)
		var matched string
		for _, f := range files {
			if filepath.Dir(f) == layerDir {
				matched = f
				break
			}
		}
		if matched == "" {
			t.Fatalf("expected a generated migration file in %s layer", fa.layerName)
		}
		assertFileContains(t, matched, fa.substr)
	}

	// Apply migration
	tofuInit(t, destDir)
	tofuApply(t, destDir, vars)
	tofuApply(t, sourceDir, vars)

	// Post-assertions
	for _, a := range tc.postAssertExist {
		dir := filepath.Join(rootDir, "layers", a.layerName)
		assertResourceInState(t, dir, a.addr)
	}
	for _, a := range tc.postAssertNotExist {
		dir := filepath.Join(rootDir, "layers", a.layerName)
		assertResourceNotInState(t, dir, a.addr)
	}

	// Cleanup & verify
	cleanupAndAssertClean(t, vars, sourceDir, destDir)
}

// forEachAssertions builds layerAssertion slices for keyed resources.
func forEachAssertions(layerName, resourceBase string, keys []string) []layerAssertion {
	out := make([]layerAssertion, len(keys))
	for i, k := range keys {
		out[i] = layerAssertion{layerName: layerName, addr: fmt.Sprintf(`%s["%s"]`, resourceBase, k)}
	}
	return out
}

func TestE2EFast_MoveOperations(t *testing.T) {
	tests := []moveTestCase{
		{
			name:                "SimpleMove",
			destLayerName:       "networking",
			sourceTF:            randomIDResource("moved") + randomIDResource("importable"),
			sourcePostMigrateTF: randomProviderHCL + randomIDResource("importable"),
			destTF:              randomIDResource("moved"),
			migrationYAMLTemplate: `
description: "Move random_id from shared to networking"
operations:
  - type: move
    source_layer: "%s"
    destination_layer: "%s"
    resources:
      - from: "random_id.moved"
`,
			preAssertExist: []layerAssertion{
				{layerName: "shared", addr: "random_id.moved"},
			},
			postAssertExist: []layerAssertion{
				{layerName: "networking", addr: "random_id.moved"},
			},
			postAssertNotExist: []layerAssertion{
				{layerName: "shared", addr: "random_id.moved"},
			},
		},
		{
			name:          "ModuleMove",
			destLayerName: "app",
			sourceTF:      moduleBlock("mod_null", "module"),
			destTF:        moduleBlock("mod_null", "module"),
			migrationYAMLTemplate: `
description: "Move random module from shared to app"
operations:
  - type: move
    source_layer: "%s"
    destination_layer: "%s"
    resources:
      - from: "module.mod_null"
`,
			preAssertExist: []layerAssertion{
				{layerName: "shared", addr: "module.mod_null.random_id.unit"},
			},
			postAssertExist: []layerAssertion{
				{layerName: "app", addr: "module.mod_null.random_id.unit"},
			},
			postAssertNotExist: []layerAssertion{
				{layerName: "shared", addr: "module.mod_null.random_id.unit"},
			},
			extraFileAssertions: []fileAssertion{
				{layerName: "shared", substr: "from = module.mod_null"},
			},
		},
		{
			name:          "KeyPatternMove",
			destLayerName: "app",
			sourceTF:      randomIDForEachResource("nsgs", []string{"app_alpha", "app_beta", "core_gamma"}),
			destTF: `
resource "random_id" "nsgs" {
  for_each = {
    alpha      = "app_alpha"
    beta       = "app_beta"
    core_gamma = "core_gamma"
  }
  byte_length = 4
  keepers = {
    prefix = var.prefix
    key    = each.value
  }
}
`,
			migrationYAMLTemplate: `
description: "Move random resources with key patterns"
operations:
  - type: move
    source_layer: "%s"
    destination_layer: "%s"
    resources:
      - from: "random_id.nsgs"
        keys:
          "app_*": '{{ .Key | trimPrefix "app_" }}'
          "*": '{{ .Key }}'
`,
			postAssertExist: forEachAssertions("app", "random_id.nsgs", []string{"alpha", "beta", "core_gamma"}),
		},
		{
			name:          "KeyedMove",
			destLayerName: "app",
			sourceTF:      randomIDForEachResource("items", []string{"alpha", "beta", "gamma"}),
			destTF:        randomIDForEachResource("items", []string{"app_alpha", "app_beta", "app_gamma"}),
			migrationYAMLTemplate: `
description: "Move random_id items with key remapping"
operations:
  - type: move
    source_layer: "%s"
    destination_layer: "%s"
    resources:
      - from: "random_id.items"
        keys:
          alpha: app_alpha
          beta: app_beta
          gamma: app_gamma
`,
			preAssertExist:  forEachAssertions("shared", "random_id.items", []string{"alpha", "beta", "gamma"}),
			postAssertExist: forEachAssertions("app", "random_id.items", []string{"app_alpha", "app_beta", "app_gamma"}),
		},
		{
			name:          "MoveWithAddressRename",
			destLayerName: "app",
			sourceTF: `
resource "random_id" "old_name" {
  byte_length = 4
  keepers = {
    prefix = var.prefix
    name   = "renamed"
  }
}
`,
			destTF: `
resource "random_id" "new_name" {
  byte_length = 4
  keepers = {
    prefix = var.prefix
    name   = "renamed"
  }
}
`,
			migrationYAMLTemplate: `
description: "Move random_id and rename it"
operations:
  - type: move
    source_layer: "%s"
    destination_layer: "%s"
    resources:
      - from: "random_id.old_name"
        to: "random_id.new_name"
`,
			preAssertExist: []layerAssertion{
				{layerName: "shared", addr: "random_id.old_name"},
			},
			postAssertExist: []layerAssertion{
				{layerName: "app", addr: "random_id.new_name"},
			},
			postAssertNotExist: []layerAssertion{
				{layerName: "shared", addr: "random_id.old_name"},
			},
		},
		{
			name:          "AddressPrefix",
			destLayerName: "app",
			sourceTF:      moduleBlock("wrapper", "prefixed"),
			destTF:        moduleBlock("wrapper", "prefixed"),
			migrationYAMLTemplate: `
description: "Move resource using address_prefix"
operations:
  - type: move
    source_layer: "%s"
    destination_layer: "%s"
    address_prefix: "module.wrapper"
    resources:
      - from: "random_id.unit"
`,
			preAssertExist: []layerAssertion{
				{layerName: "shared", addr: "module.wrapper.random_id.unit"},
			},
			postAssertExist: []layerAssertion{
				{layerName: "app", addr: "module.wrapper.random_id.unit"},
			},
			postAssertNotExist: []layerAssertion{
				{layerName: "shared", addr: "module.wrapper.random_id.unit"},
			},
		},
		{
			name:          "MoveForEachWithoutKeys",
			destLayerName: "app",
			sourceTF:      randomIDForEachResource("things", []string{"x", "y", "z"}),
			destTF:        randomIDForEachResource("things", []string{"x", "y", "z"}),
			migrationYAMLTemplate: `
description: "Move all for_each instances without key mapping"
operations:
  - type: move
    source_layer: "%s"
    destination_layer: "%s"
    resources:
      - from: "random_id.things"
`,
			postAssertExist:    forEachAssertions("app", "random_id.things", []string{"x", "y", "z"}),
			postAssertNotExist: forEachAssertions("shared", "random_id.things", []string{"x", "y", "z"}),
		},
		{
			name:          "ImportWithTemplateID",
			destLayerName: "app",
			sourceTF:      randomIDResource("composite"),
			destTF:        randomIDResource("composite"),
			migrationYAMLTemplate: `
description: "Move with template import_id"
operations:
  - type: move
    source_layer: "%s"
    destination_layer: "%s"
    resources:
      - from: "random_id.composite"
        import_id: '{{ .Attributes.id }}'
`,
			preAssertExist: []layerAssertion{
				{layerName: "shared", addr: "random_id.composite"},
			},
			postAssertExist: []layerAssertion{
				{layerName: "app", addr: "random_id.composite"},
			},
			postAssertNotExist: []layerAssertion{
				{layerName: "shared", addr: "random_id.composite"},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			runMoveTest(t, tc)
		})
	}
}

// ---------------------------------------------------------------------------
// Standalone move tests (structurally different from the 2-layer pattern)
// ---------------------------------------------------------------------------

func TestE2EFast_AllResourcesMoveWithOverridesOmit(t *testing.T) {
	t.Parallel()
	rootDir, _, vars := setupFastProject(t)

	sharedDir := filepath.Join(rootDir, "layers", "shared")
	networkingDir := filepath.Join(rootDir, "layers", "networking")

	updateTfFile(t, sharedDir, "main.tf", `
terraform {
  required_providers {
    random = {
      source = "hashicorp/random"
    }
  }
}

provider "random" {}

resource "random_id" "moved" {
  byte_length = 4
  keepers = {
    prefix = var.prefix
    name   = "moved"
  }
}

resource "random_id" "importable" {
  byte_length = 4
  keepers = {
    prefix = var.prefix
    name   = "importable"
  }
}
`)

	tofuInit(t, sharedDir)
	tofuApply(t, sharedDir, vars)
	t.Cleanup(func() {
		tofuDestroy(t, networkingDir, vars)
		tofuDestroy(t, sharedDir, vars)
	})

	assertResourceInState(t, sharedDir, "random_id.moved")

	migDir := writeMigration(t, rootDir, "001_move_random_id.yaml", fmt.Sprintf(`
description: "Move random_id from shared to networking"
operations:
  - type: move
    source_layer: "%s"
    destination_layer: "%s"
    resources:
      - from: "random_id.moved"
`, sharedDir, networkingDir))

	updateTfFile(t, networkingDir, "main.tf", `
terraform {
  required_providers {
    random = {
      source = "hashicorp/random"
    }
  }
}

provider "random" {}

resource "random_id" "moved" {
  byte_length = 4
  keepers = {
    prefix = var.prefix
    name   = "moved"
  }
}
`)

	updateTfFile(t, sharedDir, "main.tf", `
terraform {
  required_providers {
    random = {
      source = "hashicorp/random"
    }
  }
}

provider "random" {}

resource "random_id" "importable" {
  byte_length = 4
  keepers = {
    prefix = var.prefix
    name   = "importable"
  }
}
`)

	files := runGenerate(t, []string{migDir})
	if len(files) == 0 {
		t.Fatal("expected generated migration files, got none")
	}

	tofuInit(t, networkingDir)
	tofuApply(t, networkingDir, vars)
	tofuApply(t, sharedDir, vars)

	assertResourceInState(t, networkingDir, "random_id.moved")
	assertResourceNotInState(t, sharedDir, "random_id.moved")

	cleanupMigrationFiles(t, sharedDir)
	cleanupMigrationFiles(t, networkingDir)
	assertCleanPlan(t, sharedDir, vars)
	assertCleanPlan(t, networkingDir, vars)
}

// TestE2EFast_IndexedModuleForEachMove verifies moving every keyed instance of a
// for_each resource that lives inside an indexed module instance
// (module.configuration_policies[0].random_id.items) to another layer.
//
// This exercises the address handling for indexed modules end-to-end and, in
// particular, that the generated source-layer removed block strips the module
// instance index (module.configuration_policies.random_id.items) — the only
// form OpenTofu accepts — while the destination import blocks keep the full
// indexed, keyed addresses.
func TestE2EFast_IndexedModuleForEachMove(t *testing.T) {
	t.Parallel()
	rootDir, _, vars := setupFastProject(t)

	sharedDir := filepath.Join(rootDir, "layers", "shared")
	networkingDir := filepath.Join(rootDir, "layers", "networking")

	// Source layer: indexed module instance with a for_each resource + a sibling
	// (random_id.keep) that stays behind.
	updateTfFile(t, sharedDir, "main.tf",
		randomProviderHCL+indexedForeachModuleBlock("foreachmod", 1, []string{"cfg_a", "cfg_b"}))
	tofuInit(t, sharedDir)
	tofuApply(t, sharedDir, vars)
	t.Cleanup(func() {
		tofuDestroy(t, networkingDir, vars)
		tofuDestroy(t, sharedDir, vars)
	})

	assertResourceInState(t, sharedDir, `module.configuration_policies[0].random_id.items["cfg_a"]`)
	assertResourceInState(t, sharedDir, `module.configuration_policies[0].random_id.items["cfg_b"]`)

	migDir := writeMigration(t, rootDir, "001_indexed_module_move.yaml", fmt.Sprintf(`
description: "Move all keys of a for_each resource out of an indexed module"
operations:
  - type: move
    source_layer: "%s"
    destination_layer: "%s"
    resources:
      - from: "module.configuration_policies[0].random_id.items"
`, sharedDir, networkingDir))

	// Generate while the source still uses the original module, so statebridge
	// can read source state to resolve import IDs and for_each keys.
	files := runGenerate(t, []string{migDir})
	if len(files) == 0 {
		t.Fatal("expected generated migration files, got none")
	}

	// The source removed block must use the module-index-stripped config address.
	sharedFile := generatedFileInLayer(t, files, sharedDir)
	assertFileContains(t, sharedFile, "from = module.configuration_policies.random_id.items")

	// The destination import blocks keep the full indexed, keyed addresses.
	networkingFile := generatedFileInLayer(t, files, networkingDir)
	assertFileContains(t, networkingFile, `to = module.configuration_policies[0].random_id.items["cfg_a"]`)
	assertFileContains(t, networkingFile, `to = module.configuration_policies[0].random_id.items["cfg_b"]`)

	// Destination layer declares the same indexed module so the imported
	// instances have a matching configuration to bind to.
	updateTfFile(t, networkingDir, "main.tf",
		randomProviderHCL+indexedForeachModuleBlock("foreachmod", 1, []string{"cfg_a", "cfg_b"}))

	// Source layer switches to the "after" module variant, which no longer
	// declares random_id.items (required for a valid removed block).
	updateTfFile(t, sharedDir, "main.tf",
		randomProviderHCL+indexedForeachModuleBlock("foreachmod_after", 1, nil))

	// Apply the import in the destination, then the removal in the source. Both
	// need a re-init because their module configuration changed.
	tofuInit(t, networkingDir)
	tofuApply(t, networkingDir, vars)
	tofuInit(t, sharedDir)
	tofuApply(t, sharedDir, vars)

	assertResourceInState(t, networkingDir, `module.configuration_policies[0].random_id.items["cfg_a"]`)
	assertResourceInState(t, networkingDir, `module.configuration_policies[0].random_id.items["cfg_b"]`)
	assertResourceNotInState(t, sharedDir, `module.configuration_policies[0].random_id.items["cfg_a"]`)
	assertResourceNotInState(t, sharedDir, `module.configuration_policies[0].random_id.items["cfg_b"]`)
	// The sibling stays behind in the source layer.
	assertResourceInState(t, sharedDir, "module.configuration_policies[0].random_id.keep")

	cleanupMigrationFiles(t, sharedDir)
	cleanupMigrationFiles(t, networkingDir)
	assertCleanPlan(t, sharedDir, vars)
	assertCleanPlan(t, networkingDir, vars)
}

// TestE2EFast_IndexedModuleMultiInstanceMoveRejected verifies that statebridge
// refuses, at generate time, a cross-layer move out of one instance of a
// multi-instance (count = 2) module. The generated source removed block would
// otherwise forget the resource from every module instance while the import only
// re-creates the referenced one. This exercises the guard against REAL OpenTofu
// state (confirming a count = 2 module yields module.configuration_policies[0]
// and [1] addresses that the guard detects).
func TestE2EFast_IndexedModuleMultiInstanceMoveRejected(t *testing.T) {
	t.Parallel()
	rootDir, _, vars := setupFastProject(t)

	sharedDir := filepath.Join(rootDir, "layers", "shared")
	networkingDir := filepath.Join(rootDir, "layers", "networking")

	// count = 2 => module.configuration_policies[0] and [1], each with items.
	updateTfFile(t, sharedDir, "main.tf",
		randomProviderHCL+indexedForeachModuleBlock("foreachmod", 2, []string{"cfg_a", "cfg_b"}))
	tofuInit(t, sharedDir)
	tofuApply(t, sharedDir, vars)
	t.Cleanup(func() {
		tofuDestroy(t, sharedDir, vars)
	})

	assertResourceInState(t, sharedDir, `module.configuration_policies[0].random_id.items["cfg_a"]`)
	assertResourceInState(t, sharedDir, `module.configuration_policies[1].random_id.items["cfg_a"]`)

	migDir := writeMigration(t, rootDir, "001_indexed_multi_instance.yaml", fmt.Sprintf(`
description: "Attempt to move a resource out of one instance of a count=2 module"
operations:
  - type: move
    source_layer: "%s"
    destination_layer: "%s"
    resources:
      - from: "module.configuration_policies[0].random_id.items"
`, sharedDir, networkingDir))

	err := runGenerateExpectError(t, []string{migDir})
	for _, want := range []string{
		"multi-instance module",
		"module.configuration_policies[0].random_id.items",
		"module.configuration_policies[1].random_id.items",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("expected error to contain %q, got: %v", want, err)
		}
	}

	// No migration files should have been written to either layer.
	if matches, _ := filepath.Glob(filepath.Join(sharedDir, "migration.*.tf")); len(matches) != 0 {
		t.Errorf("expected no generated files in source layer, got: %v", matches)
	}
	if matches, _ := filepath.Glob(filepath.Join(networkingDir, "migration.*.tf")); len(matches) != 0 {
		t.Errorf("expected no generated files in destination layer, got: %v", matches)
	}
}

// TestE2EFast_SplitForEachToMultipleLayers tests routing different for_each keys
// to different destination layers using multiple move operations.
func TestE2EFast_SplitForEachToMultipleLayers(t *testing.T) {
	t.Parallel()
	rootDir, _, vars := setupFastProject(t)

	sharedDir := filepath.Join(rootDir, "layers", "shared")
	appDir := filepath.Join(rootDir, "layers", "app")
	networkingDir := filepath.Join(rootDir, "layers", "networking")

	updateTfFile(t, sharedDir, "main.tf", randomProviderHCL+
		randomIDForEachResource("split", []string{"app_one", "app_two", "net_one"}))
	tofuInit(t, sharedDir)
	tofuApply(t, sharedDir, vars)
	t.Cleanup(func() {
		tofuDestroy(t, networkingDir, vars)
		tofuDestroy(t, appDir, vars)
		tofuDestroy(t, sharedDir, vars)
	})

	migDir := writeMigration(t, rootDir, "001_split.yaml", fmt.Sprintf(`
description: "Split for_each keys to different layers"
operations:
  - type: move
    source_layer: "%s"
    destination_layer: "%s"
    resources:
      - from: "random_id.split"
        keys:
          "app_*": '{{ .Key | trimPrefix "app_" }}'
  - type: move
    source_layer: "%s"
    destination_layer: "%s"
    resources:
      - from: "random_id.split"
        keys:
          "net_*": '{{ .Key | trimPrefix "net_" }}'
`, sharedDir, appDir, sharedDir, networkingDir))

	updateTfFile(t, appDir, "main.tf", randomProviderHCL+
		randomIDForEachResource("split", []string{"one", "two"}))
	updateTfFile(t, networkingDir, "main.tf", randomProviderHCL+
		randomIDForEachResource("split", []string{"one"}))
	updateTfFile(t, sharedDir, "main.tf", randomProviderHCL)

	requireGenerate(t, migDir)

	tofuInit(t, appDir)
	tofuInit(t, networkingDir)
	tofuApply(t, appDir, vars)
	tofuApply(t, networkingDir, vars)
	tofuApply(t, sharedDir, vars)

	assertResourceInState(t, appDir, `random_id.split["one"]`)
	assertResourceInState(t, appDir, `random_id.split["two"]`)
	assertResourceInState(t, networkingDir, `random_id.split["one"]`)

	cleanupAndAssertClean(t, vars, sharedDir, appDir, networkingDir)
}

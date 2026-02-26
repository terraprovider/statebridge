package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tfjson "github.com/hashicorp/terraform-json"

	"github.com/redtenant/tfmigrate/internal/testutil"
)

func TestEngine_ProcessFiles_KeyedMove(t *testing.T) {
	dir := t.TempDir()
	srcLayer := filepath.Join(dir, "layers", "old")
	dstLayer := filepath.Join(dir, "layers", "new")
	if err := os.MkdirAll(srcLayer, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dstLayer, 0o755); err != nil {
		t.Fatal(err)
	}

	// Keyed move: rename exact keys + prefix pattern
	migrationContent := `
description: "Keyed move"
operations:
  - type: move
    source_layer: "` + srcLayer + `"
    destination_layer: "` + dstLayer + `"
    resources:
      - from: "aws_resource.items"
        keys:
          exact_key: new_exact
          "prefix_*": '{{ .Key | trimPrefix "prefix_" }}'
`
	migrationFile := filepath.Join(dir, "001_keyed.yaml")
	if err := os.WriteFile(migrationFile, []byte(migrationContent), 0o644); err != nil {
		t.Fatal(err)
	}

	mock := testutil.NewMockStateReader(map[string]*tfjson.State{
		srcLayer: testutil.BuildState(
			testutil.NewResource(
				`aws_resource.items["exact_key"]`, "aws_resource", "items", "exact_key",
				map[string]interface{}{"id": "id-exact"},
			),
			testutil.NewResource(
				`aws_resource.items["prefix_alpha"]`, "aws_resource", "items", "prefix_alpha",
				map[string]interface{}{"id": "id-alpha"},
			),
			testutil.NewResource(
				`aws_resource.items["prefix_beta"]`, "aws_resource", "items", "prefix_beta",
				map[string]interface{}{"id": "id-beta"},
			),
		),
	})

	engine := New(Config{StateReader: mock})

	result, err := engine.ProcessFiles(context.Background(), []string{migrationFile})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.OutputFiles) != 2 {
		t.Fatalf("expected 2 files, got %d: %v", len(result.OutputFiles), result.OutputFiles)
	}

	srcContent := readLayerFile(t, result.OutputFiles, srcLayer)
	if strings.Count(srcContent, "removed {") != 1 {
		t.Errorf("expected 1 removed block, got:\n%s", srcContent)
	}

	dstContent := readLayerFile(t, result.OutputFiles, dstLayer)
	if strings.Count(dstContent, "import {") != 3 {
		t.Errorf("expected 3 import blocks, got:\n%s", dstContent)
	}
	// exact_key → new_exact
	if !strings.Contains(dstContent, `aws_resource.items["new_exact"]`) {
		t.Error("expected exact key renamed to new_exact")
	}
	// prefix_alpha → alpha
	if !strings.Contains(dstContent, `aws_resource.items["alpha"]`) {
		t.Error("expected prefix_alpha trimmed to alpha")
	}
	// prefix_beta → beta
	if !strings.Contains(dstContent, `aws_resource.items["beta"]`) {
		t.Error("expected prefix_beta trimmed to beta")
	}
}


func TestEngine_ProcessFiles_KeyedMoveWithAddressPrefix(t *testing.T) {
	dir := t.TempDir()
	srcLayer := filepath.Join(dir, "layers", "old")
	dstLayer := filepath.Join(dir, "layers", "new")
	if err := os.MkdirAll(srcLayer, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dstLayer, 0o755); err != nil {
		t.Fatal(err)
	}

	migrationContent := `
description: "Keyed move with address prefix"
operations:
  - type: move
    source_layer: "` + srcLayer + `"
    destination_layer: "` + dstLayer + `"
    address_prefix: "module.ig"
    resources:
      - from: "azuread_access_package_catalog.all"
        keys:
          mrt_customer: customer_approval
          mrt_vaw: vaw
`
	migrationFile := filepath.Join(dir, "001_prefix.yaml")
	if err := os.WriteFile(migrationFile, []byte(migrationContent), 0o644); err != nil {
		t.Fatal(err)
	}

	mock := testutil.NewMockStateReader(map[string]*tfjson.State{
		srcLayer: testutil.BuildState(
			testutil.NewResource(
				`module.ig.azuread_access_package_catalog.all["mrt_customer"]`,
				"azuread_access_package_catalog", "all", "mrt_customer",
				map[string]interface{}{"id": "id-customer"},
			),
			testutil.NewResource(
				`module.ig.azuread_access_package_catalog.all["mrt_vaw"]`,
				"azuread_access_package_catalog", "all", "mrt_vaw",
				map[string]interface{}{"id": "id-vaw"},
			),
		),
	})

	engine := New(Config{StateReader: mock})

	result, err := engine.ProcessFiles(context.Background(), []string{migrationFile})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.OutputFiles) != 2 {
		t.Fatalf("expected 2 files, got %d: %v", len(result.OutputFiles), result.OutputFiles)
	}

	dstContent := readLayerFile(t, result.OutputFiles, dstLayer)
	// Address prefix should be applied: module.ig.azuread_access_package_catalog.all["customer_approval"]
	if !strings.Contains(dstContent, `module.ig.azuread_access_package_catalog.all["customer_approval"]`) {
		t.Errorf("expected address prefix applied to destination, got:\n%s", dstContent)
	}
	if !strings.Contains(dstContent, `module.ig.azuread_access_package_catalog.all["vaw"]`) {
		t.Errorf("expected address prefix applied to vaw destination, got:\n%s", dstContent)
	}
}


func TestEngine_ProcessFiles_KeyedMoveSplitAcrossOps(t *testing.T) {
	dir := t.TempDir()
	srcLayer := filepath.Join(dir, "layers", "old")
	engLayer := filepath.Join(dir, "layers", "engineering")
	finLayer := filepath.Join(dir, "layers", "finance")
	for _, d := range []string{srcLayer, engLayer, finLayer} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	// Same resource split across two operations with different destination layers
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
	migrationFile := filepath.Join(dir, "001_split.yaml")
	if err := os.WriteFile(migrationFile, []byte(migrationContent), 0o644); err != nil {
		t.Fatal(err)
	}

	mock := testutil.NewMockStateReader(map[string]*tfjson.State{
		srcLayer: testutil.BuildState(
			testutil.NewResource(
				`aws_resource.items["eng_admin"]`, "aws_resource", "items", "eng_admin",
				map[string]interface{}{"id": "id-eng-admin"},
			),
			testutil.NewResource(
				`aws_resource.items["eng_reader"]`, "aws_resource", "items", "eng_reader",
				map[string]interface{}{"id": "id-eng-reader"},
			),
			testutil.NewResource(
				`aws_resource.items["fin_admin"]`, "aws_resource", "items", "fin_admin",
				map[string]interface{}{"id": "id-fin-admin"},
			),
			testutil.NewResource(
				`aws_resource.items["fin_reader"]`, "aws_resource", "items", "fin_reader",
				map[string]interface{}{"id": "id-fin-reader"},
			),
		),
	})

	engine := New(Config{StateReader: mock})

	result, err := engine.ProcessFiles(context.Background(), []string{migrationFile})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.OutputFiles) != 3 {
		t.Fatalf("expected 3 files (source + 2 destinations), got %d: %v", len(result.OutputFiles), result.OutputFiles)
	}

	srcContent := readLayerFile(t, result.OutputFiles, srcLayer)
	if strings.Count(srcContent, "removed {") != 1 {
		t.Errorf("expected exactly 1 removed block in source, got:\n%s", srcContent)
	}

	engContent := readLayerFile(t, result.OutputFiles, engLayer)
	if strings.Count(engContent, "import {") != 2 {
		t.Errorf("expected 2 import blocks in engineering, got:\n%s", engContent)
	}

	finContent := readLayerFile(t, result.OutputFiles, finLayer)
	if strings.Count(finContent, "import {") != 2 {
		t.Errorf("expected 2 import blocks in finance, got:\n%s", finContent)
	}
}


func TestEngine_ProcessFiles_IncompleteCoverage(t *testing.T) {
	dir := t.TempDir()
	srcLayer := filepath.Join(dir, "layers", "old")
	dstLayer := filepath.Join(dir, "layers", "new")
	if err := os.MkdirAll(srcLayer, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dstLayer, 0o755); err != nil {
		t.Fatal(err)
	}

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
	migrationFile := filepath.Join(dir, "001_incomplete.yaml")
	if err := os.WriteFile(migrationFile, []byte(migrationContent), 0o644); err != nil {
		t.Fatal(err)
	}

	mock := testutil.NewMockStateReader(map[string]*tfjson.State{
		srcLayer: testutil.BuildState(
			testutil.NewResource(
				`aws_resource.items["eng_admin"]`, "aws_resource", "items", "eng_admin",
				map[string]interface{}{"id": "id-eng-admin"},
			),
			testutil.NewResource(
				`aws_resource.items["other_admin"]`, "aws_resource", "items", "other_admin",
				map[string]interface{}{"id": "id-other-admin"},
			),
		),
	})

	engine := New(Config{StateReader: mock})

	// Incomplete coverage causes the file to be skipped (not a fatal error).
	// Since it's the only file, ProcessFiles returns "all migration files were skipped".
	_, err := engine.ProcessFiles(context.Background(), []string{migrationFile})
	if err == nil {
		t.Fatal("expected error when all files are skipped")
	}
	if !strings.Contains(err.Error(), "skipped") {
		t.Errorf("expected error to mention files were skipped, got: %v", err)
	}
}



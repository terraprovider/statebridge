package migration

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseFile_ValidMoveOperation(t *testing.T) {
	content := `
description: "Move web instance"
schema_version: "2"
operations:
  - type: move
    description: "Move to app layer"
    source_layer: "./layers/compute"
    destination_layer: "./layers/app"
    resources:
      - from: "aws_instance.web"
        import_id: "i-0abc123"
`
	path := writeTestFile(t, "001_move.yaml", content)
	parser := NewParser()

	mf, err := parser.ParseFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mf.Description != "Move web instance" {
		t.Errorf("expected description %q, got %q", "Move web instance", mf.Description)
	}
	if mf.SchemaVersion != "2" {
		t.Errorf("expected schema_version %q, got %q", "2", mf.SchemaVersion)
	}
	if len(mf.Operations) != 1 {
		t.Fatalf("expected 1 operation, got %d", len(mf.Operations))
	}

	op := mf.Operations[0]
	if op.Type != OpMove {
		t.Errorf("expected type %q, got %q", OpMove, op.Type)
	}
	if op.SourceLayer != "./layers/compute" {
		t.Errorf("expected source_layer %q, got %q", "./layers/compute", op.SourceLayer)
	}
	if op.DestinationLayer != "./layers/app" {
		t.Errorf("expected destination_layer %q, got %q", "./layers/app", op.DestinationLayer)
	}
	if len(op.Resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(op.Resources))
	}
	res := op.Resources[0]
	if res.From != "aws_instance.web" {
		t.Errorf("expected from %q, got %q", "aws_instance.web", res.From)
	}
	if res.ImportID != "i-0abc123" {
		t.Errorf("expected import_id %q, got %q", "i-0abc123", res.ImportID)
	}
}

func TestParseFile_AllOperationTypes(t *testing.T) {
	content := `
description: "All operation types"
operations:
  - type: move
    source_layer: "./src"
    destination_layer: "./dst"
    resources:
      - from: "aws_instance.a"
  - type: rename
    layer: "./layers/net"
    renames:
      - from: "module.old"
        to: "module.new"
  - type: remove
    layer: "./layers/legacy"
    destroy: false
    entries:
      - address: "aws_iam_role.deprecated"
  - type: import
    layer: "./layers/db"
    imports:
      - address: "aws_db_instance.primary"
        id: "my-db-id"
        provider: "aws.useast1"
`
	path := writeTestFile(t, "002_all.yaml", content)
	parser := NewParser()

	mf, err := parser.ParseFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mf.Operations) != 4 {
		t.Fatalf("expected 4 operations, got %d", len(mf.Operations))
	}

	if mf.Operations[0].Type != OpMove {
		t.Errorf("op[0]: expected type move, got %q", mf.Operations[0].Type)
	}
	if mf.Operations[1].Type != OpRename {
		t.Errorf("op[1]: expected type rename, got %q", mf.Operations[1].Type)
	}
	if mf.Operations[2].Type != OpRemove {
		t.Errorf("op[2]: expected type remove, got %q", mf.Operations[2].Type)
	}
	if mf.Operations[3].Type != OpImport {
		t.Errorf("op[3]: expected type import, got %q", mf.Operations[3].Type)
	}

	// Check remove destroy field
	if mf.Operations[2].DestroyValue() != false {
		t.Error("op[2]: expected destroy=false")
	}

	// Check import provider
	if len(mf.Operations[3].Imports) != 1 {
		t.Fatalf("op[3]: expected 1 import entry, got %d", len(mf.Operations[3].Imports))
	}
	if mf.Operations[3].Imports[0].Provider != "aws.useast1" {
		t.Errorf("op[3]: expected provider %q, got %q", "aws.useast1", mf.Operations[3].Imports[0].Provider)
	}
}

func TestParseFile_MoveWithKeysMap(t *testing.T) {
	content := `
description: "Move with keys map"
operations:
  - type: move
    source_layer: "./layers/old"
    destination_layer: "./layers/new"
    resources:
      - from: "aws_s3_bucket.data"
        keys:
          exact_key: new_key
          "prefix_*": '{{ .Key | trimPrefix "prefix_" }}'
`
	path := writeTestFile(t, "003_keys.yaml", content)
	parser := NewParser()

	mf, err := parser.ParseFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	op := mf.Operations[0]
	if len(op.Resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(op.Resources))
	}
	res := op.Resources[0]
	if res.From != "aws_s3_bucket.data" {
		t.Errorf("expected from %q, got %q", "aws_s3_bucket.data", res.From)
	}
	if len(res.Keys) != 2 {
		t.Fatalf("expected 2 key entries, got %d", len(res.Keys))
	}
	if res.Keys["exact_key"] != "new_key" {
		t.Errorf("expected exact_key → new_key, got %q", res.Keys["exact_key"])
	}
	if res.Keys["prefix_*"] != `{{ .Key | trimPrefix "prefix_" }}` {
		t.Errorf("expected prefix template, got %q", res.Keys["prefix_*"])
	}
}

func TestParseFile_MoveWithAddressPrefix(t *testing.T) {
	content := `
description: "Move with address prefix"
operations:
  - type: move
    source_layer: "./src"
    destination_layer: "./dst"
    address_prefix: module.identity_governance
    resources:
      - from: azuread_access_package_catalog.all
        keys:
          mrt_customer: customer_approval
`
	path := writeTestFile(t, "004_prefix.yaml", content)
	parser := NewParser()

	mf, err := parser.ParseFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	op := mf.Operations[0]
	if op.AddressPrefix != "module.identity_governance" {
		t.Errorf("expected address_prefix %q, got %q", "module.identity_governance", op.AddressPrefix)
	}
	if len(op.Resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(op.Resources))
	}
	if op.Resources[0].From != "azuread_access_package_catalog.all" {
		t.Errorf("expected from %q, got %q", "azuread_access_package_catalog.all", op.Resources[0].From)
	}
}

func TestParseFile_MoveWithTo(t *testing.T) {
	content := `
description: "Move with destination address override"
operations:
  - type: move
    source_layer: "./src"
    destination_layer: "./dst"
    resources:
      - from: module.old.resource.all
        to: module.new.resource.all
        keys:
          key1: key1
`
	path := writeTestFile(t, "005_destaddr.yaml", content)
	parser := NewParser()

	mf, err := parser.ParseFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	res := mf.Operations[0].Resources[0]
	if res.To != "module.new.resource.all" {
		t.Errorf("expected to %q, got %q", "module.new.resource.all", res.To)
	}
}

func TestParseFile_MultipleRenames(t *testing.T) {
	content := `
description: "Multiple renames"
operations:
  - type: rename
    layer: "./layers/net"
    renames:
      - from: module.old_vpc
        to: module.new_vpc
      - from: aws_route_table.old
        to: aws_route_table.new
`
	path := writeTestFile(t, "006_renames.yaml", content)
	parser := NewParser()

	mf, err := parser.ParseFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	op := mf.Operations[0]
	if len(op.Renames) != 2 {
		t.Fatalf("expected 2 renames, got %d", len(op.Renames))
	}
	if op.Renames[0].From != "module.old_vpc" || op.Renames[0].To != "module.new_vpc" {
		t.Errorf("rename[0]: expected old_vpc→new_vpc, got %q→%q", op.Renames[0].From, op.Renames[0].To)
	}
	if op.Renames[1].From != "aws_route_table.old" || op.Renames[1].To != "aws_route_table.new" {
		t.Errorf("rename[1]: expected old→new, got %q→%q", op.Renames[1].From, op.Renames[1].To)
	}
}

func TestParseFile_MultipleRemoveEntries(t *testing.T) {
	content := `
description: "Multiple removals"
operations:
  - type: remove
    layer: "./layers/iam"
    entries:
      - address: aws_iam_role.deprecated
      - address: aws_iam_policy.old
`
	path := writeTestFile(t, "007_removes.yaml", content)
	parser := NewParser()

	mf, err := parser.ParseFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	op := mf.Operations[0]
	if len(op.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(op.Entries))
	}
	if op.Entries[0].Address != "aws_iam_role.deprecated" {
		t.Errorf("entries[0]: expected %q, got %q", "aws_iam_role.deprecated", op.Entries[0].Address)
	}
	if op.Entries[1].Address != "aws_iam_policy.old" {
		t.Errorf("entries[1]: expected %q, got %q", "aws_iam_policy.old", op.Entries[1].Address)
	}
}

func TestParseFile_RemovePerEntryDestroy(t *testing.T) {
	content := `
description: "Remove with per-entry destroy"
operations:
  - type: remove
    layer: "./layers/iam"
    destroy: false
    entries:
      - address: aws_iam_role.keep
      - address: aws_iam_role.delete
        destroy: true
`
	path := writeTestFile(t, "007b_remove_destroy.yaml", content)
	parser := NewParser()

	mf, err := parser.ParseFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	op := mf.Operations[0]
	if len(op.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(op.Entries))
	}
	if op.Entries[0].Destroy != nil {
		t.Error("entries[0]: expected nil destroy (inherit from op)")
	}
	if op.Entries[1].Destroy == nil || *op.Entries[1].Destroy != true {
		t.Error("entries[1]: expected destroy=true")
	}
}

func TestParseFile_MultipleImports(t *testing.T) {
	content := `
description: "Multiple imports"
operations:
  - type: import
    layer: "./layers/db"
    imports:
      - address: aws_db_instance.primary
        id: "my-db-id"
      - address: aws_db_instance.replica
        id: "my-replica-id"
        provider: aws.useast1
`
	path := writeTestFile(t, "008_imports.yaml", content)
	parser := NewParser()

	mf, err := parser.ParseFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	op := mf.Operations[0]
	if len(op.Imports) != 2 {
		t.Fatalf("expected 2 imports, got %d", len(op.Imports))
	}
	if op.Imports[0].Address != "aws_db_instance.primary" {
		t.Errorf("import[0]: expected address %q, got %q", "aws_db_instance.primary", op.Imports[0].Address)
	}
	if op.Imports[0].ID != "my-db-id" {
		t.Errorf("import[0]: expected id %q, got %q", "my-db-id", op.Imports[0].ID)
	}
	if op.Imports[1].Provider != "aws.useast1" {
		t.Errorf("import[1]: expected provider %q, got %q", "aws.useast1", op.Imports[1].Provider)
	}
}

func TestParseFile_ImportWithOpLevelProvider(t *testing.T) {
	content := `
description: "Import with operation-level provider"
operations:
  - type: import
    layer: "./layers/db"
    provider: "aws.useast1"
    imports:
      - address: aws_db_instance.primary
        id: "my-db-id"
      - address: aws_db_instance.replica
        id: "my-replica-id"
        provider: "aws.uswest2"
`
	path := writeTestFile(t, "008b_provider.yaml", content)
	parser := NewParser()

	mf, err := parser.ParseFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	op := mf.Operations[0]
	if op.Provider != "aws.useast1" {
		t.Errorf("expected op-level provider %q, got %q", "aws.useast1", op.Provider)
	}
	if op.Imports[0].Provider != "" {
		t.Errorf("import[0]: expected no per-entry provider, got %q", op.Imports[0].Provider)
	}
	if op.Imports[1].Provider != "aws.uswest2" {
		t.Errorf("import[1]: expected provider %q, got %q", "aws.uswest2", op.Imports[1].Provider)
	}
}

func TestParseFile_InvalidYAML(t *testing.T) {
	content := `
description: "Bad YAML
  - this is not valid
`
	path := writeTestFile(t, "bad.yaml", content)
	parser := NewParser()

	_, err := parser.ParseFile(path)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestParseFile_NonexistentFile(t *testing.T) {
	parser := NewParser()
	_, err := parser.ParseFile("/nonexistent/file.yaml")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestParseDir_SortedByFilename(t *testing.T) {
	dir := t.TempDir()

	// Write files in reverse order to verify sorting
	writeFile(t, filepath.Join(dir, "003_third.yaml"), `
description: "Third"
operations:
  - type: remove
    layer: "./l"
    entries:
      - address: "aws_instance.c"
`)
	writeFile(t, filepath.Join(dir, "001_first.yaml"), `
description: "First"
operations:
  - type: remove
    layer: "./l"
    entries:
      - address: "aws_instance.a"
`)
	writeFile(t, filepath.Join(dir, "002_second.yaml"), `
description: "Second"
operations:
  - type: remove
    layer: "./l"
    entries:
      - address: "aws_instance.b"
`)
	// Non-YAML file should be ignored
	writeFile(t, filepath.Join(dir, "readme.txt"), "not a migration")

	parser := NewParser()
	files, err := parser.ParseDir(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(files) != 3 {
		t.Fatalf("expected 3 files, got %d", len(files))
	}

	expected := []string{"First", "Second", "Third"}
	for i, mf := range files {
		if mf.Description != expected[i] {
			t.Errorf("file[%d]: expected description %q, got %q", i, expected[i], mf.Description)
		}
	}
}

func TestParseDir_EmptyDirectory(t *testing.T) {
	dir := t.TempDir()
	parser := NewParser()

	files, err := parser.ParseDir(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("expected 0 files, got %d", len(files))
	}
}

func TestParseFiles_MixedFilesAndDirs(t *testing.T) {
	dir := t.TempDir()
	subdir := filepath.Join(dir, "subdir")
	if err := os.Mkdir(subdir, 0o755); err != nil {
		t.Fatal(err)
	}

	filePath := writeFile(t, filepath.Join(dir, "standalone.yaml"), `
description: "Standalone"
operations:
  - type: remove
    layer: "./l"
    entries:
      - address: "aws_instance.a"
`)
	writeFile(t, filepath.Join(subdir, "001_sub.yaml"), `
description: "Subdir file"
operations:
  - type: remove
    layer: "./l"
    entries:
      - address: "aws_instance.b"
`)

	parser := NewParser()
	files, err := parser.ParseFiles([]string{filePath, subdir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(files))
	}
	if files[0].Description != "Standalone" {
		t.Errorf("file[0]: expected description %q, got %q", "Standalone", files[0].Description)
	}
	if files[1].Description != "Subdir file" {
		t.Errorf("file[1]: expected description %q, got %q", "Subdir file", files[1].Description)
	}
}

func TestParseFile_SetsFilePath(t *testing.T) {
	content := `
description: "Test"
operations:
  - type: remove
    layer: "./l"
    entries:
      - address: "aws_instance.x"
`
	path := writeTestFile(t, "test.yaml", content)
	parser := NewParser()

	mf, err := parser.ParseFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	absPath, _ := filepath.Abs(path)
	if mf.FilePath != absPath {
		t.Errorf("expected FilePath %q, got %q", absPath, mf.FilePath)
	}
}

func TestDestroyValue_DefaultFalse(t *testing.T) {
	op := Operation{Type: OpRemove}
	if op.DestroyValue() != false {
		t.Error("expected default destroy value to be false")
	}
}

func TestDestroyValue_ExplicitTrue(t *testing.T) {
	v := true
	op := Operation{Type: OpRemove, Destroy: &v}
	if op.DestroyValue() != true {
		t.Error("expected destroy value to be true")
	}
}

func TestParseFile_WithCondition(t *testing.T) {
	content := `
description: "Move with condition"
condition:
  resources_exist:
    - layer: "./layers/compute"
      addresses:
        - "aws_instance.web"
        - "aws_instance.api"
  resources_not_exist:
    - layer: "./layers/app"
      addresses:
        - "aws_instance.web"
operations:
  - type: move
    source_layer: "./layers/compute"
    destination_layer: "./layers/app"
    resources:
      - from: "aws_instance.web"
        import_id: "i-0abc123"
`
	path := writeTestFile(t, "001_cond.yaml", content)
	parser := NewParser()

	mf, err := parser.ParseFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mf.Condition == nil {
		t.Fatal("expected condition to be parsed")
	}
	if len(mf.Condition.ResourcesExist) != 1 {
		t.Fatalf("expected 1 resources_exist check, got %d", len(mf.Condition.ResourcesExist))
	}
	re := mf.Condition.ResourcesExist[0]
	if re.Layer != "./layers/compute" {
		t.Errorf("expected layer %q, got %q", "./layers/compute", re.Layer)
	}
	if len(re.Addresses) != 2 {
		t.Fatalf("expected 2 addresses, got %d", len(re.Addresses))
	}
	if re.Addresses[0] != "aws_instance.web" {
		t.Errorf("expected address %q, got %q", "aws_instance.web", re.Addresses[0])
	}
	if re.Addresses[1] != "aws_instance.api" {
		t.Errorf("expected address %q, got %q", "aws_instance.api", re.Addresses[1])
	}

	if len(mf.Condition.ResourcesNotExist) != 1 {
		t.Fatalf("expected 1 resources_not_exist check, got %d", len(mf.Condition.ResourcesNotExist))
	}
	rne := mf.Condition.ResourcesNotExist[0]
	if rne.Layer != "./layers/app" {
		t.Errorf("expected layer %q, got %q", "./layers/app", rne.Layer)
	}
	if len(rne.Addresses) != 1 || rne.Addresses[0] != "aws_instance.web" {
		t.Errorf("expected address %q, got %v", "aws_instance.web", rne.Addresses)
	}
}

func TestParseFile_WithoutCondition(t *testing.T) {
	content := `
description: "No condition"
operations:
  - type: remove
    layer: "./l"
    entries:
      - address: "aws_instance.x"
`
	path := writeTestFile(t, "002_nocond.yaml", content)
	parser := NewParser()

	mf, err := parser.ParseFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mf.Condition != nil {
		t.Errorf("expected nil condition, got %+v", mf.Condition)
	}
}

func TestFullAddress(t *testing.T) {
	tests := []struct {
		prefix, addr, want string
	}{
		{"", "aws_instance.web", "aws_instance.web"},
		{"module.vpc", "aws_subnet.main", "module.vpc.aws_subnet.main"},
		{"module.a.module.b", "resource.x", "module.a.module.b.resource.x"},
	}
	for _, tt := range tests {
		got := FullAddress(tt.prefix, tt.addr)
		if got != tt.want {
			t.Errorf("FullAddress(%q, %q) = %q, want %q", tt.prefix, tt.addr, got, tt.want)
		}
	}
}

func TestParseFile_WithStatusRetired(t *testing.T) {
	content := `
description: "Old migration"
status: retired
operations:
  - type: remove
    layer: "./l"
    entries:
      - address: "aws_instance.x"
`
	path := writeTestFile(t, "010_retired.yaml", content)
	parser := NewParser()

	mf, err := parser.ParseFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mf.Status != "retired" {
		t.Errorf("expected status %q, got %q", "retired", mf.Status)
	}
}

func TestParseFile_WithLayerConditions(t *testing.T) {
	content := `
description: "Move with layer conditions"
condition:
  layer_exists:
    - "./layers/source"
  layer_not_exists:
    - "./layers/deprecated"
operations:
  - type: remove
    layer: "./l"
    entries:
      - address: "aws_instance.x"
`
	path := writeTestFile(t, "011_layer_cond.yaml", content)
	parser := NewParser()

	mf, err := parser.ParseFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mf.Condition == nil {
		t.Fatal("expected condition to be parsed")
	}
	if len(mf.Condition.LayerExists) != 1 {
		t.Fatalf("expected 1 layer_exists entry, got %d", len(mf.Condition.LayerExists))
	}
	if mf.Condition.LayerExists[0] != "./layers/source" {
		t.Errorf("expected layer_exists[0] %q, got %q", "./layers/source", mf.Condition.LayerExists[0])
	}
	if len(mf.Condition.LayerNotExists) != 1 {
		t.Fatalf("expected 1 layer_not_exists entry, got %d", len(mf.Condition.LayerNotExists))
	}
	if mf.Condition.LayerNotExists[0] != "./layers/deprecated" {
		t.Errorf("expected layer_not_exists[0] %q, got %q", "./layers/deprecated", mf.Condition.LayerNotExists[0])
	}
}

// writeTestFile creates a temporary YAML file and returns its path.
func writeTestFile(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	return writeFile(t, filepath.Join(dir, name), content)
}

// writeFile writes content to a file path and returns the path.
func writeFile(t *testing.T, path, content string) string {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing test file %q: %v", path, err)
	}
	return path
}

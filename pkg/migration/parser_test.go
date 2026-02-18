package migration

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseFile_ValidMoveOperation(t *testing.T) {
	content := `
description: "Move web instance"
schema_version: "1"
operations:
  - type: move
    description: "Move to app layer"
    source:
      layer: "./layers/compute"
      address: "aws_instance.web"
    destination:
      layer: "./layers/app"
      address: "aws_instance.web"
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
	if mf.SchemaVersion != "1" {
		t.Errorf("expected schema_version %q, got %q", "1", mf.SchemaVersion)
	}
	if len(mf.Operations) != 1 {
		t.Fatalf("expected 1 operation, got %d", len(mf.Operations))
	}

	op := mf.Operations[0]
	if op.Type != OpMove {
		t.Errorf("expected type %q, got %q", OpMove, op.Type)
	}
	if op.Source == nil {
		t.Fatal("expected source to be non-nil")
	}
	if op.Source.Layer != "./layers/compute" {
		t.Errorf("expected source layer %q, got %q", "./layers/compute", op.Source.Layer)
	}
	if op.Source.Address != "aws_instance.web" {
		t.Errorf("expected source address %q, got %q", "aws_instance.web", op.Source.Address)
	}
	if op.Destination == nil {
		t.Fatal("expected destination to be non-nil")
	}
	if op.Destination.Layer != "./layers/app" {
		t.Errorf("expected destination layer %q, got %q", "./layers/app", op.Destination.Layer)
	}
	if op.ImportID != "i-0abc123" {
		t.Errorf("expected import_id %q, got %q", "i-0abc123", op.ImportID)
	}
}

func TestParseFile_AllOperationTypes(t *testing.T) {
	content := `
description: "All operation types"
operations:
  - type: move
    source:
      layer: "./src"
      address: "aws_instance.a"
    destination:
      layer: "./dst"
      address: "aws_instance.b"
  - type: rename
    layer: "./layers/net"
    from: "module.old"
    to: "module.new"
  - type: remove
    layer: "./layers/legacy"
    address: "aws_iam_role.deprecated"
    destroy: false
  - type: import
    layer: "./layers/db"
    address: "aws_db_instance.primary"
    import_id: "my-db-id"
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
	if mf.Operations[3].Provider != "aws.useast1" {
		t.Errorf("op[3]: expected provider %q, got %q", "aws.useast1", mf.Operations[3].Provider)
	}
}

func TestParseFile_WildcardWithTemplate(t *testing.T) {
	content := `
description: "Wildcard move"
operations:
  - type: move
    source:
      layer: "./layers/data"
      address: 'aws_s3_bucket.data[*]'
    destination:
      layer: "./layers/storage"
      address: 'aws_s3_bucket.data["{{ .Attributes.bucket }}"]'
    import_id: "{{ .Attributes.id }}"
`
	path := writeTestFile(t, "003_wildcard.yaml", content)
	parser := NewParser()

	mf, err := parser.ParseFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	op := mf.Operations[0]
	if op.Source.Address != "aws_s3_bucket.data[*]" {
		t.Errorf("expected wildcard address, got %q", op.Source.Address)
	}
	if op.Destination.Address != `aws_s3_bucket.data["{{ .Attributes.bucket }}"]` {
		t.Errorf("expected template destination address, got %q", op.Destination.Address)
	}
	if op.ImportID != "{{ .Attributes.id }}" {
		t.Errorf("expected template import_id, got %q", op.ImportID)
	}
}

func TestParseFile_WildcardWithKeyPrefix(t *testing.T) {
	content := `
description: "Prefix-filtered wildcard move"
operations:
  - type: move
    source:
      layer: "./layers/old"
      address: "aws_resource.items[*]"
      key_prefix: "engineering_"
    destination:
      layer: "./layers/engineering"
      address: 'aws_resource.items["{{ .Key | trimPrefix "engineering_" }}"]'
  - type: move
    source:
      layer: "./layers/old"
      address: "aws_resource.items[*]"
      key_prefix: "finance_"
    destination:
      layer: "./layers/finance"
      address: 'aws_resource.items["{{ .Key | trimPrefix "finance_" }}"]'
`
	path := writeTestFile(t, "004_prefix.yaml", content)
	parser := NewParser()

	mf, err := parser.ParseFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mf.Operations) != 2 {
		t.Fatalf("expected 2 operations, got %d", len(mf.Operations))
	}

	op0 := mf.Operations[0]
	if op0.Source.KeyPrefix != "engineering_" {
		t.Errorf("op[0]: expected key_prefix %q, got %q", "engineering_", op0.Source.KeyPrefix)
	}

	op1 := mf.Operations[1]
	if op1.Source.KeyPrefix != "finance_" {
		t.Errorf("op[1]: expected key_prefix %q, got %q", "finance_", op1.Source.KeyPrefix)
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
    address: "aws_instance.c"
`)
	writeFile(t, filepath.Join(dir, "001_first.yaml"), `
description: "First"
operations:
  - type: remove
    layer: "./l"
    address: "aws_instance.a"
`)
	writeFile(t, filepath.Join(dir, "002_second.yaml"), `
description: "Second"
operations:
  - type: remove
    layer: "./l"
    address: "aws_instance.b"
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
    address: "aws_instance.a"
`)
	writeFile(t, filepath.Join(subdir, "001_sub.yaml"), `
description: "Subdir file"
operations:
  - type: remove
    layer: "./l"
    address: "aws_instance.b"
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
    address: "aws_instance.x"
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

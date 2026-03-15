package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestImportBlock_Render(t *testing.T) {
	b := &ImportBlock{
		To:    "aws_instance.web",
		ID:    "i-0abc123def456",
		Layer: "./layers/app",
	}

	expected := `import {
  to = aws_instance.web
  id = "i-0abc123def456"
}`
	result := b.Render()
	if result != expected {
		t.Errorf("expected:\n%s\ngot:\n%s", expected, result)
	}
}

func TestImportBlock_RenderWithProvider(t *testing.T) {
	b := &ImportBlock{
		To:       "aws_db_instance.primary",
		ID:       "my-database",
		Provider: "aws.useast1",
		Layer:    "./layers/db",
	}

	expected := `import {
  to       = aws_db_instance.primary
  id       = "my-database"
  provider = aws.useast1
}`
	result := b.Render()
	if result != expected {
		t.Errorf("expected:\n%s\ngot:\n%s", expected, result)
	}
}

func TestImportBlock_RenderWithForEachKey(t *testing.T) {
	b := &ImportBlock{
		To:    `aws_s3_bucket.data["my-bucket"]`,
		ID:    "my-bucket-id",
		Layer: "./layers/storage",
	}

	expected := `import {
  to = aws_s3_bucket.data["my-bucket"]
  id = "my-bucket-id"
}`
	result := b.Render()
	if result != expected {
		t.Errorf("expected:\n%s\ngot:\n%s", expected, result)
	}
}

func TestMovedBlock_Render(t *testing.T) {
	b := &MovedBlock{
		From:  "module.old_vpc",
		To:    "module.new_vpc",
		Layer: "./layers/networking",
	}

	expected := `moved {
  from = module.old_vpc
  to   = module.new_vpc
}`
	result := b.Render()
	if result != expected {
		t.Errorf("expected:\n%s\ngot:\n%s", expected, result)
	}
}

func TestMovedBlock_RenderWithForEachKey(t *testing.T) {
	b := &MovedBlock{
		From:  `aws_security_group.sg["key-a"]`,
		To:    `aws_security_group.sg["key-b"]`,
		Layer: "./layers/networking",
	}

	expected := `moved {
  from = aws_security_group.sg["key-a"]
  to   = aws_security_group.sg["key-b"]
}`
	result := b.Render()
	if result != expected {
		t.Errorf("expected:\n%s\ngot:\n%s", expected, result)
	}
}

func TestRemovedBlock_Render_DestroyFalse(t *testing.T) {
	b := &RemovedBlock{
		From:    "aws_iam_role.deprecated",
		Destroy: false,
		Layer:   "./layers/legacy",
	}

	expected := `removed {
  from = aws_iam_role.deprecated

  lifecycle {
    destroy = false
  }
}`
	result := b.Render()
	if result != expected {
		t.Errorf("expected:\n%s\ngot:\n%s", expected, result)
	}
}

func TestRemovedBlock_Render_DestroyTrue(t *testing.T) {
	b := &RemovedBlock{
		From:    "aws_instance.old",
		Destroy: true,
		Layer:   "./layers/compute",
	}

	expected := `removed {
  from = aws_instance.old

  lifecycle {
    destroy = true
  }
}`
	result := b.Render()
	if result != expected {
		t.Errorf("expected:\n%s\ngot:\n%s", expected, result)
	}
}

func TestBlock_LayerPath(t *testing.T) {
	tests := []struct {
		name  string
		block Block
		layer string
	}{
		{"import", &ImportBlock{Layer: "./layers/app"}, "./layers/app"},
		{"moved", &MovedBlock{Layer: "./layers/net"}, "./layers/net"},
		{"removed", &RemovedBlock{Layer: "./layers/old"}, "./layers/old"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.block.LayerPath() != tt.layer {
				t.Errorf("expected layer %q, got %q", tt.layer, tt.block.LayerPath())
			}
		})
	}
}

func TestBlock_Comment(t *testing.T) {
	b := &ImportBlock{Description: "Import web server"}
	if b.Comment() != "Import web server" {
		t.Errorf("expected comment %q, got %q", "Import web server", b.Comment())
	}
}

func TestBlock_SourceFile(t *testing.T) {
	tests := []struct {
		name   string
		block  Block
		source string
	}{
		{"import", &ImportBlock{Source: "001.yaml"}, "001.yaml"},
		{"moved", &MovedBlock{Source: "002.yaml"}, "002.yaml"},
		{"removed", &RemovedBlock{Source: "003.yaml"}, "003.yaml"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.block.SourceFile() != tt.source {
				t.Errorf("expected source %q, got %q", tt.source, tt.block.SourceFile())
			}
		})
	}
}

func TestBlock_SortAddress(t *testing.T) {
	tests := []struct {
		name    string
		block   Block
		address string
	}{
		{"import uses To", &ImportBlock{To: "aws_instance.web"}, "aws_instance.web"},
		{"moved uses From", &MovedBlock{From: "module.old"}, "module.old"},
		{"removed uses From", &RemovedBlock{From: "aws_iam_role.x"}, "aws_iam_role.x"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.block.SortAddress() != tt.address {
				t.Errorf("expected address %q, got %q", tt.address, tt.block.SortAddress())
			}
		})
	}
}

func TestSortBlocks(t *testing.T) {
	blocks := []Block{
		&ImportBlock{To: "z_resource.b", Source: "001.yaml"},
		&RemovedBlock{From: "b_resource.y", Source: "001.yaml"},
		&MovedBlock{From: "m_resource.c", Source: "001.yaml"},
		&ImportBlock{To: "a_resource.a", Source: "001.yaml"},
		&RemovedBlock{From: "a_resource.x", Source: "001.yaml"},
	}

	SortBlocks(blocks)

	// Expected order: removed(a), removed(b), moved(m), import(a), import(z)
	expected := []string{
		"a_resource.x", // removed
		"b_resource.y", // removed
		"m_resource.c", // moved
		"a_resource.a", // import
		"z_resource.b", // import
	}

	for i, exp := range expected {
		if blocks[i].SortAddress() != exp {
			t.Errorf("position %d: expected %q, got %q", i, exp, blocks[i].SortAddress())
		}
	}
}

func TestWriter_GroupedByLayerAndSource(t *testing.T) {
	w := NewWriter()
	w.AddBlock(&ImportBlock{To: "a", ID: "1", Layer: "./app", Source: "001.yaml"})
	w.AddBlock(&RemovedBlock{From: "b", Layer: "./compute", Source: "001.yaml"})
	w.AddBlock(&MovedBlock{From: "c", To: "d", Layer: "./app", Source: "002.yaml"})

	grouped := w.groupedByLayerAndSource()
	if len(grouped) != 3 {
		t.Errorf("expected 3 groups, got %d", len(grouped))
	}

	key1 := groupKey{Layer: "./app", SourceFile: "001.yaml"}
	if len(grouped[key1]) != 1 {
		t.Errorf("expected 1 block for app/001, got %d", len(grouped[key1]))
	}

	key2 := groupKey{Layer: "./app", SourceFile: "002.yaml"}
	if len(grouped[key2]) != 1 {
		t.Errorf("expected 1 block for app/002, got %d", len(grouped[key2]))
	}
}

func TestWriter_RenderAll_DeterministicOutput(t *testing.T) {
	w := NewWriter()
	w.AddBlock(&ImportBlock{To: "z_instance.web", ID: "i-2", Layer: "./dst", Source: "001.yaml"})
	w.AddBlock(&RemovedBlock{From: "z_instance.web", Layer: "./src", Source: "001.yaml"})
	w.AddBlock(&ImportBlock{To: "a_instance.api", ID: "i-1", Layer: "./dst", Source: "001.yaml"})
	w.AddBlock(&RemovedBlock{From: "a_instance.api", Layer: "./src", Source: "001.yaml"})

	all := w.RenderAll()
	if len(all) != 2 {
		t.Fatalf("expected 2 output files, got %d", len(all))
	}

	// Check that blocks are sorted: removed(a) before removed(z) in source
	for path, content := range all {
		if strings.Contains(path, "src") {
			idx1 := strings.Index(content, "a_instance.api")
			idx2 := strings.Index(content, "z_instance.web")
			if idx1 >= idx2 {
				t.Error("expected a_instance.api before z_instance.web in sorted output")
			}
		}
		if strings.Contains(path, "dst") {
			idx1 := strings.Index(content, "a_instance.api")
			idx2 := strings.Index(content, "z_instance.web")
			if idx1 >= idx2 {
				t.Error("expected a_instance.api before z_instance.web in sorted import output")
			}
		}
	}
}

func TestWriter_RenderAll_SeparateFilesPerSource(t *testing.T) {
	w := NewWriter()
	w.AddBlock(&RemovedBlock{From: "a", Layer: "./layer", Source: "001.yaml"})
	w.AddBlock(&RemovedBlock{From: "b", Layer: "./layer", Source: "002.yaml"})

	all := w.RenderAll()
	if len(all) != 2 {
		t.Fatalf("expected 2 output files (one per source), got %d", len(all))
	}

	// Verify different files for different sources
	for path := range all {
		base := filepath.Base(path)
		if !strings.HasPrefix(base, "migration.") {
			t.Errorf("expected filename to start with 'migration.', got %q", base)
		}
		if !strings.HasSuffix(base, ".tf") {
			t.Errorf("expected filename to end with '.tf', got %q", base)
		}
	}
}

func TestWriter_WriteAll(t *testing.T) {
	dir := t.TempDir()
	layerDir := filepath.Join(dir, "layers", "app")
	if err := os.MkdirAll(layerDir, 0o755); err != nil {
		t.Fatal(err)
	}

	w := NewWriter()
	w.AddBlock(&ImportBlock{
		To:     "aws_instance.web",
		ID:     "i-123",
		Layer:  layerDir,
		Source: "001_move.yaml",
	})

	files, err := w.WriteAll()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}

	content, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatalf("reading output file: %v", err)
	}

	if !strings.Contains(string(content), "import {") {
		t.Error("expected import block in output file")
	}
	if !strings.Contains(string(content), `id = "i-123"`) {
		t.Error("expected import ID in output file")
	}

	// Verify filename format: migration.<stem>.<hash>.tf
	base := filepath.Base(files[0])
	if !strings.HasPrefix(base, "migration.001_move.") {
		t.Errorf("expected filename to start with 'migration.001_move.', got %q", base)
	}
	if !strings.HasSuffix(base, ".tf") {
		t.Errorf("expected filename to end with '.tf', got %q", base)
	}
}

func TestWriter_WriteAll_DryRun(t *testing.T) {
	w := NewWriter()
	w.DryRun = true
	w.AddBlock(&ImportBlock{
		To:     "aws_instance.web",
		ID:     "i-123",
		Layer:  "/nonexistent/layer",
		Source: "001.yaml",
	})

	files, err := w.WriteAll()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file path, got %d", len(files))
	}

	// File should not actually exist
	if _, err := os.Stat(files[0]); !os.IsNotExist(err) {
		t.Error("expected file to not exist in dry-run mode")
	}
}

func TestWriter_WriteAll_ConsistentHash(t *testing.T) {
	dir := t.TempDir()
	layerDir := filepath.Join(dir, "layers", "app")
	if err := os.MkdirAll(layerDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Run twice with same blocks — should produce same filename
	var filenames [2]string
	for i := range 2 {
		w := NewWriter()
		w.AddBlock(&ImportBlock{To: "aws_instance.web", ID: "i-123", Layer: layerDir, Source: "001.yaml"})
		files, err := w.WriteAll()
		if err != nil {
			t.Fatalf("run %d: unexpected error: %v", i, err)
		}
		filenames[i] = filepath.Base(files[0])
	}

	if filenames[0] != filenames[1] {
		t.Errorf("expected consistent filenames across runs, got %q and %q", filenames[0], filenames[1])
	}
}

func TestOutputFilename(t *testing.T) {
	content := "# test content\n"
	name := outputFilename("/path/to/001_move.yaml", content)

	if !strings.HasPrefix(name, "migration.001_move.") {
		t.Errorf("expected prefix 'migration.001_move.', got %q", name)
	}
	if !strings.HasSuffix(name, ".tf") {
		t.Errorf("expected suffix '.tf', got %q", name)
	}

	// SHA256 hash should be 8 hex chars
	parts := strings.Split(name, ".")
	if len(parts) != 4 {
		t.Fatalf("expected 4 parts in filename, got %d: %q", len(parts), name)
	}
	hash := parts[2]
	if len(hash) != 8 {
		t.Errorf("expected 8-char hash, got %q (%d chars)", hash, len(hash))
	}
}

func TestYamlStem(t *testing.T) {
	tests := []struct {
		path     string
		expected string
	}{
		{"/path/to/001_move.yaml", "001_move"},
		{"002_rename.yml", "002_rename"},
		{"simple.yaml", "simple"},
		{"noext", "noext"},
	}

	for _, tt := range tests {
		result := yamlStem(tt.path)
		if result != tt.expected {
			t.Errorf("yamlStem(%q) = %q, expected %q", tt.path, result, tt.expected)
		}
	}
}

func TestWriter_HasBlocks_Empty(t *testing.T) {
	w := NewWriter()
	if w.HasBlocks() {
		t.Error("expected HasBlocks() to return false for empty writer")
	}
}

func TestWriter_HasBlocks_AfterAdd(t *testing.T) {
	w := NewWriter()
	w.AddBlock(&ImportBlock{To: "aws_instance.web", ID: "i-123", Layer: "./app", Source: "001.yaml"})
	if !w.HasBlocks() {
		t.Error("expected HasBlocks() to return true after adding a block")
	}
}

func TestWriter_AddBlocks(t *testing.T) {
	w := NewWriter()
	blocks := []Block{
		&ImportBlock{To: "aws_instance.web", ID: "i-123", Layer: "./app", Source: "001.yaml"},
		&ImportBlock{To: "aws_instance.api", ID: "i-456", Layer: "./app", Source: "001.yaml"},
		&RemovedBlock{From: "aws_instance.web", Destroy: false, Layer: "./compute", Source: "001.yaml"},
	}
	w.AddBlocks(blocks)

	if !w.HasBlocks() {
		t.Error("expected HasBlocks() to return true after AddBlocks")
	}

	rendered := w.RenderAll()
	if len(rendered) != 2 {
		t.Errorf("expected 2 output files (2 layers), got %d", len(rendered))
	}
}

func TestWriter_AddBlocks_Empty(t *testing.T) {
	w := NewWriter()
	w.AddBlocks(nil)
	if w.HasBlocks() {
		t.Error("expected HasBlocks() to return false after adding nil blocks")
	}

	w.AddBlocks([]Block{})
	if w.HasBlocks() {
		t.Error("expected HasBlocks() to return false after adding empty blocks")
	}
}

func TestWriter_SetFileMetadata(t *testing.T) {
	w := NewWriter()

	meta := &MigrationMetadata{
		Conditions: &MetadataCondition{
			ResourcesExist: []MetadataResourceCheck{
				{Layer: ".", Addresses: []string{"aws_instance.web"}},
			},
		},
	}
	w.SetFileMetadata("001.yaml", meta)

	// Add a block from the same source file to verify metadata is used
	w.AddBlock(&ImportBlock{
		To:     "aws_instance.web",
		ID:     "i-123",
		Layer:  t.TempDir(),
		Source: "001.yaml",
	})

	rendered := w.RenderAll()
	for _, content := range rendered {
		if !strings.Contains(content, "tfmigrate:metadata:begin") {
			t.Error("expected metadata comment in rendered output")
		}
		if !strings.Contains(content, "resources_exist") {
			t.Error("expected resources_exist condition in metadata")
		}
	}
}

func TestWriter_SetFileMetadata_NilIgnored(t *testing.T) {
	w := NewWriter()
	w.SetFileMetadata("001.yaml", nil)

	// Should not panic or store anything
	w.AddBlock(&ImportBlock{
		To:     "aws_instance.web",
		ID:     "i-123",
		Layer:  t.TempDir(),
		Source: "001.yaml",
	})

	rendered := w.RenderAll()
	if len(rendered) != 1 {
		t.Fatalf("expected 1 output file, got %d", len(rendered))
	}
}

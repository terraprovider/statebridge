package testutil

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// SetupLayers creates a temporary directory with subdirectories for each named
// layer under "<tmpdir>/layers/<name>". It returns the base temp directory and
// a map of layer name → absolute path.
func SetupLayers(t *testing.T, names ...string) (dir string, layers map[string]string) {
	t.Helper()
	dir = t.TempDir()
	layers = make(map[string]string, len(names))
	for _, name := range names {
		p := filepath.Join(dir, "layers", name)
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatalf("creating layer dir %q: %v", name, err)
		}
		layers[name] = p
	}
	return dir, layers
}

// WriteMigration writes a YAML migration file to the given directory and
// returns the full path to the written file.
func WriteMigration(t *testing.T, dir, filename, content string) string {
	t.Helper()
	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing migration file %s: %v", filename, err)
	}
	return path
}

// FindLayerFile returns the first file path whose directory matches the given
// layer path. Returns empty string if not found.
func FindLayerFile(files []string, layer string) string {
	for _, f := range files {
		if strings.HasPrefix(f, layer+string(filepath.Separator)) || strings.HasPrefix(f, layer+"/") {
			return f
		}
	}
	return ""
}

// ReadLayerFile reads the content of the output file belonging to the given
// layer. Fatals if no matching file is found.
func ReadLayerFile(t *testing.T, files []string, layer string) string {
	t.Helper()
	f := FindLayerFile(files, layer)
	if f == "" {
		t.Fatalf("no output file found for layer %q in %v", layer, files)
	}
	content, err := os.ReadFile(f)
	if err != nil {
		t.Fatalf("reading output file %q: %v", f, err)
	}
	return string(content)
}

// ReadFirstOutput reads the content of the first output file. Fatals if there
// are no output files.
func ReadFirstOutput(t *testing.T, outputFiles []string) string {
	t.Helper()
	if len(outputFiles) == 0 {
		t.Fatal("no output files to read")
	}
	content, err := os.ReadFile(outputFiles[0])
	if err != nil {
		t.Fatalf("reading output file: %v", err)
	}
	return string(content)
}

// AssertContains checks that content contains substr, calling t.Errorf on failure.
func AssertContains(t *testing.T, content, substr string) {
	t.Helper()
	if !strings.Contains(content, substr) {
		t.Errorf("expected content to contain %q, got:\n%s", substr, content)
	}
}

// AssertNotContains checks that content does NOT contain substr.
func AssertNotContains(t *testing.T, content, substr string) {
	t.Helper()
	if strings.Contains(content, substr) {
		t.Errorf("expected content NOT to contain %q, got:\n%s", substr, content)
	}
}

// AssertBlockCount checks that the given block type (e.g. "removed {",
// "import {", "moved {") appears exactly n times in content.
// It matches at the start of each line (after trimming whitespace) to avoid
// false positives (e.g. "moved {" matching inside "removed {").
func AssertBlockCount(t *testing.T, content, blockType string, expected int) {
	t.Helper()
	actual := 0
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(strings.TrimLeft(line, " \t"), blockType) {
			actual++
		}
	}
	if actual != expected {
		t.Errorf("expected %d %q blocks, got %d in:\n%s", expected, blockType, actual, content)
	}
}

// RequireOutputCount asserts that the number of output files equals n.
func RequireOutputCount(t *testing.T, outputFiles []string, n int) {
	t.Helper()
	if len(outputFiles) != n {
		t.Fatalf("expected %d output file(s), got %d: %v", n, len(outputFiles), outputFiles)
	}
}

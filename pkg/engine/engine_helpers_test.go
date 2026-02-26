package engine

import (
"os"
"path/filepath"
"strings"
"testing"
)

// findLayerFile returns the first output file path whose directory matches the
// given layer. Returns empty string if not found.
func findLayerFile(files []string, layer string) string {
	for _, f := range files {
		if strings.HasPrefix(f, layer+string(filepath.Separator)) || strings.HasPrefix(f, layer+"/") {
			return f
		}
	}
	return ""
}

// readLayerFile reads the content of the output file in the given layer.
func readLayerFile(t *testing.T, files []string, layer string) string {
	t.Helper()
	f := findLayerFile(files, layer)
	if f == "" {
		t.Fatalf("no output file found for layer %q in %v", layer, files)
	}
	content, err := os.ReadFile(f)
	if err != nil {
		t.Fatalf("reading output file %q: %v", f, err)
	}
	return string(content)
}

//go:build e2e

package e2e

import (
	"os"
	"path/filepath"
	"testing"
)

// setupTestProject copies the testproject to a temp directory, returns the
// root directory and a cleanup function that destroys resources.
func setupTestProject(t *testing.T) (rootDir string, prefix string, vars map[string]string) {
	t.Helper()

	location := os.Getenv("E2E_LOCATION")
	if location == "" {
		location = "westeurope"
	}

	prefix = uniquePrefix(t)
	rootDir = t.TempDir()

	// Find testproject relative to this test file
	testprojectSrc := filepath.Join(testdataDir(), "testproject")
	copyDir(t, testprojectSrc, filepath.Join(rootDir, "testproject"))
	rootDir = filepath.Join(rootDir, "testproject")

	vars = map[string]string{
		"prefix":   prefix,
		"location": location,
	}

	return rootDir, prefix, vars
}

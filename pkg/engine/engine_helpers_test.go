package engine

import (
	"context"
	"testing"

	"github.com/terraprovider/statebridge/internal/testutil"
)

// readLayerFile delegates to testutil.ReadLayerFile.
func readLayerFile(t *testing.T, files []string, layer string) string {
	t.Helper()
	return testutil.ReadLayerFile(t, files, layer)
}

// runEngine creates an Engine with the given config, processes the migration
// files, and returns the result. Fatals on error.
func runEngine(t *testing.T, cfg Config, migrationFiles []string) *ProcessResult {
	t.Helper()
	eng := New(cfg)
	result, err := eng.ProcessFiles(context.Background(), migrationFiles)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return result
}

// runEngineExpectError creates an Engine with the given config, processes
// the migration files, and returns the error. Fatals if no error occurs.
func runEngineExpectError(t *testing.T, cfg Config, migrationFiles []string) error {
	t.Helper()
	eng := New(cfg)
	_, err := eng.ProcessFiles(context.Background(), migrationFiles)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	return err
}

// assertAllSkippedWithError asserts that processing produced no output files
// and that filePath was skipped due to a processing error (SkipError). This
// is the non-fatal outcome ("Warning: all migration files were skipped, no
// output generated") when every migration file in a run ends up skipped due
// to an error, such as a referenced resource not existing in state.
func assertAllSkippedWithError(t *testing.T, result *ProcessResult, filePath string) {
	t.Helper()
	if len(result.OutputFiles) != 0 {
		t.Errorf("expected no output files, got: %v", result.OutputFiles)
	}
	for _, sf := range result.SkippedFiles {
		if sf.FilePath == filePath && sf.Reason == SkipError {
			return
		}
	}
	t.Errorf("expected %q to be skipped with SkipError, got: %+v", filePath, result.SkippedFiles)
}

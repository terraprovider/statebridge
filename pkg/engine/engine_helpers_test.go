package engine

import (
	"context"
	"testing"

	"github.com/redtenant/tfmigrate/internal/testutil"
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

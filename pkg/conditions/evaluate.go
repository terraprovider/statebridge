// Package conditions provides shared condition evaluation logic for
// migration metadata. Both the upload guard and the download command use
// this to determine whether a migration's conditions are met against a
// layer's state.
package conditions

import (
	"context"
	"fmt"
	"os"

	tfjson "github.com/hashicorp/terraform-json"

	"github.com/redtenant/tfmigrate/pkg/generator"
	"github.com/redtenant/tfmigrate/pkg/state"
)

// StateReaderFunc is a lazy factory for reading state for a given layer
// directory. It allows callers to defer state reading until actually needed.
type StateReaderFunc func(ctx context.Context, layerDir string) (*tfjson.State, error)

// NewStateReaderFunc creates a StateReaderFunc from a state.StateReader.
func NewStateReaderFunc(reader state.StateReader) StateReaderFunc {
	return func(ctx context.Context, layerDir string) (*tfjson.State, error) {
		return reader.ReadState(ctx, layerDir)
	}
}

// EvaluateMetadataConditions checks whether a migration's metadata conditions
// are met against the given layer's state. Returns true if all conditions pass
// (migration is still active/needed), false if any condition fails.
//
// Cross-layer conditions (layer != ".") cannot be evaluated in the context of
// a single layer, so they are treated as met with a warning to stderr.
//
// Returns (true, nil) if there are no conditions to evaluate.
func EvaluateMetadataConditions(
	ctx context.Context,
	meta *generator.MigrationMetadata,
	readState StateReaderFunc,
	layerDir string,
) (bool, error) {
	if meta == nil || meta.Conditions == nil {
		return true, nil
	}

	cond := meta.Conditions

	for _, check := range cond.ResourcesExist {
		if check.Layer != "." {
			fmt.Fprintf(os.Stderr, "Warning: cross-layer condition (layer=%q) cannot be evaluated, treating as met\n", check.Layer)
			continue
		}

		s, err := readState(ctx, layerDir)
		if err != nil {
			return false, fmt.Errorf("reading state for %q: %w", layerDir, err)
		}

		for _, addr := range check.Addresses {
			if !state.ResourceExists(s, addr) {
				return false, nil
			}
		}
	}

	for _, check := range cond.ResourcesNotExist {
		if check.Layer != "." {
			fmt.Fprintf(os.Stderr, "Warning: cross-layer condition (layer=%q) cannot be evaluated, treating as met\n", check.Layer)
			continue
		}

		s, err := readState(ctx, layerDir)
		if err != nil {
			return false, fmt.Errorf("reading state for %q: %w", layerDir, err)
		}

		for _, addr := range check.Addresses {
			if state.ResourceExists(s, addr) {
				return false, nil
			}
		}
	}

	return true, nil
}

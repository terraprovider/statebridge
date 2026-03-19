// Package conditions provides shared condition evaluation logic for
// migration metadata. Both the upload guard and the download command use
// this to determine whether a migration's conditions are met against a
// layer's state.
package conditions

import (
	"context"
	"errors"
	"fmt"
	"os"

	tfjson "github.com/hashicorp/terraform-json"

	"github.com/terraprovider/statebridge/pkg/generator"
	"github.com/terraprovider/statebridge/pkg/state"
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

	// Layer existence conditions — cheap checks, no state reading needed.
	for _, path := range cond.LayerExists {
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			return false, nil
		} else if err != nil {
			return false, fmt.Errorf("checking layer %q: %w", path, err)
		}
	}
	for _, path := range cond.LayerNotExists {
		if _, err := os.Stat(path); err == nil {
			return false, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return false, fmt.Errorf("checking layer %q: %w", path, err)
		}
	}

	// Check whether any resource conditions reference the local layer.
	needsState := false
	for _, check := range cond.ResourcesExist {
		if check.Layer == "." {
			needsState = true
			break
		}
	}
	if !needsState {
		for _, check := range cond.ResourcesNotExist {
			if check.Layer == "." {
				needsState = true
				break
			}
		}
	}

	var idx *state.StateIndex
	if needsState {
		if readState == nil {
			return false, fmt.Errorf("state evaluation required for %q but no state reader available", layerDir)
		}
		s, err := readState(ctx, layerDir)
		if err != nil {
			return false, fmt.Errorf("reading state for %q: %w", layerDir, err)
		}
		idx = state.NewStateIndex(s)
	}

	for _, check := range cond.ResourcesExist {
		if check.Layer != "." {
			fmt.Fprintf(os.Stderr, "Warning: cross-layer condition (layer=%q) cannot be evaluated, treating as met\n", check.Layer)
			continue
		}

		for _, addr := range check.Addresses {
			if !idx.ResourceExists(addr) {
				return false, nil
			}
		}
	}

	for _, check := range cond.ResourcesNotExist {
		if check.Layer != "." {
			fmt.Fprintf(os.Stderr, "Warning: cross-layer condition (layer=%q) cannot be evaluated, treating as met\n", check.Layer)
			continue
		}

		for _, addr := range check.Addresses {
			if idx.ResourceExists(addr) {
				return false, nil
			}
		}
	}

	return true, nil
}

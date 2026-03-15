package state

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/hashicorp/terraform-exec/tfexec"
	tfjson "github.com/hashicorp/terraform-json"
)

// StateReader reads Terraform/OpenTofu state for a given layer directory.
type StateReader interface {
	// ReadState returns the full parsed state for the Terraform root module
	// at the given directory path. Implementations may call `tofu show -json`
	// or equivalent commands.
	ReadState(ctx context.Context, layerPath string) (*tfjson.State, error)
}

// TofuStateReader implements StateReader using the OpenTofu (tofu) binary.
// It includes built-in per-layer caching to avoid redundant state reads, and
// optional auto-init: if initArgs are provided and a state read fails, it
// runs `tofu init` and retries once.
type TofuStateReader struct {
	tofuPath    string
	initArgs    []string
	cache       map[string]*tfjson.State
	initialized map[string]bool
}

// NewTofuStateReader creates a TofuStateReader with the given tofu binary path
// and optional init arguments (e.g., ["-backend-config=key=value"]).
// If initArgs is non-empty, a failed state read will trigger `tofu init`
// before retrying.
func NewTofuStateReader(tofuPath string, initArgs []string) *TofuStateReader {
	return &TofuStateReader{
		tofuPath:    tofuPath,
		initArgs:    initArgs,
		cache:       make(map[string]*tfjson.State),
		initialized: make(map[string]bool),
	}
}

// NewTofuStateReaderFromPath creates a TofuStateReader, discovering the tofu
// binary via PATH. Returns an error if tofu is not found.
func NewTofuStateReaderFromPath(initArgs []string) (*TofuStateReader, error) {
	tofuPath, err := exec.LookPath("tofu")
	if err != nil {
		return nil, fmt.Errorf("tofu binary not found in PATH: %w", err)
	}
	return NewTofuStateReader(tofuPath, initArgs), nil
}

// TofuPath returns the resolved path to the tofu binary.
func (r *TofuStateReader) TofuPath() string {
	return r.tofuPath
}

// ReadState runs `tofu show -json` in the given layer directory and returns
// the parsed state. Results are cached per absolute layer path. If the read
// fails and initArgs were provided, `tofu init` is run and the read retried.
func (r *TofuStateReader) ReadState(ctx context.Context, layerPath string) (*tfjson.State, error) {
	absPath, err := filepath.Abs(layerPath)
	if err != nil {
		return nil, fmt.Errorf("resolving layer path %q: %w", layerPath, err)
	}

	// Return cached result if available.
	if s, ok := r.cache[absPath]; ok {
		return s, nil
	}

	// Check that the layer directory actually exists before attempting tofu operations.
	if _, err := os.Stat(absPath); errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("layer directory %q does not exist", absPath)
	}

	tf, err := tfexec.NewTerraform(absPath, r.tofuPath)
	if err != nil {
		return nil, fmt.Errorf("initializing tofu for layer %q: %w", absPath, err)
	}

	s, err := tf.Show(ctx)
	if err != nil {
		// If init args are configured and we haven't tried init yet, do so.
		if len(r.initArgs) > 0 && !r.initialized[absPath] {
			if initErr := r.runInit(ctx, tf, absPath); initErr != nil {
				return nil, fmt.Errorf("tofu init failed for %q: %w", layerPath, initErr)
			}
			r.initialized[absPath] = true

			s, err = tf.Show(ctx)
			if err != nil {
				return nil, fmt.Errorf("state read failed after init for %q: %w", layerPath, err)
			}
		} else {
			return nil, fmt.Errorf("reading state for %q: %w", layerPath, err)
		}
	}

	r.cache[absPath] = s
	return s, nil
}

// runInit executes `tofu init` with the configured backend-config arguments.
func (r *TofuStateReader) runInit(ctx context.Context, tf *tfexec.Terraform, absPath string) error {
	tf.SetStdout(os.Stderr)
	tf.SetStderr(os.Stderr)

	var opts []tfexec.InitOption
	for _, arg := range r.initArgs {
		if strings.HasPrefix(arg, "-backend-config=") {
			opts = append(opts, tfexec.BackendConfig(strings.TrimPrefix(arg, "-backend-config=")))
		}
	}

	fmt.Fprintf(os.Stderr, "Running tofu init in %s\n", absPath)
	return tf.Init(ctx, opts...)
}

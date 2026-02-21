package state

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tfexec "github.com/hashicorp/terraform-exec/tfexec"
	tfjson "github.com/hashicorp/terraform-json"
)

// InitStateReader wraps another StateReader and automatically runs
// `tofu init` when a state read fails, then retries. This handles the
// common CI case where layer backends are not pre-initialized.
type InitStateReader struct {
	inner       StateReader
	tofuPath    string
	initArgs    []string
	initialized map[string]bool
}

// NewInitStateReader creates a reader that automatically initializes layers
// on state read failure. The tofuPath is the path to the tofu binary.
// initArgs are extra arguments passed to `tofu init` (e.g., -backend-config, -reconfigure).
func NewInitStateReader(inner StateReader, tofuPath string, initArgs []string) *InitStateReader {
	return &InitStateReader{
		inner:       inner,
		tofuPath:    tofuPath,
		initArgs:    initArgs,
		initialized: make(map[string]bool),
	}
}

// ReadState attempts to read Terraform state. If the read fails and the layer
// has not been initialized yet, it runs `tofu init` and retries once.
func (r *InitStateReader) ReadState(ctx context.Context, layerPath string) (*tfjson.State, error) {
	s, err := r.inner.ReadState(ctx, layerPath)
	if err == nil {
		return s, nil
	}

	absPath, pathErr := filepath.Abs(layerPath)
	if pathErr != nil {
		return nil, fmt.Errorf("resolving layer path %q: %w", layerPath, pathErr)
	}

	if r.initialized[absPath] {
		return nil, fmt.Errorf("state read failed after init for layer %q: %w", layerPath, err)
	}

	if initErr := r.runInit(ctx, absPath); initErr != nil {
		return nil, fmt.Errorf("tofu init failed for layer %q: %w", layerPath, initErr)
	}
	r.initialized[absPath] = true

	s, retryErr := r.inner.ReadState(ctx, layerPath)
	if retryErr != nil {
		return nil, fmt.Errorf("state read failed after init for layer %q: %w", layerPath, retryErr)
	}
	return s, nil
}

// runInit executes `tofu init` with the configured arguments in the given directory.
func (r *InitStateReader) runInit(ctx context.Context, absPath string) error {
	tf, err := tfexec.NewTerraform(absPath, r.tofuPath)
	if err != nil {
		return fmt.Errorf("initializing terraform-exec: %w", err)
	}
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

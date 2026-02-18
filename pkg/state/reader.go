package state

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"

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
type TofuStateReader struct {
	// TofuPath is the absolute path to the tofu binary.
	TofuPath string
}

// NewTofuStateReader creates a TofuStateReader, discovering the tofu binary via PATH.
func NewTofuStateReader() (*TofuStateReader, error) {
	tofuPath, err := exec.LookPath("tofu")
	if err != nil {
		return nil, fmt.Errorf("tofu binary not found in PATH: %w", err)
	}
	return &TofuStateReader{TofuPath: tofuPath}, nil
}

// NewTofuStateReaderWithPath creates a TofuStateReader with an explicit binary path.
func NewTofuStateReaderWithPath(tofuPath string) *TofuStateReader {
	return &TofuStateReader{TofuPath: tofuPath}
}

// ReadState runs `tofu show -json` in the given layer directory and returns
// the parsed state. The layer must already be initialized (tofu init).
func (r *TofuStateReader) ReadState(ctx context.Context, layerPath string) (*tfjson.State, error) {
	absPath, err := filepath.Abs(layerPath)
	if err != nil {
		return nil, fmt.Errorf("resolving layer path %q: %w", layerPath, err)
	}

	tf, err := tfexec.NewTerraform(absPath, r.TofuPath)
	if err != nil {
		return nil, fmt.Errorf("initializing tofu for layer %q: %w", absPath, err)
	}

	state, err := tf.Show(ctx)
	if err != nil {
		return nil, fmt.Errorf("reading state for layer %q: %w", absPath, err)
	}

	return state, nil
}

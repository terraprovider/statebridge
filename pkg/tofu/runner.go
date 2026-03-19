// Package tofu provides utilities for running OpenTofu/Terraform commands
// and scanning migration files for target resource addresses.
package tofu

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	tfexec "github.com/hashicorp/terraform-exec/tfexec"
	"github.com/terraprovider/statebridge/pkg/generator"
)

// Runner executes tofu commands in a working directory.
type Runner struct {
	tf *tfexec.Terraform
}

// PlanOpts holds typed options for the plan command, replacing raw extra args.
type PlanOpts struct {
	Out         string
	Vars        []string
	VarFiles    []string
	Lock        *bool
	LockTimeout string
}

// NewRunner creates a Runner backed by terraform-exec for the given tofu
// binary and working directory.
func NewRunner(tofuPath, workDir string) (*Runner, error) {
	tf, err := tfexec.NewTerraform(workDir, tofuPath)
	if err != nil {
		return nil, fmt.Errorf("initializing terraform-exec: %w", err)
	}
	tf.SetStdout(os.Stdout)
	tf.SetStderr(os.Stderr)
	return &Runner{tf: tf}, nil
}

// Plan runs tofu plan with optional -target flags and typed options.
// Returns true if the plan detects changes, false otherwise.
func (r *Runner) Plan(ctx context.Context, targets []string, opts PlanOpts) (bool, error) {
	var planOpts []tfexec.PlanOption
	for _, t := range targets {
		planOpts = append(planOpts, tfexec.Target(t))
	}
	if opts.Out != "" {
		planOpts = append(planOpts, tfexec.Out(opts.Out))
	}
	for _, v := range opts.Vars {
		planOpts = append(planOpts, tfexec.Var(v))
	}
	for _, vf := range opts.VarFiles {
		planOpts = append(planOpts, tfexec.VarFile(vf))
	}
	if opts.Lock != nil {
		planOpts = append(planOpts, tfexec.Lock(*opts.Lock))
	}
	if opts.LockTimeout != "" {
		planOpts = append(planOpts, tfexec.LockTimeout(opts.LockTimeout))
	}
	return r.tf.Plan(ctx, planOpts...)
}

// ScanMigrationTargets scans a directory for migration.*.tf files, parses
// metadata from each, and returns the deduplicated sorted list of resource
// addresses. This is used by the plan command to determine -target flags.
func ScanMigrationTargets(dir string) ([]string, error) {
	pattern := filepath.Join(dir, "migration.*.tf")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("scanning migration files in %q: %w", dir, err)
	}

	seen := make(map[string]bool)
	for _, match := range matches {
		content, err := os.ReadFile(match)
		if err != nil {
			return nil, fmt.Errorf("reading %q: %w", match, err)
		}

		meta, err := generator.ParseMetadataComment(string(content))
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not parse metadata in %q: %v\n", match, err)
			continue
		}
		if meta == nil {
			continue
		}

		for _, addr := range meta.Resources {
			seen[addr] = true
		}
	}

	addrs := make([]string, 0, len(seen))
	for addr := range seen {
		addrs = append(addrs, addr)
	}
	sort.Strings(addrs)
	return addrs, nil
}

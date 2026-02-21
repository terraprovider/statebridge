// Package tofu provides utilities for running OpenTofu/Terraform commands
// and scanning migration files for target resource addresses.
package tofu

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"

	"github.com/redtenant/tfmigrate/pkg/generator"
)

// Runner executes tofu commands in a working directory.
type Runner struct {
	tofuPath string
	workDir  string
}

// NewRunner creates a Runner for the given tofu binary and working directory.
func NewRunner(tofuPath, workDir string) *Runner {
	return &Runner{tofuPath: tofuPath, workDir: workDir}
}

// Plan runs tofu plan with optional -target flags.
// Stdout and stderr are streamed directly to the terminal.
func (r *Runner) Plan(ctx context.Context, targets []string, extraArgs []string) error {
	args := []string{"plan"}
	for _, t := range targets {
		args = append(args, fmt.Sprintf("-target=%s", t))
	}
	args = append(args, extraArgs...)
	return r.run(ctx, args)
}

// Apply runs tofu apply -auto-approve with optional -target flags.
// Stdout and stderr are streamed directly to the terminal.
func (r *Runner) Apply(ctx context.Context, targets []string, extraArgs []string) error {
	args := []string{"apply", "-auto-approve"}
	for _, t := range targets {
		args = append(args, fmt.Sprintf("-target=%s", t))
	}
	args = append(args, extraArgs...)
	return r.run(ctx, args)
}

// run executes a tofu command with the given arguments, streaming stdout/stderr.
func (r *Runner) run(ctx context.Context, args []string) error {
	cmd := exec.CommandContext(ctx, r.tofuPath, args...)
	cmd.Dir = r.workDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("tofu %s: %w", args[0], err)
	}
	return nil
}

// ScanMigrationTargets scans a directory for migration.*.tf files, parses
// metadata from each, and returns the deduplicated sorted list of resource
// addresses. This is used by plan and apply commands to determine -target flags.
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

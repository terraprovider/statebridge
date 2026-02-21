// Package tofu provides utilities for running OpenTofu/Terraform commands
// and scanning migration files for target resource addresses.
package tofu

import (
	"context"
	"errors"
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
// Returns the tofu exit code and any execution error.
func (r *Runner) Plan(ctx context.Context, targets []string, extraArgs []string) (int, error) {
	args := []string{"plan"}
	for _, t := range targets {
		args = append(args, fmt.Sprintf("-target=%s", t))
	}
	args = append(args, extraArgs...)
	return r.run(ctx, args)
}

// run executes a tofu command with the given arguments, streaming stdout/stderr.
// Returns the process exit code and any error. If tofu exits with a non-zero
// code, the exit code is returned with a nil error (the caller decides how to
// handle it). A non-nil error indicates a failure to start the process.
func (r *Runner) run(ctx context.Context, args []string) (int, error) {
	cmd := exec.CommandContext(ctx, r.tofuPath, args...)
	cmd.Dir = r.workDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return exitErr.ExitCode(), nil
		}
		return 1, fmt.Errorf("tofu %s: %w", args[0], err)
	}
	return 0, nil
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

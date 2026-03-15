// Package cmd implements the CLI commands for tfmigrate using cobra.
package cmd

import (
	"errors"
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"

	"github.com/redtenant/tfmigrate/pkg/auth"
	"github.com/redtenant/tfmigrate/pkg/upload"
)

// ExitCodeError wraps an exit code so that cobra RunE handlers can propagate
// the exact exit code from an external process (e.g., tofu --detailed-exitcode).
type ExitCodeError struct {
	Code int
}

func (e *ExitCodeError) Error() string {
	return fmt.Sprintf("exit code %d", e.Code)
}

// rootCmd represents the base command when called without any subcommands.
var rootCmd = &cobra.Command{
	Use:   "tfmigrate",
	Short: "Generate OpenTofu HCL migration code from declarative YAML files",
	Long: `tfmigrate is a declarative code generator for OpenTofu state migrations.

It reads YAML migration files that describe resource moves, renames, imports,
and removals, then generates the corresponding HCL code (import, moved, removed
blocks) in the appropriate layer directories.

Use cases:
  - Move resources between Terraform/OpenTofu layers (root modules)
  - Rename resources or modules within a layer
  - Remove resources from state without destroying infrastructure
  - Import existing cloud resources into state
  - Re-key for_each resources with complex key transformations`,
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		var exitErr *ExitCodeError
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.Code)
		}
		os.Exit(1)
	}
}

// buildInitArgs converts --backend-config flag values into tofu init arguments.
func buildInitArgs(backendConfigs []string) []string {
	var args []string
	for _, bc := range backendConfigs {
		args = append(args, "-backend-config="+bc)
	}
	return args
}

// resolveTofuPath returns the tofu binary path from a flag or PATH lookup.
func resolveTofuPath(flagPath string) (string, error) {
	if flagPath != "" {
		return flagPath, nil
	}
	path, err := exec.LookPath("tofu")
	if err != nil {
		return "", fmt.Errorf("tofu binary not found in PATH; use --tofu-path to specify it")
	}
	return path, nil
}

// createUploaderFactory builds a UploaderFactory that handles all backends.
// Azure credentials are resolved lazily: only when an azurerm backend is
// encountered. S3/GCS use their native SDK credential chains; local needs
// no credentials.
func createUploaderFactory() (upload.UploaderFactory, error) {
	credCfg, err := auth.NewCredentialConfiguration(
		auth.WithDefaultEnvironmentVariables(),
	)
	if err != nil {
		return nil, fmt.Errorf("configuring credentials: %w", err)
	}

	cred, err := credCfg.TokenCredential()
	if err != nil {
		// Azure cred failed — create a factory that works for non-Azure
		// backends but returns a clear error for azurerm.
		return upload.BucketUploaderFactory(nil), nil
	}

	return upload.BucketUploaderFactory(cred), nil
}

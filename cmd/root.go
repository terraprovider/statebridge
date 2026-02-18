// Package cmd implements the CLI commands for tfmigrate using cobra.
package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

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
		os.Exit(1)
	}
}

package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/redtenant/tfmigrate/pkg/tofu"
)

var (
	flagApplyNoTarget bool
	flagApplyTofuPath string
)

// applyCmd represents the apply command.
var applyCmd = &cobra.Command{
	Use:   "apply [-- extra-tofu-args...]",
	Short: "Run tofu apply targeted to migration resources",
	Long: `Run tofu apply -auto-approve scoped to the resources touched by migration
files in the current directory. By default, only resources listed in migration
metadata are targeted with -target flags. Use --no-target for a full apply.

Migration files (migration.*.tf) must be present in the current directory,
typically placed there by the download command.

Extra arguments can be passed to tofu after --:

Examples:
  # Targeted apply (default)
  tfmigrate apply

  # Full apply without targeting
  tfmigrate apply --no-target

  # Pass extra flags to tofu
  tfmigrate apply -- -var="env=prod"`,
	Args: cobra.ArbitraryArgs,
	RunE: runApply,
}

func init() {
	rootCmd.AddCommand(applyCmd)

	applyCmd.Flags().BoolVar(&flagApplyNoTarget, "no-target", false,
		"Run tofu apply without -target flags (full apply)")
	applyCmd.Flags().StringVar(&flagApplyTofuPath, "tofu-path", "",
		"Override path to the tofu binary (default: auto-detect from PATH)")
}

func runApply(cmd *cobra.Command, args []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting current directory: %w", err)
	}

	tofuPath, err := resolveTofuPath(flagApplyTofuPath)
	if err != nil {
		return err
	}

	targets, err := tofu.ScanMigrationTargets(cwd)
	if err != nil {
		return fmt.Errorf("scanning migration targets: %w", err)
	}

	if len(targets) == 0 {
		return fmt.Errorf("no migration files with metadata found in current directory")
	}

	if !flagApplyNoTarget {
		fmt.Fprintf(os.Stderr, "Applying with %d target(s):\n", len(targets))
		for _, t := range targets {
			fmt.Fprintf(os.Stderr, "  -target=%s\n", t)
		}
		fmt.Fprintln(os.Stderr)
	} else {
		targets = nil
		fmt.Fprintln(os.Stderr, "Applying without target scoping (--no-target)")
		fmt.Fprintln(os.Stderr)
	}

	var extraArgs []string
	if dashIdx := cmd.ArgsLenAtDash(); dashIdx >= 0 {
		extraArgs = args[dashIdx:]
	}

	runner := tofu.NewRunner(tofuPath, cwd)
	ctx := context.Background()

	return runner.Apply(ctx, targets, extraArgs)
}

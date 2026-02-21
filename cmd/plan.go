package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"

	"github.com/redtenant/tfmigrate/pkg/tofu"
)

var (
	flagPlanNoTarget bool
	flagPlanTofuPath string
)

// planCmd represents the plan command.
var planCmd = &cobra.Command{
	Use:   "plan [-- extra-tofu-args...]",
	Short: "Run tofu plan targeted to migration resources",
	Long: `Run tofu plan scoped to the resources touched by migration files in the
current directory. By default, only resources listed in migration metadata
are targeted with -target flags. Use --no-target for a full plan.

Migration files (migration.*.tf) must be present in the current directory,
typically placed there by the download command.

Extra arguments can be passed to tofu after --:

Examples:
  # Targeted plan (default)
  tfmigrate plan

  # Full plan without targeting
  tfmigrate plan --no-target

  # Pass extra flags to tofu
  tfmigrate plan -- -var="env=prod"`,
	Args: cobra.ArbitraryArgs,
	RunE: runPlan,
}

func init() {
	rootCmd.AddCommand(planCmd)

	planCmd.Flags().BoolVar(&flagPlanNoTarget, "no-target", false,
		"Run tofu plan without -target flags (full plan)")
	planCmd.Flags().StringVar(&flagPlanTofuPath, "tofu-path", "",
		"Override path to the tofu binary (default: auto-detect from PATH)")
}

func runPlan(cmd *cobra.Command, args []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting current directory: %w", err)
	}

	tofuPath, err := resolveTofuPath(flagPlanTofuPath)
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

	if !flagPlanNoTarget {
		fmt.Fprintf(os.Stderr, "Planning with %d target(s):\n", len(targets))
		for _, t := range targets {
			fmt.Fprintf(os.Stderr, "  -target=%s\n", t)
		}
		fmt.Fprintln(os.Stderr)
	} else {
		targets = nil
		fmt.Fprintln(os.Stderr, "Planning without target scoping (--no-target)")
		fmt.Fprintln(os.Stderr)
	}

	var extraArgs []string
	if dashIdx := cmd.ArgsLenAtDash(); dashIdx >= 0 {
		extraArgs = args[dashIdx:]
	}

	runner := tofu.NewRunner(tofuPath, cwd)
	ctx := context.Background()

	return runner.Plan(ctx, targets, extraArgs)
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

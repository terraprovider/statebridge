package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/terraprovider/statebridge/pkg/tofu"
)

var (
	flagPlanNoTarget         bool
	flagPlanTofuPath         string
	flagPlanDetailedExitcode bool
	flagPlanOut              string
	flagPlanVar              []string
	flagPlanVarFile          []string
	flagPlanLock             bool
	flagPlanLockTimeout      string
)

// planCmd represents the plan command.
var planCmd = &cobra.Command{
	Use:   "plan",
	Short: "Run tofu plan targeted to migration resources",
	Long: `Run tofu plan scoped to the resources touched by migration files in the
current directory. By default, only resources listed in migration metadata
are targeted with -target flags. Use --no-target for a full plan.

Migration files (migration.*.tf) must be present in the current directory,
typically placed there by the download command.

Examples:
  # Targeted plan (default)
  statebridge plan

  # Full plan without targeting
  statebridge plan --no-target

  # Save plan to file with detailed exit code
  statebridge plan --out=tfplan --detailed-exitcode

  # Pass variables and disable locking
  statebridge plan --var="env=prod" --lock=false`,
	Args: cobra.NoArgs,
	RunE: runPlan,
}

func init() {
	rootCmd.AddCommand(planCmd)

	planCmd.Flags().BoolVar(&flagPlanNoTarget, "no-target", false,
		"Run tofu plan without -target flags (full plan)")
	planCmd.Flags().StringVar(&flagPlanTofuPath, "tofu-path", "",
		"Override path to the tofu binary (default: auto-detect from PATH)")
	planCmd.Flags().BoolVar(&flagPlanDetailedExitcode, "detailed-exitcode", false,
		"Return exit code 2 when plan has changes")
	planCmd.Flags().StringVar(&flagPlanOut, "out", "",
		"Save the plan to a file")
	planCmd.Flags().StringSliceVar(&flagPlanVar, "var", nil,
		"Set a variable (key=value, can be repeated)")
	planCmd.Flags().StringSliceVar(&flagPlanVarFile, "var-file", nil,
		"Variable file path (can be repeated)")
	planCmd.Flags().BoolVar(&flagPlanLock, "lock", true,
		"Lock the state file during operations")
	planCmd.Flags().StringVar(&flagPlanLockTimeout, "lock-timeout", "",
		"Duration to retry a state lock (e.g. 30s)")
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

	if len(targets) == 0 && !flagPlanNoTarget {
		fmt.Fprintln(os.Stderr, "No migration files found, nothing to plan.")
		return nil
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

	opts := tofu.PlanOpts{
		Out:         flagPlanOut,
		Vars:        flagPlanVar,
		VarFiles:    flagPlanVarFile,
		LockTimeout: flagPlanLockTimeout,
	}
	if cmd.Flags().Changed("lock") {
		opts.Lock = &flagPlanLock
	}

	runner, err := tofu.NewRunner(tofuPath, cwd)
	if err != nil {
		return err
	}
	ctx := context.Background()

	hasChanges, err := runner.Plan(ctx, targets, opts)
	if err != nil {
		return err
	}
	if hasChanges && flagPlanDetailedExitcode {
		return &ExitCodeError{Code: 2}
	}
	return nil
}

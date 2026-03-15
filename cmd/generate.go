package cmd

import (
	"context"
	"fmt"
	"os"
	"sort"

	"github.com/spf13/cobra"

	"github.com/redtenant/tfmigrate/pkg/engine"
	"github.com/redtenant/tfmigrate/pkg/state"
	"github.com/redtenant/tfmigrate/pkg/upload"
)

var (
	flagDryRun                bool
	flagTofuPath              string
	flagUpload                bool
	flagGenerateBackendConfig []string
	flagGenerateForce         bool
	flagStrict                bool
)

// generateCmd represents the generate command.
var generateCmd = &cobra.Command{
	Use:   "generate [migration-files-or-dirs...]",
	Short: "Generate HCL migration code from YAML migration files",
	Long: `Generate OpenTofu HCL migration code (import, moved, removed blocks)
from declarative YAML migration files.

Migration files are processed in the order specified. If a directory is given,
all .yaml and .yml files in that directory are processed, sorted by filename.

Generated HCL files are written to the respective layer directories specified
in each migration operation.

Examples:
  # Process a single migration file
  tfmigrate generate migrations/001_move_compute.yaml

  # Process all migrations in a directory
  tfmigrate generate migrations/

  # Process multiple files and directories
  tfmigrate generate migrations/001_move.yaml migrations/002_rename.yaml

  # Dry run: preview generated HCL without writing files
  tfmigrate generate --dry-run migrations/

  # Generate and upload to blob storage
  tfmigrate generate --upload migrations/`,
	Args: cobra.MinimumNArgs(1),
	RunE: runGenerate,
}

func init() {
	rootCmd.AddCommand(generateCmd)

	generateCmd.Flags().BoolVar(&flagDryRun, "dry-run", false,
		"Print generated HCL to stdout instead of writing files")
	generateCmd.Flags().StringVar(&flagTofuPath, "tofu-path", "",
		"Override path to the tofu binary (default: auto-detect from PATH)")
	generateCmd.Flags().BoolVar(&flagUpload, "upload", false,
		"Upload generated files to blob storage after generation")
	generateCmd.Flags().StringSliceVar(&flagGenerateBackendConfig, "backend-config", nil,
		"Backend configuration passed to tofu init, as key=value or path to a file (repeatable)")
	generateCmd.Flags().BoolVar(&flagGenerateForce, "force", false,
		"Force upload even if existing migrations are still active (overwrite protection bypass)")
	generateCmd.Flags().BoolVar(&flagStrict, "strict", false,
		"Treat missing layer directories as errors instead of auto-skipping")
}

func runGenerate(cmd *cobra.Command, args []string) error {
	if flagUpload && flagDryRun {
		return fmt.Errorf("--upload and --dry-run cannot be used together")
	}

	// Build init args from --backend-config flags
	initArgs := buildInitArgs(flagGenerateBackendConfig)

	// Resolve the tofu binary eagerly — required for state reads.
	resolvedTofuPath, err := resolveTofuPath(flagTofuPath)
	if err != nil {
		return err
	}
	stateReader := state.NewTofuStateReader(resolvedTofuPath, initArgs)

	cfg := engine.Config{
		StateReader: stateReader,
		DryRun:      flagDryRun,
		Strict:      flagStrict,
	}

	eng := engine.New(cfg)
	ctx := context.Background()

	result, err := eng.ProcessFiles(ctx, args)
	if err != nil {
		return err
	}

	if flagDryRun {
		// In dry-run mode, print the rendered content for each output file
		w := eng.Writer()
		rendered := w.RenderAll()
		// Sort paths for deterministic output
		paths := make([]string, 0, len(rendered))
		for p := range rendered {
			paths = append(paths, p)
		}
		sort.Strings(paths)
		for _, p := range paths {
			fmt.Fprintf(os.Stdout, "# File: %s\n", p)
			fmt.Fprintln(os.Stdout, rendered[p])
		}
	} else {
		for _, f := range result.OutputFiles {
			fmt.Fprintf(os.Stdout, "Generated: %s\n", f)
		}
	}

	if flagUpload {
		if err := runUploadAfterGenerate(ctx, eng, resolvedTofuPath, result.SkippedFiles); err != nil {
			return fmt.Errorf("uploading migration files: %w", err)
		}
	}

	return nil
}

// runUploadAfterGenerate handles uploading generated files to blob storage
// after the generate pipeline completes. It also auto-prunes stale blobs for
// migration files that were retired or skipped due to missing layers.
func runUploadAfterGenerate(ctx context.Context, eng *engine.Engine, tofuPath string, skippedFiles []engine.SkippedFile) error {
	factory, err := createUploaderFactory()
	if err != nil {
		return err
	}

	// Build init args from --backend-config flags
	initArgs := buildInitArgs(flagGenerateBackendConfig)

	var opts []upload.ManagerOption
	opts = append(opts, upload.WithForce(flagGenerateForce))
	opts = append(opts, upload.WithTofuPath(tofuPath, initArgs))

	mgr := upload.NewManager(factory, initArgs, opts...)
	rendered := eng.Writer().RenderAll()

	if err := mgr.UploadRendered(ctx, rendered); err != nil {
		return err
	}

	// Auto-prune stale blobs for retired/layer-missing migrations.
	// Only prune from layers we're actively uploading to.
	var pruneStems []string
	for _, sf := range skippedFiles {
		if sf.Reason == engine.SkipRetired || sf.Reason == engine.SkipLayerMissing {
			pruneStems = append(pruneStems, sf.Stem)
		}
	}
	if len(pruneStems) > 0 && len(rendered) > 0 {
		var layerPaths []string
		for lp := range rendered {
			layerPaths = append(layerPaths, lp)
		}
		pruned, err := mgr.PruneStems(ctx, pruneStems, layerPaths)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: auto-prune failed: %v\n", err)
		} else if pruned > 0 {
			fmt.Fprintf(os.Stderr, "Auto-pruned %d stale migration blob(s)\n", pruned)
		}
	}

	return nil
}

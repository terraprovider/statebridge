package cmd

import (
	"context"
	"fmt"
	"os"
	"sort"
	"sync"

	tfjson "github.com/hashicorp/terraform-json"
	"github.com/spf13/cobra"

	"github.com/redtenant/tfmigrate/pkg/auth"
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

  # Generate and upload to Azure Blob Storage
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
		"Upload generated files to Azure Blob Storage after generation")
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

	// Resolve the tofu binary and create the state reader.
	// When --tofu-path is explicit, resolve eagerly.
	// Otherwise, defer discovery until state is actually needed so that
	// migrations that never read state (rename, remove, import with explicit
	// IDs) work even when tofu is not installed.
	var stateReader state.StateReader
	var resolvedTofuPath string

	if flagTofuPath != "" {
		resolvedTofuPath = flagTofuPath
		stateReader = state.NewTofuStateReader(flagTofuPath, initArgs)
	} else {
		lazy := newLazyStateReader(initArgs)
		stateReader = lazy
		// resolvedTofuPath will be populated after ProcessFiles if lazy resolved.
		defer func() {
			resolvedTofuPath = lazy.tofuPath()
		}()
	}

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

// runUploadAfterGenerate handles uploading generated files to Azure Blob Storage
// after the generate pipeline completes. It also auto-prunes stale blobs for
// migration files that were retired or skipped due to missing layers.
func runUploadAfterGenerate(ctx context.Context, eng *engine.Engine, tofuPath string, skippedFiles []engine.SkippedFile) error {
	credCfg, err := auth.NewCredentialConfiguration(
		auth.WithDefaultEnvironmentVariables(),
	)
	if err != nil {
		return fmt.Errorf("configuring Azure credentials: %w", err)
	}

	cred, err := credCfg.TokenCredential()
	if err != nil {
		return fmt.Errorf("creating Azure credential: %w", err)
	}

	// Build init args from --backend-config flags
	initArgs := buildInitArgs(flagGenerateBackendConfig)

	var opts []upload.ManagerOption
	opts = append(opts, upload.WithForce(flagGenerateForce))
	if tofuPath != "" {
		opts = append(opts, upload.WithTofuPath(tofuPath, initArgs))
	}

	mgr := upload.NewManager(cred, initArgs, opts...)
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

// lazyStateReader defers tofu binary discovery until ReadState is first called.
// This allows migrations that never read state to succeed without tofu installed.
type lazyStateReader struct {
	initArgs []string
	once     sync.Once
	reader   *state.TofuStateReader
	err      error
}

func newLazyStateReader(initArgs []string) *lazyStateReader {
	return &lazyStateReader{initArgs: initArgs}
}

func (l *lazyStateReader) ReadState(ctx context.Context, layerPath string) (*tfjson.State, error) {
	l.once.Do(func() {
		l.reader, l.err = state.NewTofuStateReaderFromPath(l.initArgs)
		if l.err != nil {
			l.err = fmt.Errorf("%w (required for state resolution; use --tofu-path to specify location)", l.err)
		}
	})
	if l.err != nil {
		return nil, l.err
	}
	return l.reader.ReadState(ctx, layerPath)
}

func (l *lazyStateReader) tofuPath() string {
	if l.reader != nil {
		return l.reader.TofuPath()
	}
	return ""
}

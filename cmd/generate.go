package cmd

import (
	"context"
	"fmt"
	"os"
	"sort"

	tfjson "github.com/hashicorp/terraform-json"
	"github.com/spf13/cobra"

	"github.com/redtenant/tfmigrate/pkg/auth"
	"github.com/redtenant/tfmigrate/pkg/engine"
	"github.com/redtenant/tfmigrate/pkg/state"
	"github.com/redtenant/tfmigrate/pkg/upload"
)

var (
	flagDryRun   bool
	flagTofuPath string
	flagUpload   bool
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
}

func runGenerate(cmd *cobra.Command, args []string) error {
	if flagUpload && flagDryRun {
		return fmt.Errorf("--upload and --dry-run cannot be used together")
	}

	// Resolve the tofu binary path
	var tofuPath string
	var stateReader state.StateReader

	if flagTofuPath != "" {
		tofuPath = flagTofuPath
		stateReader = state.NewTofuStateReaderWithPath(tofuPath)
	} else {
		reader, lookupErr := state.NewTofuStateReader()
		if lookupErr != nil {
			// tofu not found is acceptable in dry-run mode or when state
			// lookup is not needed; we'll fail later if state is actually needed
			fmt.Fprintf(os.Stderr, "Warning: %v\n", lookupErr)
			fmt.Fprintf(os.Stderr, "State auto-resolution will not be available. Use explicit import_id values.\n")
			stateReader = &noopStateReader{}
		} else {
			tofuPath = reader.TofuPath
			stateReader = reader
		}
	}

	// Wrap in a cache to avoid re-reading the same layer's state
	cachedReader := state.NewCachedStateReader(stateReader)

	cfg := engine.Config{
		StateReader: cachedReader,
		TofuPath:    tofuPath,
		DryRun:      flagDryRun,
	}

	eng := engine.New(cfg)
	ctx := context.Background()

	files, err := eng.ProcessFiles(ctx, args)
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
		for _, f := range files {
			fmt.Fprintf(os.Stdout, "Generated: %s\n", f)
		}
	}

	if flagUpload {
		if err := runUploadAfterGenerate(ctx, eng); err != nil {
			return fmt.Errorf("uploading migration files: %w", err)
		}
	}

	return nil
}

// runUploadAfterGenerate handles uploading generated files to Azure Blob Storage
// after the generate pipeline completes.
func runUploadAfterGenerate(ctx context.Context, eng *engine.Engine) error {
	credCfg, err := auth.NewCredentialConfiguration(
		auth.WithDefaultEnvironmentVariables(),
	)
	if err != nil {
		return fmt.Errorf("configuring Azure credentials: %w", err)
	}

	cred, err := credCfg.AzCore()
	if err != nil {
		return fmt.Errorf("creating Azure credential: %w", err)
	}

	// Collect init args from all processed migration files
	var initArgs []string
	for _, mf := range eng.MigrationFiles() {
		if mf.Init != nil {
			initArgs = append(initArgs, mf.Init.Args...)
		}
	}

	mgr := upload.NewManager(cred, initArgs)
	rendered := eng.Writer().RenderAll()

	return mgr.UploadRendered(ctx, rendered)
}

// noopStateReader is used as a fallback when the tofu binary is not found.
// It returns an error when state is actually requested, guiding the user
// to provide explicit import IDs.
type noopStateReader struct{}

func (n *noopStateReader) ReadState(_ context.Context, layerPath string) (*tfjson.State, error) {
	return nil, fmt.Errorf("tofu binary not available; cannot read state for layer %q. Provide explicit import_id values in your migration file", layerPath)
}

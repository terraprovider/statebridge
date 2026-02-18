package cmd

import (
	"context"
	"fmt"
	"os"

	tfjson "github.com/hashicorp/terraform-json"
	"github.com/spf13/cobra"

	"github.com/redtenant/tfmigrate/pkg/engine"
	"github.com/redtenant/tfmigrate/pkg/state"
)

var (
	flagDryRun         bool
	flagOutputFilename string
	flagTofuPath       string
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
  tfmigrate generate --dry-run migrations/`,
	Args: cobra.MinimumNArgs(1),
	RunE: runGenerate,
}

func init() {
	rootCmd.AddCommand(generateCmd)

	generateCmd.Flags().BoolVar(&flagDryRun, "dry-run", false,
		"Print generated HCL to stdout instead of writing files")
	generateCmd.Flags().StringVar(&flagOutputFilename, "output-filename", "",
		"Override output filename within each layer (default: migrations.tf)")
	generateCmd.Flags().StringVar(&flagTofuPath, "tofu-path", "",
		"Override path to the tofu binary (default: auto-detect from PATH)")
}

func runGenerate(cmd *cobra.Command, args []string) error {
	// Create state reader
	var stateReader state.StateReader
	var err error

	if flagTofuPath != "" {
		stateReader = state.NewTofuStateReaderWithPath(flagTofuPath)
	} else {
		stateReader, err = state.NewTofuStateReader()
		if err != nil {
			// tofu not found is acceptable in dry-run mode or when state
			// lookup is not needed; we'll fail later if state is actually needed
			fmt.Fprintf(os.Stderr, "Warning: %v\n", err)
			fmt.Fprintf(os.Stderr, "State auto-resolution will not be available. Use explicit import_id values.\n")
			stateReader = &noopStateReader{}
		}
	}

	// Wrap in a cache to avoid re-reading the same layer's state
	cachedReader := state.NewCachedStateReader(stateReader)

	cfg := engine.Config{
		StateReader:    cachedReader,
		DryRun:         flagDryRun,
		OutputFilename: flagOutputFilename,
	}

	eng := engine.New(cfg)
	ctx := context.Background()

	files, err := eng.ProcessFiles(ctx, args)
	if err != nil {
		return err
	}

	if flagDryRun {
		// In dry-run mode, print the rendered content for each layer
		w := eng.Writer()
		for layer, content := range w.RenderAll() {
			fmt.Fprintf(os.Stdout, "# Layer: %s\n", layer)
			fmt.Fprintln(os.Stdout, content)
		}
	} else {
		for _, f := range files {
			fmt.Fprintf(os.Stdout, "Generated: %s\n", f)
		}
	}

	return nil
}

// noopStateReader is used as a fallback when the tofu binary is not found.
// It returns an error when state is actually requested, guiding the user
// to provide explicit import IDs.
type noopStateReader struct{}

func (n *noopStateReader) ReadState(_ context.Context, layerPath string) (*tfjson.State, error) {
	return nil, fmt.Errorf("tofu binary not available; cannot read state for layer %q. Provide explicit import_id values in your migration file", layerPath)
}

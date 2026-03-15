package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/redtenant/tfmigrate/pkg/conditions"
	"github.com/redtenant/tfmigrate/pkg/generator"
	"github.com/redtenant/tfmigrate/pkg/state"
	"github.com/redtenant/tfmigrate/pkg/upload"
)

var (
	flagPruneDryRun        bool
	flagPruneBackendConfig []string
	flagPruneTofuPath      string
	flagPruneForce         bool
)

// pruneCmd represents the prune command.
var pruneCmd = &cobra.Command{
	Use:   "prune [layer-dirs...]",
	Short: "Remove completed migration blobs from blob storage",
	Long: `Prune migration blobs whose conditions indicate they are no longer active.

For each layer directory, discovers the blob storage backend, lists all
migration blobs, evaluates their embedded conditions against the current
state, and deletes blobs whose conditions fail (migration completed).

Blobs without embedded conditions are kept (cannot determine safety).

Examples:
  # Dry run: list what would be pruned
  tfmigrate prune --dry-run ./layers/compute ./layers/networking

  # Prune completed migrations
  tfmigrate prune ./layers/compute ./layers/networking

  # Force delete all migration blobs (no condition evaluation)
  tfmigrate prune --force ./layers/compute`,
	Args: cobra.MinimumNArgs(1),
	RunE: runPrune,
}

func init() {
	rootCmd.AddCommand(pruneCmd)

	pruneCmd.Flags().BoolVar(&flagPruneDryRun, "dry-run", false,
		"List migration blobs that would be pruned without deleting them")
	pruneCmd.Flags().StringSliceVar(&flagPruneBackendConfig, "backend-config", nil,
		"Backend configuration passed to tofu init, as key=value or path to a file (repeatable)")
	pruneCmd.Flags().StringVar(&flagPruneTofuPath, "tofu-path", "",
		"Path to the tofu binary for condition evaluation (default: auto-detect from PATH)")
	pruneCmd.Flags().BoolVar(&flagPruneForce, "force", false,
		"Delete all migration blobs without evaluating conditions")
}

func runPrune(cmd *cobra.Command, args []string) error {
	factory, err := createUploaderFactory()
	if err != nil {
		return err
	}

	// Build init args from --backend-config flags
	initArgs := buildInitArgs(flagPruneBackendConfig)

	// Resolve the tofu binary eagerly — required for condition evaluation.
	tofuPath, err := resolveTofuPath(flagPruneTofuPath)
	if err != nil {
		return err
	}
	stateReader := state.NewTofuStateReader(tofuPath, initArgs)

	ctx := context.Background()

	var totalPruned, totalKept int

	for _, layerDir := range args {
		pruned, kept, err := pruneLayer(ctx, layerDir, factory, stateReader, initArgs, flagPruneForce, flagPruneDryRun)
		if err != nil {
			return fmt.Errorf("pruning %q: %w", layerDir, err)
		}
		totalPruned += pruned
		totalKept += kept
	}

	fmt.Fprintf(os.Stderr, "\nPruned %d blob(s), kept %d\n", totalPruned, totalKept)
	return nil
}

func pruneLayer(
	ctx context.Context,
	layerDir string,
	factory upload.UploaderFactory,
	stateReader state.StateReader,
	initArgs []string,
	force bool,
	dryRun bool,
) (int, int, error) {
	config, err := upload.DiscoverBackendConfig(layerDir, initArgs)
	if err != nil {
		return 0, 0, fmt.Errorf("discovering backend config: %w", err)
	}

	uploader, err := factory(config)
	if err != nil {
		return 0, 0, fmt.Errorf("creating blob client: %w", err)
	}
	defer func() { _ = uploader.Close() }()

	migrationBlobs, err := listMigrationBlobs(ctx, uploader)
	if err != nil {
		return 0, 0, err
	}

	if len(migrationBlobs) == 0 {
		fmt.Fprintf(os.Stderr, "%s: no migration blobs found\n", layerDir)
		return 0, 0, nil
	}

	var pruned, kept int

	for _, blobName := range migrationBlobs {
		action, err := pruneBlob(ctx, uploader, blobName, layerDir, stateReader, force, dryRun)
		if err != nil {
			return pruned, kept, err
		}
		if action {
			pruned++
		} else {
			kept++
		}
	}

	return pruned, kept, nil
}

// listMigrationBlobs returns the filtered list of migration.*.tf blobs.
func listMigrationBlobs(ctx context.Context, uploader upload.BlobUploader) ([]string, error) {
	blobs, err := uploader.ListBlobs(ctx, "migrations/")
	if err != nil {
		return nil, fmt.Errorf("listing migration blobs: %w", err)
	}

	var result []string
	for _, b := range blobs {
		name := filepath.Base(b)
		if strings.HasPrefix(name, "migration.") && strings.HasSuffix(name, ".tf") {
			result = append(result, b)
		}
	}
	return result, nil
}

// pruneBlob evaluates a single blob and either prunes or keeps it.
// Returns true if the blob was pruned, false if kept.
func pruneBlob(
	ctx context.Context,
	uploader upload.BlobUploader,
	blobName string,
	layerDir string,
	stateReader state.StateReader,
	force bool,
	dryRun bool,
) (bool, error) {
	filename := filepath.Base(blobName)

	if force {
		return deleteOrDryRun(ctx, uploader, blobName, layerDir, filename, dryRun)
	}

	// Download and parse metadata for condition evaluation
	content, err := uploader.DownloadBlob(ctx, blobName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not download %q, keeping: %v\n", blobName, err)
		return false, nil
	}

	meta, err := generator.ParseMetadataComment(string(content))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not parse metadata in %q, keeping: %v\n", filename, err)
		return false, nil
	}

	if meta == nil || meta.Conditions == nil {
		fmt.Fprintf(os.Stderr, "%s: no conditions embedded, keeping\n", filename)
		return false, nil
	}

	// Evaluate conditions: if they FAIL, the migration is complete -> safe to prune.
	// If they PASS, the migration is still active -> keep.
	readState := conditions.NewStateReaderFunc(stateReader)
	active, err := conditions.EvaluateMetadataConditions(ctx, meta, readState, layerDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: condition evaluation failed for %q, keeping: %v\n", filename, err)
		return false, nil
	}

	if active {
		fmt.Fprintf(os.Stderr, "%s: conditions still met (active), keeping\n", filename)
		return false, nil
	}

	return deleteOrDryRun(ctx, uploader, blobName, layerDir, filename, dryRun)
}

// deleteOrDryRun deletes a blob or prints a dry-run message.
// Returns true (pruned) on success.
func deleteOrDryRun(
	ctx context.Context,
	uploader upload.BlobUploader,
	blobName string,
	layerDir string,
	filename string,
	dryRun bool,
) (bool, error) {
	if dryRun {
		fmt.Fprintf(os.Stdout, "Would prune: %s/%s\n", layerDir, filename)
		return true, nil
	}
	if err := uploader.DeleteBlob(ctx, blobName); err != nil {
		return false, fmt.Errorf("deleting %q: %w", blobName, err)
	}
	fmt.Fprintf(os.Stdout, "Pruned: %s/%s\n", layerDir, filename)
	return true, nil
}

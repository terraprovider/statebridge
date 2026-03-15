package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/spf13/cobra"

	"github.com/redtenant/tfmigrate/pkg/auth"
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
	Short: "Remove completed migration blobs from Azure Blob Storage",
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
	initArgs := buildInitArgs(flagPruneBackendConfig)

	// Resolve tofu path for condition evaluation (not needed with --force)
	var stateReader state.StateReader
	if !flagPruneForce {
		tofuPath := flagPruneTofuPath
		if tofuPath == "" {
			if resolved, err := exec.LookPath("tofu"); err == nil {
				tofuPath = resolved
			} else {
				return fmt.Errorf("tofu binary not found in PATH (required for condition evaluation; use --force to skip)")
			}
		}
		stateReader = state.NewTofuStateReader(tofuPath, initArgs)
	}

	ctx := context.Background()

	var totalPruned, totalKept int

	for _, layerDir := range args {
		pruned, kept, err := pruneLayer(ctx, layerDir, cred, stateReader)
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
	cred azcore.TokenCredential,
	stateReader state.StateReader,
) (int, int, error) {
	initArgs := buildInitArgs(flagPruneBackendConfig)

	config, err := upload.DiscoverBackendConfig(layerDir, initArgs)
	if err != nil {
		return 0, 0, fmt.Errorf("discovering backend config: %w", err)
	}

	uploader, err := upload.DefaultUploaderFactory(config.StorageAccountName, config.ContainerName, cred)
	if err != nil {
		return 0, 0, fmt.Errorf("creating blob client: %w", err)
	}

	blobs, err := uploader.ListBlobs(ctx, "migrations/")
	if err != nil {
		return 0, 0, fmt.Errorf("listing migration blobs: %w", err)
	}

	// Filter to migration.*.tf files
	var migrationBlobs []string
	for _, b := range blobs {
		name := filepath.Base(b)
		if strings.HasPrefix(name, "migration.") && strings.HasSuffix(name, ".tf") {
			migrationBlobs = append(migrationBlobs, b)
		}
	}

	if len(migrationBlobs) == 0 {
		fmt.Fprintf(os.Stderr, "%s: no migration blobs found\n", layerDir)
		return 0, 0, nil
	}

	var pruned, kept int

	for _, blobName := range migrationBlobs {
		filename := filepath.Base(blobName)

		if flagPruneForce {
			if flagPruneDryRun {
				fmt.Fprintf(os.Stdout, "Would prune: %s/%s\n", layerDir, filename)
			} else {
				if err := uploader.DeleteBlob(ctx, blobName); err != nil {
					return pruned, kept, fmt.Errorf("deleting %q: %w", blobName, err)
				}
				fmt.Fprintf(os.Stdout, "Pruned: %s/%s\n", layerDir, filename)
			}
			pruned++
			continue
		}

		// Download and parse metadata for condition evaluation
		content, err := uploader.DownloadBlob(ctx, blobName)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not download %q, keeping: %v\n", blobName, err)
			kept++
			continue
		}

		meta, err := generator.ParseMetadataComment(string(content))
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not parse metadata in %q, keeping: %v\n", filename, err)
			kept++
			continue
		}

		if meta == nil || meta.Conditions == nil {
			fmt.Fprintf(os.Stderr, "%s: no conditions embedded, keeping\n", filename)
			kept++
			continue
		}

		// Evaluate conditions: if they FAIL, the migration is complete -> safe to prune.
		// If they PASS, the migration is still active -> keep.
		readState := conditions.NewStateReaderFunc(stateReader)
		active, err := conditions.EvaluateMetadataConditions(ctx, meta, readState, layerDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: condition evaluation failed for %q, keeping: %v\n", filename, err)
			kept++
			continue
		}

		if active {
			fmt.Fprintf(os.Stderr, "%s: conditions still met (active), keeping\n", filename)
			kept++
			continue
		}

		// Conditions failed -> migration is complete -> prune
		if flagPruneDryRun {
			fmt.Fprintf(os.Stdout, "Would prune: %s/%s\n", layerDir, filename)
		} else {
			if err := uploader.DeleteBlob(ctx, blobName); err != nil {
				return pruned, kept, fmt.Errorf("deleting %q: %w", blobName, err)
			}
			fmt.Fprintf(os.Stdout, "Pruned: %s/%s\n", layerDir, filename)
		}
		pruned++
	}

	return pruned, kept, nil
}

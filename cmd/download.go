package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/redtenant/tfmigrate/pkg/download"
)

var (
	flagDownloadBackendConfig []string
	flagDownloadTofuPath      string
	flagDownloadDryRun        bool
)

// downloadCmd represents the download command.
var downloadCmd = &cobra.Command{
	Use:   "download",
	Short: "Download applicable migration files from blob storage",
	Long: `Download migration files from the blob storage container/bucket configured
in the current layer's backend. Each migration file's conditions are evaluated
against the layer's state, and only applicable migrations are written to the
current directory.

This command must be run from within a layer directory that contains Terraform
backend configuration. The backend type (azurerm, s3, gcs, local) is auto-detected.

The backend is automatically initialized to read state for condition evaluation.

Examples:
  # Download applicable migrations to the current layer directory
  cd layers/compute && tfmigrate download

  # Override backend config
  tfmigrate download --backend-config=storage_account_name=myacct

  # Preview what would be downloaded
  tfmigrate download --dry-run`,
	Args: cobra.NoArgs,
	RunE: runDownload,
}

func init() {
	rootCmd.AddCommand(downloadCmd)

	downloadCmd.Flags().StringSliceVar(&flagDownloadBackendConfig, "backend-config", nil,
		"Backend configuration passed to tofu init, as key=value or path to a file (repeatable)")
	downloadCmd.Flags().StringVar(&flagDownloadTofuPath, "tofu-path", "",
		"Override path to the tofu binary (default: auto-detect from PATH)")
	downloadCmd.Flags().BoolVar(&flagDownloadDryRun, "dry-run", false,
		"Print what would be downloaded without writing files")
}

func runDownload(cmd *cobra.Command, args []string) error {
	factory, err := createUploaderFactory()
	if err != nil {
		return err
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting current directory: %w", err)
	}

	// Build init args from --backend-config flags
	initArgs := buildInitArgs(flagDownloadBackendConfig)

	// Resolve the tofu binary eagerly — required for condition evaluation.
	tofuPath, err := resolveTofuPath(flagDownloadTofuPath)
	if err != nil {
		return err
	}

	var dlOpts []download.DownloaderOption
	dlOpts = append(dlOpts, download.WithTofuPath(tofuPath))
	dlOpts = append(dlOpts, download.WithDryRun(flagDownloadDryRun))

	dl := download.NewDownloader(factory, initArgs, dlOpts...)
	ctx := context.Background()

	files, err := dl.Download(ctx, cwd)
	if err != nil {
		return fmt.Errorf("downloading migrations: %w", err)
	}

	fmt.Fprintf(os.Stderr, "\n%d migration(s) downloaded\n", len(files))
	return nil
}

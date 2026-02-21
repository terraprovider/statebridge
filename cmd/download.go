package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/redtenant/tfmigrate/pkg/auth"
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
	Short: "Download applicable migration files from Azure Blob Storage",
	Long: `Download migration files from the Azure Blob Storage container configured
in the current layer's backend. Each migration file's conditions are evaluated
against the layer's state, and only applicable migrations are written to the
current directory.

This command must be run from within a layer directory that contains Terraform
backend configuration (backend "azurerm" block in .tf files).

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
		"Backend config overrides in key=value format (repeatable)")
	downloadCmd.Flags().StringVar(&flagDownloadTofuPath, "tofu-path", "",
		"Override path to the tofu binary (default: auto-detect from PATH)")
	downloadCmd.Flags().BoolVar(&flagDownloadDryRun, "dry-run", false,
		"Print what would be downloaded without writing files")
}

func runDownload(cmd *cobra.Command, args []string) error {
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

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting current directory: %w", err)
	}

	dl := download.NewDownloader(cred, flagDownloadBackendConfig, flagDownloadTofuPath, flagDownloadDryRun)
	ctx := context.Background()

	files, err := dl.Download(ctx, cwd)
	if err != nil {
		return fmt.Errorf("downloading migrations: %w", err)
	}

	fmt.Fprintf(os.Stderr, "\n%d migration(s) downloaded\n", len(files))
	return nil
}

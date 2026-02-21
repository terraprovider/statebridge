package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/redtenant/tfmigrate/pkg/auth"
	"github.com/redtenant/tfmigrate/pkg/upload"
)

var flagUploadBackendConfig []string

// uploadCmd represents the standalone upload command.
var uploadCmd = &cobra.Command{
	Use:   "upload [layer-dirs...]",
	Short: "Upload pre-generated migration files to Azure Blob Storage",
	Long: `Upload previously generated migration .tf files from layer directories
to Azure Blob Storage. Each layer directory is scanned for migration.*.tf files
and uploaded to the storage container configured in the layer's backend.

Backend configuration is discovered from the layer's .tf files (backend "azurerm"
block) and optionally supplemented with --backend-config flags.

Authentication uses Azure SDK credentials configured via environment variables
(ARM_CLIENT_ID, ARM_TENANT_ID, ARM_CLIENT_SECRET, ARM_USE_CLI, ARM_USE_MSI).

Examples:
  # Upload migration files from specific layer directories
  tfmigrate upload ./layers/compute ./layers/networking

  # Upload with additional backend config overrides
  tfmigrate upload --backend-config=storage_account_name=myacct ./layers/compute`,
	Args: cobra.MinimumNArgs(1),
	RunE: runUpload,
}

func init() {
	rootCmd.AddCommand(uploadCmd)

	uploadCmd.Flags().StringSliceVar(&flagUploadBackendConfig, "backend-config", nil,
		"Backend configuration passed to tofu init, as key=value or path to a file (repeatable)")
}

func runUpload(cmd *cobra.Command, args []string) error {
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
	var initArgs []string
	for _, bc := range flagUploadBackendConfig {
		initArgs = append(initArgs, "-backend-config="+bc)
	}

	mgr := upload.NewManager(cred, initArgs)
	ctx := context.Background()

	return mgr.UploadFromDisk(ctx, args)
}

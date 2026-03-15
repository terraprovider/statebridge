package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/redtenant/tfmigrate/pkg/auth"
	"github.com/redtenant/tfmigrate/pkg/upload"
)

var (
	flagUploadBackendConfig []string
	flagUploadForce         bool
	flagUploadTofuPath      string
)

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
	uploadCmd.Flags().BoolVar(&flagUploadForce, "force", false,
		"Force upload even if existing migrations are still active (overwrite protection bypass)")
	uploadCmd.Flags().StringVar(&flagUploadTofuPath, "tofu-path", "",
		"Path to the tofu binary for upload guard state evaluation (default: auto-detect from PATH)")
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
	initArgs := buildInitArgs(flagUploadBackendConfig)

	var opts []upload.ManagerOption
	opts = append(opts, upload.WithForce(flagUploadForce))

	// Resolve tofu path for the upload guard (not needed with --force)
	if !flagUploadForce {
		tofuPath, err := resolveTofuPath(flagUploadTofuPath)
		if err != nil {
			return fmt.Errorf("%w (required for upload guard; use --force to skip)", err)
		}
		opts = append(opts, upload.WithTofuPath(tofuPath, initArgs))
	}

	mgr := upload.NewManager(cred, initArgs, opts...)
	ctx := context.Background()

	return mgr.UploadFromDisk(ctx, args)
}

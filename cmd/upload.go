package cmd

import (
	"context"

	"github.com/spf13/cobra"

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
	Short: "Upload pre-generated migration files to blob storage",
	Long: `Upload previously generated migration .tf files from layer directories
to blob storage. Each layer directory is scanned for migration.*.tf files
and uploaded to the storage container/bucket configured in the layer's backend.

The backend type (azurerm, s3, gcs, local) is auto-detected from the layer's
.tf files (backend block) and optionally supplemented with --backend-config flags.

Authentication uses the native credential chain for each backend (Azure SDK
environment variables for azurerm, AWS SDK credential chain for s3, GCP
Application Default Credentials for gcs, no auth for local).

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
		"Path to the tofu binary (default: auto-detect from PATH)")
}

func runUpload(cmd *cobra.Command, args []string) error {
	factory, err := createUploaderFactory()
	if err != nil {
		return err
	}

	// Build init args from --backend-config flags
	initArgs := buildInitArgs(flagUploadBackendConfig)

	// Resolve the tofu binary eagerly — required for upload guard.
	tofuPath, err := resolveTofuPath(flagUploadTofuPath)
	if err != nil {
		return err
	}

	var opts []upload.ManagerOption
	opts = append(opts, upload.WithForce(flagUploadForce))
	opts = append(opts, upload.WithTofuPath(tofuPath, initArgs))

	mgr := upload.NewManager(factory, initArgs, opts...)
	ctx := context.Background()

	return mgr.UploadFromDisk(ctx, args)
}

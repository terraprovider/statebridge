//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/terraprovider/statebridge/pkg/download"
	"github.com/terraprovider/statebridge/pkg/upload"
)

// crossTenantBackendConfigPath returns the path to the checked-in backend
// config file describing a storage account that lives in a different Entra
// tenant/subscription than the one running the rest of this test suite.
func crossTenantBackendConfigPath() string {
	return filepath.Join(testdataDir(), "testdata", "cross_tenant_backend.hcl")
}

// TestE2E_CrossTenantUploadDownload proves that migration files can be
// uploaded to and downloaded from a storage account whose credentials
// (client_id, tenant_id, use_oidc) are supplied entirely via a
// --backend-config=<file> and differ from the default ARM_* environment
// credentials used elsewhere in this test run. This exercises the
// credential-merging feature end to end: environment variables populate the
// baseline credential, and the file's values take precedence, letting a
// single layer's state storage authenticate against a different tenant.
//
// The base credential passed to Manager/Downloader below is deliberately the
// *default* (primary-tenant) credential, not one built directly from the
// cross-tenant file. If the merge in upload.ResolveCredential did not
// correctly apply the file's client_id/tenant_id/use_oidc on top of it, the
// upload/download calls below would fail to authenticate against the
// cross-tenant storage account.
func TestE2E_CrossTenantUploadDownload(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	sharedDir := filepath.Join(rootDir, "shared")
	appDir := filepath.Join(rootDir, "app")
	for _, dir := range []string{sharedDir, appDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("creating %s: %v", dir, err)
		}
	}

	prefix := uniquePrefix(t)
	vars := map[string]string{"prefix": prefix}
	// The resource name embeds the unique prefix (not just its keepers) so
	// that any leftover blobs from a prior run in the shared cross-tenant
	// container can never be mistaken for applicable to this run: their
	// embedded conditions reference a different, prior-run-specific address.
	resourceName := "cross_tenant_" + prefix

	const variablesHCL = `
variable "prefix" {
  type = string
}
`

	updateTfFile(t, sharedDir, "main.tf", randomProviderHCL+variablesHCL+randomIDResource(resourceName))
	updateTfFile(t, appDir, "main.tf", randomProviderHCL+variablesHCL)

	tofuInit(t, sharedDir)
	tofuApply(t, sharedDir, vars)
	tofuInit(t, appDir)
	t.Cleanup(func() {
		tofuDestroy(t, appDir, vars)
		tofuDestroy(t, sharedDir, vars)
	})

	migDir := writeMigration(t, rootDir, "001_cross_tenant_"+prefix+".yaml", fmt.Sprintf(`
description: "Cross-tenant upload/download test"
operations:
  - type: move
    source_layer: "%s"
    destination_layer: "%s"
    resources:
      - from: "random_id.%s"
`, sharedDir, appDir, resourceName))

	updateTfFile(t, appDir, "main.tf", randomProviderHCL+variablesHCL+randomIDResource(resourceName))
	updateTfFile(t, sharedDir, "main.tf", randomProviderHCL+variablesHCL)

	// Re-initialize the app layer after updating main.tf to refresh the lock file.
	tofuInit(t, appDir)

	files := requireGenerate(t, migDir)

	// Everything needed to reach the cross-tenant storage account (storage
	// account, container, client_id, tenant_id, use_oidc) comes from the
	// checked-in backend config file — no separate env vars required.
	initArgs := []string{"-backend-config=" + crossTenantBackendConfigPath()}

	cred := getCredential(t)
	ctx := context.Background()

	mgr := upload.NewManager(cred, initArgs)
	if err := mgr.UploadFromDisk(ctx, []string{appDir}); err != nil {
		t.Fatalf("uploading app migration: %v", err)
	}
	if err := mgr.UploadFromDisk(ctx, []string{sharedDir}); err != nil {
		t.Fatalf("uploading shared migration: %v", err)
	}

	// The cross-tenant container is a fixed, persistent fixture shared
	// across test runs (not created/deleted per run), so clean up exactly
	// the blobs this run uploaded, identified by name, rather than touching
	// anything else in the container.
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		config, err := upload.DiscoverBackendConfig(appDir, initArgs)
		if err != nil {
			t.Logf("WARNING: cleanup: discovering backend config: %v", err)
			return
		}
		cleanupCred, err := upload.ResolveCredential(cred, config)
		if err != nil {
			t.Logf("WARNING: cleanup: resolving credential: %v", err)
			return
		}
		uploader, err := upload.DefaultUploaderFactory(config.StorageAccountName, config.ContainerName, cleanupCred)
		if err != nil {
			t.Logf("WARNING: cleanup: creating blob client: %v", err)
			return
		}
		for _, f := range files {
			blobName := "migrations/" + filepath.Base(f)
			if err := uploader.DeleteBlob(cleanupCtx, blobName); err != nil {
				t.Logf("WARNING: cleanup: deleting %s: %v", blobName, err)
			}
		}
	})

	cleanupMigrationFiles(t, appDir)
	cleanupMigrationFiles(t, sharedDir)

	dl := download.NewDownloader(cred, initArgs, download.WithTofuPath(tofuExecPath(t)))
	downloaded, err := dl.Download(ctx, appDir)
	if err != nil {
		t.Fatalf("downloading migration: %v", err)
	}
	if len(downloaded) == 0 {
		t.Fatal("expected downloaded migration files, got none")
	}
	for _, f := range downloaded {
		assertFileContains(t, f, "import")
	}

	tofuInit(t, appDir)
	tofuApply(t, appDir, vars)
	tofuApply(t, sharedDir, vars)

	assertResourceInState(t, appDir, "random_id."+resourceName)
	assertResourceNotInState(t, sharedDir, "random_id."+resourceName)

	cleanupAndAssertClean(t, vars, sharedDir, appDir)
}

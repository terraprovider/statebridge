//go:build e2e_fast

package e2e

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/terraprovider/statebridge/pkg/conditions"
	"github.com/terraprovider/statebridge/pkg/download"
	"github.com/terraprovider/statebridge/pkg/generator"
	"github.com/terraprovider/statebridge/pkg/state"
	"github.com/terraprovider/statebridge/pkg/upload"
)

func TestE2EFast_UploadDownload(t *testing.T) {
	t.Parallel()
	env := setupStorageTest(t)

	// Create resource in shared layer
	updateTfFile(t, env.sharedDir, "main.tf", randomProviderHCL+randomIDResource("upload_test"))
	tofuInit(t, env.sharedDir)
	tofuApply(t, env.sharedDir, env.vars)
	tofuInit(t, env.appDir)
	t.Cleanup(func() {
		tofuDestroy(t, env.appDir, env.vars)
		tofuDestroy(t, env.sharedDir, env.vars)
	})

	// Write move migration and generate
	migDir := writeMigration(t, env.rootDir, "001_upload.yaml", fmt.Sprintf(`
description: "Move resource for upload/download test"
operations:
  - type: move
    source_layer: "%s"
    destination_layer: "%s"
    resources:
      - from: "random_id.upload_test"
`, env.sharedDir, env.appDir))

	updateTfFile(t, env.appDir, "main.tf", randomProviderHCL+randomIDResource("upload_test"))
	updateTfFile(t, env.sharedDir, "main.tf", randomProviderHCL)

	// Re-initialize app layer after updating main.tf to update lock file
	tofuInit(t, env.appDir)

	requireGenerate(t, migDir)

	// Upload generated files from both layers
	mgr := upload.NewManager(env.cred, env.initArgs)
	if err := mgr.UploadFromDisk(env.ctx, []string{env.appDir}); err != nil {
		t.Fatalf("upload: %v", err)
	}
	if err := mgr.UploadFromDisk(env.ctx, []string{env.sharedDir}); err != nil {
		t.Fatalf("upload shared: %v", err)
	}

	// Clean local migration files to simulate fresh download
	cleanupMigrationFiles(t, env.appDir)
	cleanupMigrationFiles(t, env.sharedDir)

	// Download into app layer
	dl := download.NewDownloader(env.cred, env.initArgs, download.WithTofuPath(tofuExecPath(t)))
	downloaded, err := dl.Download(env.ctx, env.appDir)
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	if len(downloaded) == 0 {
		t.Fatal("expected downloaded migration files, got none")
	}
	for _, f := range downloaded {
		assertFileContains(t, f, "import")
	}

	// Apply the migration
	tofuInit(t, env.appDir)
	tofuApply(t, env.appDir, env.vars)
	tofuApply(t, env.sharedDir, env.vars)

	assertResourceInState(t, env.appDir, "random_id.upload_test")
	assertResourceNotInState(t, env.sharedDir, "random_id.upload_test")

	cleanupAndAssertClean(t, env.vars, env.sharedDir, env.appDir)
}

// TestE2EFast_UploadGuard tests that the upload guard prevents overwriting
// active migrations and that --force bypasses the guard.
func TestE2EFast_UploadGuard(t *testing.T) {
	t.Parallel()
	env := setupStorageTest(t)

	updateTfFile(t, env.sharedDir, "main.tf", randomProviderHCL+randomIDResource("guard_test"))
	tofuInit(t, env.sharedDir)
	tofuApply(t, env.sharedDir, env.vars)
	tofuInit(t, env.appDir)
	t.Cleanup(func() {
		tofuDestroy(t, env.appDir, env.vars)
		tofuDestroy(t, env.sharedDir, env.vars)
	})

	// Generate first migration
	migDir := writeMigration(t, env.rootDir, "001_guard.yaml", fmt.Sprintf(`
description: "Move resource for guard test"
operations:
  - type: move
    source_layer: "%s"
    destination_layer: "%s"
    resources:
      - from: "random_id.guard_test"
`, env.sharedDir, env.appDir))

	updateTfFile(t, env.appDir, "main.tf", randomProviderHCL+randomIDResource("guard_test"))
	updateTfFile(t, env.sharedDir, "main.tf", randomProviderHCL)

	requireGenerate(t, migDir)

	// First upload: should succeed
	mgr := upload.NewManager(env.cred, env.initArgs,
		upload.WithTofuPath(tofuExecPath(t), env.initArgs),
	)
	if err := mgr.UploadFromDisk(env.ctx, []string{env.appDir}); err != nil {
		t.Fatalf("first upload: %v", err)
	}

	// Modify migration to produce different content hash
	cleanupMigrationFiles(t, env.appDir)
	cleanupMigrationFiles(t, env.sharedDir)
	writeMigration(t, env.rootDir, "001_guard.yaml", fmt.Sprintf(`
description: "Move resource for guard test (modified)"
condition:
  layer_exists:
    - "%s"
operations:
  - type: move
    source_layer: "%s"
    destination_layer: "%s"
    resources:
      - from: "random_id.guard_test"
`, env.sharedDir, env.sharedDir, env.appDir))
	runGenerate(t, []string{migDir})

	// Second upload with guard: should be refused (migration still active)
	mgr2 := upload.NewManager(env.cred, env.initArgs,
		upload.WithTofuPath(tofuExecPath(t), env.initArgs),
	)
	err := mgr2.UploadFromDisk(env.ctx, []string{env.appDir})
	if err == nil {
		t.Fatal("expected upload guard to refuse overwrite, but upload succeeded")
	}
	if !strings.Contains(err.Error(), "refusing to overwrite") {
		t.Fatalf("expected 'refusing to overwrite' error, got: %v", err)
	}

	// Upload with --force: should succeed
	mgr3 := upload.NewManager(env.cred, env.initArgs,
		upload.WithTofuPath(tofuExecPath(t), env.initArgs),
		upload.WithForce(true),
	)
	if err := mgr3.UploadFromDisk(env.ctx, []string{env.appDir}); err != nil {
		t.Fatalf("force upload: %v", err)
	}
}

// TestE2EFast_DownloadConditionSkip tests that download skips migration files
// whose auto-inferred conditions are no longer met (migration already applied).
func TestE2EFast_DownloadConditionSkip(t *testing.T) {
	t.Parallel()
	env := setupStorageTest(t)

	updateTfFile(t, env.sharedDir, "main.tf", randomProviderHCL+randomIDResource("condskip"))
	tofuInit(t, env.sharedDir)
	tofuApply(t, env.sharedDir, env.vars)
	tofuInit(t, env.appDir)
	t.Cleanup(func() {
		tofuDestroy(t, env.appDir, env.vars)
		tofuDestroy(t, env.sharedDir, env.vars)
	})

	// Generate and upload the migration
	migDir := writeMigration(t, env.rootDir, "001_condskip.yaml", fmt.Sprintf(`
description: "Move for condition skip test"
operations:
  - type: move
    source_layer: "%s"
    destination_layer: "%s"
    resources:
      - from: "random_id.condskip"
`, env.sharedDir, env.appDir))

	updateTfFile(t, env.appDir, "main.tf", randomProviderHCL+randomIDResource("condskip"))
	updateTfFile(t, env.sharedDir, "main.tf", randomProviderHCL)

	requireGenerate(t, migDir)

	// Upload migration files from app layer (import blocks)
	mgr := upload.NewManager(env.cred, env.initArgs)
	if err := mgr.UploadFromDisk(env.ctx, []string{env.appDir}); err != nil {
		t.Fatalf("upload: %v", err)
	}

	// Apply the migration so the resource is now in appDir
	tofuInit(t, env.appDir)
	tofuApply(t, env.appDir, env.vars)
	tofuApply(t, env.sharedDir, env.vars)

	assertResourceInState(t, env.appDir, "random_id.condskip")
	assertResourceNotInState(t, env.sharedDir, "random_id.condskip")

	cleanupMigrationFiles(t, env.appDir)

	// Download: auto-inferred conditions should detect migration is done
	dl := download.NewDownloader(env.cred, env.initArgs, download.WithTofuPath(tofuExecPath(t)))
	downloaded, err := dl.Download(env.ctx, env.appDir)
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	if len(downloaded) != 0 {
		t.Errorf("expected no files downloaded (migration already applied), got %d: %v", len(downloaded), downloaded)
	}

	matches, _ := filepath.Glob(filepath.Join(env.appDir, "migration.*.tf"))
	if len(matches) != 0 {
		t.Errorf("expected no migration files on disk, got: %v", matches)
	}
}

// TestE2EFast_Prune tests that completed migrations are pruned from blob storage
// after their conditions no longer hold.
func TestE2EFast_Prune(t *testing.T) {
	t.Parallel()
	env := setupStorageTest(t)

	updateTfFile(t, env.sharedDir, "main.tf", randomProviderHCL+randomIDResource("prune_test"))
	tofuInit(t, env.sharedDir)
	tofuApply(t, env.sharedDir, env.vars)
	tofuInit(t, env.appDir)
	t.Cleanup(func() {
		tofuDestroy(t, env.appDir, env.vars)
		tofuDestroy(t, env.sharedDir, env.vars)
	})

	// Generate and upload
	migDir := writeMigration(t, env.rootDir, "001_prune.yaml", fmt.Sprintf(`
description: "Move for prune test"
operations:
  - type: move
    source_layer: "%s"
    destination_layer: "%s"
    resources:
      - from: "random_id.prune_test"
`, env.sharedDir, env.appDir))

	updateTfFile(t, env.appDir, "main.tf", randomProviderHCL+randomIDResource("prune_test"))
	updateTfFile(t, env.sharedDir, "main.tf", randomProviderHCL)

	requireGenerate(t, migDir)

	// Upload from app layer
	mgr := upload.NewManager(env.cred, env.initArgs)
	if err := mgr.UploadFromDisk(env.ctx, []string{env.appDir}); err != nil {
		t.Fatalf("upload: %v", err)
	}

	// Verify blobs exist before prune
	uploader, err := upload.DefaultUploaderFactory(env.storageAccount, env.containerName, env.cred)
	if err != nil {
		t.Fatalf("creating uploader: %v", err)
	}
	blobsBefore, err := uploader.ListBlobs(env.ctx, "migrations/")
	if err != nil {
		t.Fatalf("listing blobs: %v", err)
	}
	if len(blobsBefore) == 0 {
		t.Fatal("expected blobs in storage before prune")
	}

	// Apply the migration
	tofuInit(t, env.appDir)
	tofuApply(t, env.appDir, env.vars)
	tofuApply(t, env.sharedDir, env.vars)

	assertResourceInState(t, env.appDir, "random_id.prune_test")
	assertResourceNotInState(t, env.sharedDir, "random_id.prune_test")

	// Prune: replicate the prune logic
	stateReader, err := state.NewTofuStateReaderFromPath(nil)
	if err != nil {
		t.Fatalf("creating state reader: %v", err)
	}
	readState := conditions.NewStateReaderFunc(stateReader)

	var pruned int
	for _, blobName := range blobsBefore {
		if !strings.HasSuffix(blobName, ".tf") {
			continue
		}
		content, err := uploader.DownloadBlob(env.ctx, blobName)
		if err != nil {
			t.Fatalf("downloading blob %s: %v", blobName, err)
		}
		meta, err := generator.ParseMetadataComment(string(content))
		if err != nil {
			t.Fatalf("parsing metadata for %s: %v", blobName, err)
		}
		if meta == nil || meta.Conditions == nil {
			continue
		}
		active, err := conditions.EvaluateMetadataConditions(env.ctx, meta, readState, env.appDir)
		if err != nil {
			t.Fatalf("evaluating conditions for %s: %v", blobName, err)
		}
		if !active {
			if err := uploader.DeleteBlob(env.ctx, blobName); err != nil {
				t.Fatalf("deleting blob %s: %v", blobName, err)
			}
			pruned++
		}
	}

	if pruned == 0 {
		t.Error("expected at least one blob to be pruned after migration was applied")
	}

	blobsAfter, err := uploader.ListBlobs(env.ctx, "migrations/")
	if err != nil {
		t.Fatalf("listing blobs after prune: %v", err)
	}
	if len(blobsAfter) != 0 {
		t.Errorf("expected no blobs after prune, got %d: %v", len(blobsAfter), blobsAfter)
	}

	cleanupMigrationFiles(t, env.appDir)
	cleanupMigrationFiles(t, env.sharedDir)
}

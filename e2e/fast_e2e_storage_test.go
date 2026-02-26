//go:build e2e_fast

package e2e

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/redtenant/tfmigrate/pkg/conditions"
	"github.com/redtenant/tfmigrate/pkg/download"
	"github.com/redtenant/tfmigrate/pkg/generator"
	"github.com/redtenant/tfmigrate/pkg/state"
	"github.com/redtenant/tfmigrate/pkg/upload"
)

func TestE2EFast_UploadDownload(t *testing.T) {
  t.Parallel()
  storageAccount := requireEnv(t, "E2E_STORAGE_ACCOUNT_NAME")
  ctx := context.Background()
  cred := getCredential(t)

  rootDir, prefix, vars := setupFastProject(t)
  containerName := prefix
  createContainer(t, ctx, cred, storageAccount, containerName)
  t.Cleanup(func() { deleteContainer(t, ctx, cred, storageAccount, containerName) })

  sharedDir := filepath.Join(rootDir, "layers", "shared")
  appDir := filepath.Join(rootDir, "layers", "app")
  initArgs := storageInitArgs(storageAccount, containerName)

  // Create resource in shared layer
  updateTfFile(t, sharedDir, "main.tf", `
terraform {
  required_providers {
    random = {
      source = "hashicorp/random"
    }
  }
}

provider "random" {}

resource "random_id" "upload_test" {
  byte_length = 4
  keepers = {
    prefix = var.prefix
    name   = "upload_test"
  }
}
`)

  tofuInit(t, sharedDir)
  tofuApply(t, sharedDir, vars)
  tofuInit(t, appDir)
  t.Cleanup(func() {
    tofuDestroy(t, appDir, vars)
    tofuDestroy(t, sharedDir, vars)
  })

  // Write move migration and generate
  migDir := writeMigration(t, rootDir, "001_upload.yaml", fmt.Sprintf(`
description: "Move resource for upload/download test"
operations:
  - type: move
    source_layer: "%s"
    destination_layer: "%s"
    resources:
      - from: "random_id.upload_test"
`, sharedDir, appDir))

  updateTfFile(t, appDir, "main.tf", `
terraform {
  required_providers {
    random = {
      source = "hashicorp/random"
    }
  }
}

provider "random" {}

resource "random_id" "upload_test" {
  byte_length = 4
  keepers = {
    prefix = var.prefix
    name   = "upload_test"
  }
}
`)

  updateTfFile(t, sharedDir, "main.tf", `
terraform {
  required_providers {
    random = {
      source = "hashicorp/random"
    }
  }
}

provider "random" {}
`)

  // Re-initialize app layer after updating main.tf to update lock file
  tofuInit(t, appDir)

  files := runGenerate(t, []string{migDir})
  if len(files) == 0 {
    t.Fatal("expected generated migration files")
  }

  // Upload generated files from the app layer
  mgr := upload.NewManager(cred, initArgs)
  if err := mgr.UploadFromDisk(ctx, []string{appDir}); err != nil {
    t.Fatalf("upload: %v", err)
  }

  // Also upload from shared layer (removed blocks)
  if err := mgr.UploadFromDisk(ctx, []string{sharedDir}); err != nil {
    t.Fatalf("upload shared: %v", err)
  }

  // Clean local migration files to simulate fresh download
  cleanupMigrationFiles(t, appDir)
  cleanupMigrationFiles(t, sharedDir)

  // Download into app layer
  dl := download.NewDownloader(cred, initArgs, tofuExecPath(t), false)
  downloaded, err := dl.Download(ctx, appDir)
  if err != nil {
    t.Fatalf("download: %v", err)
  }
  if len(downloaded) == 0 {
    t.Fatal("expected downloaded migration files, got none")
  }

  // Verify downloaded file contains import block
  for _, f := range downloaded {
    assertFileContains(t, f, "import")
  }

  // Apply the migration
  tofuInit(t, appDir)
  tofuApply(t, appDir, vars)
  tofuApply(t, sharedDir, vars)

  assertResourceInState(t, appDir, "random_id.upload_test")
  assertResourceNotInState(t, sharedDir, "random_id.upload_test")

  cleanupMigrationFiles(t, appDir)
  cleanupMigrationFiles(t, sharedDir)
  assertCleanPlan(t, sharedDir, vars)
  assertCleanPlan(t, appDir, vars)
}

// TestE2EFast_UploadGuard tests that the upload guard prevents overwriting
// active migrations and that --force bypasses the guard.

func TestE2EFast_UploadGuard(t *testing.T) {
  t.Parallel()
  storageAccount := requireEnv(t, "E2E_STORAGE_ACCOUNT_NAME")
  ctx := context.Background()
  cred := getCredential(t)

  rootDir, prefix, vars := setupFastProject(t)
  containerName := prefix
  createContainer(t, ctx, cred, storageAccount, containerName)
  t.Cleanup(func() { deleteContainer(t, ctx, cred, storageAccount, containerName) })

  sharedDir := filepath.Join(rootDir, "layers", "shared")
  appDir := filepath.Join(rootDir, "layers", "app")
  initArgs := storageInitArgs(storageAccount, containerName)

  // Create resource
  updateTfFile(t, sharedDir, "main.tf", `
terraform {
  required_providers {
    random = {
      source = "hashicorp/random"
    }
  }
}

provider "random" {}

resource "random_id" "guard_test" {
  byte_length = 4
  keepers = {
    prefix = var.prefix
    name   = "guard_test"
  }
}
`)

  tofuInit(t, sharedDir)
  tofuApply(t, sharedDir, vars)
  tofuInit(t, appDir)
  t.Cleanup(func() {
    tofuDestroy(t, appDir, vars)
    tofuDestroy(t, sharedDir, vars)
  })

  // Generate first migration
  migDir := writeMigration(t, rootDir, "001_guard.yaml", fmt.Sprintf(`
description: "Move resource for guard test"
operations:
  - type: move
    source_layer: "%s"
    destination_layer: "%s"
    resources:
      - from: "random_id.guard_test"
`, sharedDir, appDir))

  updateTfFile(t, appDir, "main.tf", `
terraform {
  required_providers {
    random = {
      source = "hashicorp/random"
    }
  }
}

provider "random" {}

resource "random_id" "guard_test" {
  byte_length = 4
  keepers = {
    prefix = var.prefix
    name   = "guard_test"
  }
}
`)

  updateTfFile(t, sharedDir, "main.tf", `
terraform {
  required_providers {
    random = {
      source = "hashicorp/random"
    }
  }
}

provider "random" {}
`)

  files := runGenerate(t, []string{migDir})
  if len(files) == 0 {
    t.Fatal("expected generated migration files")
  }

  // First upload: should succeed
  mgr := upload.NewManager(cred, initArgs,
    upload.WithTofuPath(tofuExecPath(t), initArgs),
  )
  if err := mgr.UploadFromDisk(ctx, []string{appDir}); err != nil {
    t.Fatalf("first upload: %v", err)
  }

  // Modify migration to produce different content hash
  // (add an explicit condition that changes the metadata)
  cleanupMigrationFiles(t, appDir)
  cleanupMigrationFiles(t, sharedDir)
  writeMigration(t, rootDir, "001_guard.yaml", fmt.Sprintf(`
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
`, sharedDir, sharedDir, appDir))
  runGenerate(t, []string{migDir})

  // Second upload with guard: should be refused (migration still active)
  mgr2 := upload.NewManager(cred, initArgs,
    upload.WithTofuPath(tofuExecPath(t), initArgs),
  )
  err := mgr2.UploadFromDisk(ctx, []string{appDir})
  if err == nil {
    t.Fatal("expected upload guard to refuse overwrite, but upload succeeded")
  }
  if !strings.Contains(err.Error(), "refusing to overwrite") {
    t.Fatalf("expected 'refusing to overwrite' error, got: %v", err)
  }

  // Upload with --force: should succeed
  mgr3 := upload.NewManager(cred, initArgs,
    upload.WithTofuPath(tofuExecPath(t), initArgs),
    upload.WithForce(true),
  )
  if err := mgr3.UploadFromDisk(ctx, []string{appDir}); err != nil {
    t.Fatalf("force upload: %v", err)
  }
}

// TestE2EFast_DownloadConditionSkip tests that download skips migration files
// whose auto-inferred conditions are no longer met (migration already applied).

func TestE2EFast_DownloadConditionSkip(t *testing.T) {
  t.Parallel()
  storageAccount := requireEnv(t, "E2E_STORAGE_ACCOUNT_NAME")
  ctx := context.Background()
  cred := getCredential(t)

  rootDir, prefix, vars := setupFastProject(t)
  containerName := prefix
  createContainer(t, ctx, cred, storageAccount, containerName)
  t.Cleanup(func() { deleteContainer(t, ctx, cred, storageAccount, containerName) })

  sharedDir := filepath.Join(rootDir, "layers", "shared")
  appDir := filepath.Join(rootDir, "layers", "app")
  initArgs := storageInitArgs(storageAccount, containerName)

  // Create resource
  updateTfFile(t, sharedDir, "main.tf", `
terraform {
  required_providers {
    random = {
      source = "hashicorp/random"
    }
  }
}

provider "random" {}

resource "random_id" "condskip" {
  byte_length = 4
  keepers = {
    prefix = var.prefix
    name   = "condskip"
  }
}
`)

  tofuInit(t, sharedDir)
  tofuApply(t, sharedDir, vars)
  tofuInit(t, appDir)
  t.Cleanup(func() {
    tofuDestroy(t, appDir, vars)
    tofuDestroy(t, sharedDir, vars)
  })

  // Generate and upload the migration
  migDir := writeMigration(t, rootDir, "001_condskip.yaml", fmt.Sprintf(`
description: "Move for condition skip test"
operations:
  - type: move
    source_layer: "%s"
    destination_layer: "%s"
    resources:
      - from: "random_id.condskip"
`, sharedDir, appDir))

  updateTfFile(t, appDir, "main.tf", `
terraform {
  required_providers {
    random = {
      source = "hashicorp/random"
    }
  }
}

provider "random" {}

resource "random_id" "condskip" {
  byte_length = 4
  keepers = {
    prefix = var.prefix
    name   = "condskip"
  }
}
`)

  updateTfFile(t, sharedDir, "main.tf", `
terraform {
  required_providers {
    random = {
      source = "hashicorp/random"
    }
  }
}

provider "random" {}
`)

  files := runGenerate(t, []string{migDir})
  if len(files) == 0 {
    t.Fatal("expected generated migration files")
  }

  // Upload migration files from app layer (import blocks)
  mgr := upload.NewManager(cred, initArgs)
  if err := mgr.UploadFromDisk(ctx, []string{appDir}); err != nil {
    t.Fatalf("upload: %v", err)
  }

  // Apply the migration so the resource is now in appDir
  tofuInit(t, appDir)
  tofuApply(t, appDir, vars)
  tofuApply(t, sharedDir, vars)

  assertResourceInState(t, appDir, "random_id.condskip")
  assertResourceNotInState(t, sharedDir, "random_id.condskip")

  // Clean local migration files
  cleanupMigrationFiles(t, appDir)

  // Download: auto-inferred conditions should detect migration is done (import
  // block has resources_not_exist for "random_id.condskip", but it IS in state)
  dl := download.NewDownloader(cred, initArgs, tofuExecPath(t), false)
  downloaded, err := dl.Download(ctx, appDir)
  if err != nil {
    t.Fatalf("download: %v", err)
  }

  // The migration file should be skipped because the import condition (resources_not_exist)
  // fails — the resource already exists in the app layer's state
  if len(downloaded) != 0 {
    t.Errorf("expected no files downloaded (migration already applied), got %d: %v", len(downloaded), downloaded)
  }

  // Verify no migration files on disk
  matches, _ := filepath.Glob(filepath.Join(appDir, "migration.*.tf"))
  if len(matches) != 0 {
    t.Errorf("expected no migration files on disk, got: %v", matches)
  }
}

// TestE2EFast_Prune tests that completed migrations are pruned from blob storage
// after their conditions no longer hold.

func TestE2EFast_Prune(t *testing.T) {
  t.Parallel()
  storageAccount := requireEnv(t, "E2E_STORAGE_ACCOUNT_NAME")
  ctx := context.Background()
  cred := getCredential(t)

  rootDir, prefix, vars := setupFastProject(t)
  containerName := prefix
  createContainer(t, ctx, cred, storageAccount, containerName)
  t.Cleanup(func() { deleteContainer(t, ctx, cred, storageAccount, containerName) })

  sharedDir := filepath.Join(rootDir, "layers", "shared")
  appDir := filepath.Join(rootDir, "layers", "app")
  initArgs := storageInitArgs(storageAccount, containerName)

  // Create resource
  updateTfFile(t, sharedDir, "main.tf", `
terraform {
  required_providers {
    random = {
      source = "hashicorp/random"
    }
  }
}

provider "random" {}

resource "random_id" "prune_test" {
  byte_length = 4
  keepers = {
    prefix = var.prefix
    name   = "prune_test"
  }
}
`)

  tofuInit(t, sharedDir)
  tofuApply(t, sharedDir, vars)
  tofuInit(t, appDir)
  t.Cleanup(func() {
    tofuDestroy(t, appDir, vars)
    tofuDestroy(t, sharedDir, vars)
  })

  // Generate and upload
  migDir := writeMigration(t, rootDir, "001_prune.yaml", fmt.Sprintf(`
description: "Move for prune test"
operations:
  - type: move
    source_layer: "%s"
    destination_layer: "%s"
    resources:
      - from: "random_id.prune_test"
`, sharedDir, appDir))

  updateTfFile(t, appDir, "main.tf", `
terraform {
  required_providers {
    random = {
      source = "hashicorp/random"
    }
  }
}

provider "random" {}

resource "random_id" "prune_test" {
  byte_length = 4
  keepers = {
    prefix = var.prefix
    name   = "prune_test"
  }
}
`)

  updateTfFile(t, sharedDir, "main.tf", `
terraform {
  required_providers {
    random = {
      source = "hashicorp/random"
    }
  }
}

provider "random" {}
`)

  files := runGenerate(t, []string{migDir})
  if len(files) == 0 {
    t.Fatal("expected generated migration files")
  }

  // Upload from app layer
  mgr := upload.NewManager(cred, initArgs)
  if err := mgr.UploadFromDisk(ctx, []string{appDir}); err != nil {
    t.Fatalf("upload: %v", err)
  }

  // Verify blobs exist before prune
  uploader, err := upload.DefaultUploaderFactory(storageAccount, containerName, cred)
  if err != nil {
    t.Fatalf("creating uploader: %v", err)
  }
  blobsBefore, err := uploader.ListBlobs(ctx, "migrations/")
  if err != nil {
    t.Fatalf("listing blobs: %v", err)
  }
  if len(blobsBefore) == 0 {
    t.Fatal("expected blobs in storage before prune")
  }

  // Apply the migration
  tofuInit(t, appDir)
  tofuApply(t, appDir, vars)
  tofuApply(t, sharedDir, vars)

  assertResourceInState(t, appDir, "random_id.prune_test")
  assertResourceNotInState(t, sharedDir, "random_id.prune_test")

  // Prune: replicate the prune logic since cmd.pruneLayer is unexported.
  // For each blob, download → parse metadata → evaluate conditions → prune if done.
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
    content, err := uploader.DownloadBlob(ctx, blobName)
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
    active, err := conditions.EvaluateMetadataConditions(ctx, meta, readState, appDir)
    if err != nil {
      t.Fatalf("evaluating conditions for %s: %v", blobName, err)
    }
    if !active {
      // Conditions failed = migration complete = safe to prune
      if err := uploader.DeleteBlob(ctx, blobName); err != nil {
        t.Fatalf("deleting blob %s: %v", blobName, err)
      }
      pruned++
    }
  }

  if pruned == 0 {
    t.Error("expected at least one blob to be pruned after migration was applied")
  }

  // Verify blobs are gone
  blobsAfter, err := uploader.ListBlobs(ctx, "migrations/")
  if err != nil {
    t.Fatalf("listing blobs after prune: %v", err)
  }
  if len(blobsAfter) != 0 {
    t.Errorf("expected no blobs after prune, got %d: %v", len(blobsAfter), blobsAfter)
  }

  cleanupMigrationFiles(t, appDir)
  cleanupMigrationFiles(t, sharedDir)
}

package upload

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
)

func TestManagerPruneStems(t *testing.T) {
	ctx := context.Background()

	t.Run("prunes matching blobs", func(t *testing.T) {
		mock := newMockUploader()
		mock.setBlobs("migrations/migration.001_move.", []string{
			"migrations/migration.001_move.a1b2c3d4.tf",
		})
		mock.setBlobs("migrations/migration.002_rename.", []string{
			"migrations/migration.002_rename.e5f6a7b8.tf",
		})

		dir := t.TempDir()
		layerPath := filepath.Join(dir, "layer")
		if err := os.MkdirAll(layerPath, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(layerPath, "backend.tf"), []byte(`
terraform {
  backend "azurerm" {
    storage_account_name = "pruneacct"
    container_name       = "prunecontainer"
  }
}
`), 0o644); err != nil {
			t.Fatal(err)
		}

		mgr := NewManager(nil, nil)
		mgr.WithUploaderFactory(func(sa, cn string, _ azcore.TokenCredential) (BlobUploader, error) {
			return mock, nil
		})

		pruned, err := mgr.PruneStems(ctx, []string{"001_move", "002_rename"}, []string{layerPath})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if pruned != 2 {
			t.Errorf("expected 2 pruned, got %d", pruned)
		}
		if len(mock.deleted) != 2 {
			t.Fatalf("expected 2 deletions, got %d: %v", len(mock.deleted), mock.deleted)
		}
	})

	t.Run("skips non-tf blobs", func(t *testing.T) {
		mock := newMockUploader()
		mock.setBlobs("migrations/migration.001_move.", []string{
			"migrations/migration.001_move.a1b2c3d4.tf",
			"migrations/migration.001_move.metadata.json",
		})

		dir := t.TempDir()
		layerPath := filepath.Join(dir, "layer")
		if err := os.MkdirAll(layerPath, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(layerPath, "backend.tf"), []byte(`
terraform {
  backend "azurerm" {
    storage_account_name = "acct"
    container_name       = "container"
  }
}
`), 0o644); err != nil {
			t.Fatal(err)
		}

		mgr := NewManager(nil, nil)
		mgr.WithUploaderFactory(func(sa, cn string, _ azcore.TokenCredential) (BlobUploader, error) {
			return mock, nil
		})

		pruned, err := mgr.PruneStems(ctx, []string{"001_move"}, []string{layerPath})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if pruned != 1 {
			t.Errorf("expected 1 pruned (only .tf), got %d", pruned)
		}
	})

	t.Run("no matching blobs is no-op", func(t *testing.T) {
		mock := newMockUploader()

		dir := t.TempDir()
		layerPath := filepath.Join(dir, "layer")
		if err := os.MkdirAll(layerPath, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(layerPath, "backend.tf"), []byte(`
terraform {
  backend "azurerm" {
    storage_account_name = "acct"
    container_name       = "container"
  }
}
`), 0o644); err != nil {
			t.Fatal(err)
		}

		mgr := NewManager(nil, nil)
		mgr.WithUploaderFactory(func(sa, cn string, _ azcore.TokenCredential) (BlobUploader, error) {
			return mock, nil
		})

		pruned, err := mgr.PruneStems(ctx, []string{"noexist"}, []string{layerPath})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if pruned != 0 {
			t.Errorf("expected 0 pruned, got %d", pruned)
		}
		if len(mock.deleted) != 0 {
			t.Errorf("expected 0 deletions, got %d", len(mock.deleted))
		}
	})

	t.Run("prunes across multiple layers", func(t *testing.T) {
		mock := newMockUploader()
		mock.setBlobs("migrations/migration.001_move.", []string{
			"migrations/migration.001_move.a1b2c3d4.tf",
		})

		dir := t.TempDir()
		backendHCL := []byte(`
terraform {
  backend "azurerm" {
    storage_account_name = "sharedacct"
    container_name       = "sharedcontainer"
  }
}
`)
		layer1 := filepath.Join(dir, "layer1")
		layer2 := filepath.Join(dir, "layer2")
		for _, l := range []string{layer1, layer2} {
			if err := os.MkdirAll(l, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(l, "backend.tf"), backendHCL, 0o644); err != nil {
				t.Fatal(err)
			}
		}

		mgr := NewManager(nil, nil)
		mgr.WithUploaderFactory(func(sa, cn string, _ azcore.TokenCredential) (BlobUploader, error) {
			return mock, nil
		})

		pruned, err := mgr.PruneStems(ctx, []string{"001_move"}, []string{layer1, layer2})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Same uploader (cached), so blob is listed twice but only one deletion per call
		// The mock returns the same blob for both layers since they share an uploader
		if pruned < 1 {
			t.Errorf("expected at least 1 pruned, got %d", pruned)
		}
	})
}

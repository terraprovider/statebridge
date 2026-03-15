package upload

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
)

func TestCheckActiveBlobs_GuardTriggered(t *testing.T) {
	ctx := context.Background()
	mock := newMockUploader()

	// Pre-populate an existing blob with same stem but different hash
	existingBlob := "migrations/migration.001_move.oldolold.tf"
	mock.setBlobs("migrations/migration.001_move.", []string{existingBlob})
	mock.setBlobData(existingBlob, []byte("# existing migration content"))

	// Guard checker says the blob is still active
	guardChecker := func(_ context.Context, _ []byte, _ string) (bool, error) {
		return true, nil
	}

	mgr := NewManager(nil, nil, WithGuardChecker(guardChecker))

	err := mgr.checkActiveBlobs(ctx, mock, "migration.001_move.newnew99.tf", "/layers/compute")
	if err == nil {
		t.Fatal("expected error when guard detects active blob")
	}
	if !strings.Contains(err.Error(), "refusing to overwrite") {
		t.Errorf("expected 'refusing to overwrite' in error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "--force") {
		t.Errorf("expected '--force' in error message, got: %v", err)
	}
}

func TestCheckActiveBlobs_GuardNotTriggered(t *testing.T) {
	ctx := context.Background()
	mock := newMockUploader()

	existingBlob := "migrations/migration.001_move.oldolold.tf"
	mock.setBlobs("migrations/migration.001_move.", []string{existingBlob})
	mock.setBlobData(existingBlob, []byte("# existing migration content"))

	// Guard checker says the blob is NOT active (conditions don't pass)
	guardChecker := func(_ context.Context, _ []byte, _ string) (bool, error) {
		return false, nil
	}

	mgr := NewManager(nil, nil, WithGuardChecker(guardChecker))

	err := mgr.checkActiveBlobs(ctx, mock, "migration.001_move.newnew99.tf", "/layers/compute")
	if err != nil {
		t.Fatalf("expected no error when guard passes, got: %v", err)
	}
}

func TestCheckActiveBlobs_ForceBypassesGuard(t *testing.T) {
	ctx := context.Background()
	mock := newMockUploader()

	existingBlob := "migrations/migration.001_move.oldolold.tf"
	mock.setBlobs("migrations/migration.001_move.", []string{existingBlob})
	mock.setBlobData(existingBlob, []byte("# existing migration content"))

	// Guard checker would reject, but --force should bypass
	guardChecker := func(_ context.Context, _ []byte, _ string) (bool, error) {
		t.Error("guard checker should not be called when force is true")
		return true, nil
	}

	mgr := NewManager(nil, nil, WithGuardChecker(guardChecker), WithForce(true))

	err := mgr.checkActiveBlobs(ctx, mock, "migration.001_move.newnew99.tf", "/layers/compute")
	if err != nil {
		t.Fatalf("expected no error with --force, got: %v", err)
	}
}

func TestCheckActiveBlobs_NoExistingBlobs(t *testing.T) {
	ctx := context.Background()
	mock := newMockUploader()

	guardChecker := func(_ context.Context, _ []byte, _ string) (bool, error) {
		t.Error("guard checker should not be called when no existing blobs")
		return true, nil
	}

	mgr := NewManager(nil, nil, WithGuardChecker(guardChecker))

	err := mgr.checkActiveBlobs(ctx, mock, "migration.001_move.newnew99.tf", "/layers/compute")
	if err != nil {
		t.Fatalf("expected no error with no existing blobs, got: %v", err)
	}
}

func TestCheckActiveBlobs_NoGuardChecker(t *testing.T) {
	ctx := context.Background()
	mock := newMockUploader()

	existingBlob := "migrations/migration.001_move.oldolold.tf"
	mock.setBlobs("migrations/migration.001_move.", []string{existingBlob})
	mock.setBlobData(existingBlob, []byte("# existing migration content"))

	// No guard checker configured — should be a no-op
	mgr := NewManager(nil, nil)

	err := mgr.checkActiveBlobs(ctx, mock, "migration.001_move.newnew99.tf", "/layers/compute")
	if err != nil {
		t.Fatalf("expected no error without guard checker, got: %v", err)
	}
}

func TestCheckActiveBlobs_SameHashSkipped(t *testing.T) {
	ctx := context.Background()
	mock := newMockUploader()

	// Existing blob has the same hash as the new one — should not be checked
	sameBlob := "migrations/migration.001_move.newnew99.tf"
	mock.setBlobs("migrations/migration.001_move.", []string{sameBlob})
	mock.setBlobData(sameBlob, []byte("# same content"))

	guardChecker := func(_ context.Context, _ []byte, _ string) (bool, error) {
		t.Error("guard checker should not be called for same-hash blob")
		return true, nil
	}

	mgr := NewManager(nil, nil, WithGuardChecker(guardChecker))

	err := mgr.checkActiveBlobs(ctx, mock, "migration.001_move.newnew99.tf", "/layers/compute")
	if err != nil {
		t.Fatalf("expected no error for same-hash blob, got: %v", err)
	}
}

func TestCheckActiveBlobs_GuardEvalError(t *testing.T) {
	ctx := context.Background()
	mock := newMockUploader()

	existingBlob := "migrations/migration.001_move.oldolold.tf"
	mock.setBlobs("migrations/migration.001_move.", []string{existingBlob})
	mock.setBlobData(existingBlob, []byte("# existing migration content"))

	// Guard checker returns an error — should warn but not block
	guardChecker := func(_ context.Context, _ []byte, _ string) (bool, error) {
		return false, fmt.Errorf("state read failed")
	}

	mgr := NewManager(nil, nil, WithGuardChecker(guardChecker))

	err := mgr.checkActiveBlobs(ctx, mock, "migration.001_move.newnew99.tf", "/layers/compute")
	if err != nil {
		t.Fatalf("expected no blocking error on guard eval failure, got: %v", err)
	}
}

func TestUploadFileIntegration_GuardBlocksUpload(t *testing.T) {
	ctx := context.Background()
	mock := newMockUploader()

	existingBlob := "migrations/migration.001_move.oldolold.tf"
	mock.setBlobs("migrations/migration.001_move.", []string{existingBlob})
	mock.setBlobData(existingBlob, []byte("# active migration"))

	guardChecker := func(_ context.Context, _ []byte, _ string) (bool, error) {
		return true, nil
	}

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

	mgr := NewManager(nil, nil, WithGuardChecker(guardChecker))
	mgr.WithUploaderFactory(func(_, _ string, _ azcore.TokenCredential) (BlobUploader, error) {
		return mock, nil
	})

	rendered := map[string]string{
		filepath.Join(layerPath, "migration.001_move.newnew99.tf"): "# new content\n",
	}

	err := mgr.UploadRendered(ctx, rendered)
	if err == nil {
		t.Fatal("expected error from guard blocking upload")
	}
	if !strings.Contains(err.Error(), "refusing to overwrite") {
		t.Errorf("expected 'refusing to overwrite' in error, got: %v", err)
	}

	// Verify nothing was uploaded
	if len(mock.uploaded) > 0 {
		t.Errorf("expected no uploads when guard blocks, got: %v", mock.uploaded)
	}
}

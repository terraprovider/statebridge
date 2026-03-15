package upload

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
)

// mockBlobUploader records all operations for verification.
type mockBlobUploader struct {
	mu       sync.Mutex
	uploaded map[string][]byte   // blobName -> content
	deleted  []string            // blob names deleted (in order)
	blobs    map[string][]string // prefix -> blob names returned by ListBlobs
	blobData map[string][]byte   // blobName -> content for DownloadBlob
}

func newMockUploader() *mockBlobUploader {
	return &mockBlobUploader{
		uploaded: make(map[string][]byte),
		blobs:    make(map[string][]string),
		blobData: make(map[string][]byte),
	}
}

func (m *mockBlobUploader) Upload(_ context.Context, blobName string, content []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.uploaded[blobName] = append([]byte{}, content...)
	return nil
}

func (m *mockBlobUploader) ListBlobs(_ context.Context, prefix string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Return blobs that match the prefix
	var result []string
	for p, names := range m.blobs {
		if strings.HasPrefix(p, prefix) || strings.HasPrefix(prefix, p) {
			result = append(result, names...)
		}
	}
	// Also check exact prefix match
	if names, ok := m.blobs[prefix]; ok {
		// Deduplicate
		seen := make(map[string]bool)
		for _, n := range result {
			seen[n] = true
		}
		for _, n := range names {
			if !seen[n] {
				result = append(result, n)
			}
		}
	}
	return result, nil
}

func (m *mockBlobUploader) DeleteBlob(_ context.Context, blobName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deleted = append(m.deleted, blobName)
	return nil
}

func (m *mockBlobUploader) DownloadBlob(_ context.Context, blobName string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	data, ok := m.blobData[blobName]
	if !ok {
		// Also check uploaded content (uploaded blobs become downloadable)
		data, ok = m.uploaded[blobName]
		if !ok {
			return nil, fmt.Errorf("blob %q not found", blobName)
		}
	}
	return append([]byte{}, data...), nil
}

// setBlobData configures the mock to return specific content for a blob download.
func (m *mockBlobUploader) setBlobData(blobName string, content []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.blobData[blobName] = content
}

// setBlobs configures the mock to return specific blobs for a prefix.
func (m *mockBlobUploader) setBlobs(prefix string, names []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.blobs[prefix] = names
}

func TestYamlStemFromFilename(t *testing.T) {
	tests := []struct {
		filename string
		want     string
		wantErr  bool
	}{
		{"migration.001_move.a1b2c3d4.tf", "001_move", false},
		{"migration.002_rename_vpc.e5f6a7b8.tf", "002_rename_vpc", false},
		{"migration.003_multi.part.name.abc12345.tf", "003_multi.part.name", false},
		{"notamigration.tf", "", true},
		{"migration..tf", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			got, err := YamlStemFromFilename(tt.filename)
			if (err != nil) != tt.wantErr {
				t.Errorf("YamlStemFromFilename(%q) error = %v, wantErr %v", tt.filename, err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("YamlStemFromFilename(%q) = %q, want %q", tt.filename, got, tt.want)
			}
		})
	}
}

func TestCleanupOldVersions(t *testing.T) {
	ctx := context.Background()

	t.Run("deletes old versions", func(t *testing.T) {
		mock := newMockUploader()
		mock.setBlobs("migrations/migration.001_move.", []string{
			"migrations/migration.001_move.oldold00.tf",
			"migrations/migration.001_move.old12345.tf",
		})

		mgr := &Manager{uploadedInSession: make(map[string]bool)}
		err := mgr.cleanupOldVersions(ctx, mock, "migration.001_move.new99999.tf")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(mock.deleted) != 2 {
			t.Fatalf("expected 2 deletions, got %d: %v", len(mock.deleted), mock.deleted)
		}
	})

	t.Run("skips current version", func(t *testing.T) {
		mock := newMockUploader()
		mock.setBlobs("migrations/migration.001_move.", []string{
			"migrations/migration.001_move.a1b2c3d4.tf",
		})

		mgr := &Manager{uploadedInSession: make(map[string]bool)}
		err := mgr.cleanupOldVersions(ctx, mock, "migration.001_move.a1b2c3d4.tf")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(mock.deleted) != 0 {
			t.Fatalf("expected 0 deletions, got %d: %v", len(mock.deleted), mock.deleted)
		}
	})

	t.Run("no existing blobs is no-op", func(t *testing.T) {
		mock := newMockUploader()

		mgr := &Manager{uploadedInSession: make(map[string]bool)}
		err := mgr.cleanupOldVersions(ctx, mock, "migration.001_move.a1b2c3d4.tf")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(mock.deleted) != 0 {
			t.Fatalf("expected 0 deletions, got %d", len(mock.deleted))
		}
	})

	t.Run("skips blobs uploaded in current session", func(t *testing.T) {
		mock := newMockUploader()
		mock.setBlobs("migrations/migration.001_move.", []string{
			"migrations/migration.001_move.oldold00.tf",
			"migrations/migration.001_move.session11.tf",
			"migrations/migration.001_move.old12345.tf",
		})

		mgr := &Manager{uploadedInSession: make(map[string]bool)}
		// Mark one as uploaded in this session (e.g., from another layer)
		mgr.uploadedInSession["migrations/migration.001_move.session11.tf"] = true

		err := mgr.cleanupOldVersions(ctx, mock, "migration.001_move.new99999.tf")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Should only delete the 2 old versions, not the session one
		if len(mock.deleted) != 2 {
			t.Fatalf("expected 2 deletions, got %d: %v", len(mock.deleted), mock.deleted)
		}
		for _, deleted := range mock.deleted {
			if deleted == "migrations/migration.001_move.session11.tf" {
				t.Fatalf("should not have deleted session blob")
			}
		}
	})
}

func TestManagerUploadRendered(t *testing.T) {
	ctx := context.Background()
	mock := newMockUploader()

	// Set up a layer directory with backend config
	dir := t.TempDir()
	layerPath := filepath.Join(dir, "layers", "compute")
	if err := os.MkdirAll(layerPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(layerPath, "backend.tf"), []byte(`
terraform {
  backend "azurerm" {
    storage_account_name = "testacct"
    container_name       = "testcontainer"
  }
}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	mgr := NewManager(nil, nil)
	mgr.WithUploaderFactory(func(sa, cn string, _ azcore.TokenCredential) (BlobUploader, error) {
		if sa != "testacct" {
			return nil, fmt.Errorf("unexpected storage account: %s", sa)
		}
		if cn != "testcontainer" {
			return nil, fmt.Errorf("unexpected container: %s", cn)
		}
		return mock, nil
	})

	rendered := map[string]string{
		filepath.Join(layerPath, "migration.001_move.a1b2c3d4.tf"): "# Generated content\n",
	}

	if err := mgr.UploadRendered(ctx, rendered); err != nil {
		t.Fatalf("UploadRendered failed: %v", err)
	}

	if _, ok := mock.uploaded["migrations/migration.001_move.a1b2c3d4.tf"]; !ok {
		t.Error("expected blob to be uploaded")
	}
}

func TestManagerUploadFromDisk(t *testing.T) {
	ctx := context.Background()
	mock := newMockUploader()

	// Set up a layer directory with backend config and migration files
	dir := t.TempDir()
	layerPath := filepath.Join(dir, "layers", "net")
	if err := os.MkdirAll(layerPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(layerPath, "backend.tf"), []byte(`
terraform {
  backend "azurerm" {
    storage_account_name = "diskacct"
    container_name       = "diskcontainer"
  }
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(layerPath, "migration.002_rename.b2c3d4e5.tf"), []byte("# migration content"), 0o644); err != nil {
		t.Fatal(err)
	}

	mgr := NewManager(nil, nil)
	mgr.WithUploaderFactory(func(sa, cn string, _ azcore.TokenCredential) (BlobUploader, error) {
		return mock, nil
	})

	if err := mgr.UploadFromDisk(ctx, []string{layerPath}); err != nil {
		t.Fatalf("UploadFromDisk failed: %v", err)
	}

	if _, ok := mock.uploaded["migrations/migration.002_rename.b2c3d4e5.tf"]; !ok {
		t.Error("expected blob to be uploaded")
	}
}

func TestManagerUploaderCaching(t *testing.T) {
	ctx := context.Background()
	mock := newMockUploader()

	// Create two layer dirs with same backend config
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
		if err := os.WriteFile(filepath.Join(l, "migration.001_move.abcd1234.tf"), []byte("# content"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	factoryCalls := 0
	mgr := NewManager(nil, nil)
	mgr.WithUploaderFactory(func(sa, cn string, _ azcore.TokenCredential) (BlobUploader, error) {
		factoryCalls++
		return mock, nil
	})

	if err := mgr.UploadFromDisk(ctx, []string{layer1, layer2}); err != nil {
		t.Fatalf("UploadFromDisk failed: %v", err)
	}

	if factoryCalls != 1 {
		t.Errorf("expected 1 factory call (cached), got %d", factoryCalls)
	}
}

func TestManagerCleanupAndUpload(t *testing.T) {
	ctx := context.Background()
	mock := newMockUploader()

	// Pre-populate old blob
	mock.setBlobs("migrations/migration.001_move.", []string{
		"migrations/migration.001_move.oldolold.tf",
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

	rendered := map[string]string{
		filepath.Join(layerPath, "migration.001_move.newnew99.tf"): "# new content\n",
	}

	mgr := NewManager(nil, nil)
	mgr.WithUploaderFactory(func(sa, cn string, _ azcore.TokenCredential) (BlobUploader, error) {
		return mock, nil
	})

	if err := mgr.UploadRendered(ctx, rendered); err != nil {
		t.Fatalf("UploadRendered failed: %v", err)
	}

	// Verify old version was deleted
	if len(mock.deleted) != 1 {
		t.Fatalf("expected 1 deletion, got %d: %v", len(mock.deleted), mock.deleted)
	}
	if mock.deleted[0] != "migrations/migration.001_move.oldolold.tf" {
		t.Errorf("deleted wrong blob: %s", mock.deleted[0])
	}

	// Verify new version was uploaded
	if _, ok := mock.uploaded["migrations/migration.001_move.newnew99.tf"]; !ok {
		keys := make([]string, 0, len(mock.uploaded))
		for k := range mock.uploaded {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		t.Errorf("expected new blob to be uploaded, uploaded blobs: %v", keys)
	}
}

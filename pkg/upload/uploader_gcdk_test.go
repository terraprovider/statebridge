package upload

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"gocloud.dev/blob/fileblob"
)

func TestGoCDKUploader_RoundTrip(t *testing.T) {
	t.Helper()
	dir := t.TempDir()

	bucket, err := fileblob.OpenBucket(dir, nil)
	if err != nil {
		t.Fatalf("opening fileblob bucket: %v", err)
	}
	t.Cleanup(func() { _ = bucket.Close() })

	uploader := NewGoCDKUploader(bucket)
	ctx := context.Background()

	// Upload a blob
	content := []byte("hello world")
	if err := uploader.Upload(ctx, "test/hello.txt", content); err != nil {
		t.Fatalf("Upload: %v", err)
	}

	// Download it back
	got, err := uploader.DownloadBlob(ctx, "test/hello.txt")
	if err != nil {
		t.Fatalf("DownloadBlob: %v", err)
	}
	if string(got) != "hello world" {
		t.Errorf("DownloadBlob content = %q, want %q", got, "hello world")
	}

	// List blobs with prefix
	names, err := uploader.ListBlobs(ctx, "test/")
	if err != nil {
		t.Fatalf("ListBlobs: %v", err)
	}
	if len(names) != 1 || names[0] != "test/hello.txt" {
		t.Errorf("ListBlobs = %v, want [test/hello.txt]", names)
	}

	// Delete it
	if err := uploader.DeleteBlob(ctx, "test/hello.txt"); err != nil {
		t.Fatalf("DeleteBlob: %v", err)
	}

	// Verify it's gone
	names, err = uploader.ListBlobs(ctx, "test/")
	if err != nil {
		t.Fatalf("ListBlobs after delete: %v", err)
	}
	if len(names) != 0 {
		t.Errorf("ListBlobs after delete = %v, want empty", names)
	}
}

func TestGoCDKUploader_ListMultiple(t *testing.T) {
	dir := t.TempDir()

	bucket, err := fileblob.OpenBucket(dir, nil)
	if err != nil {
		t.Fatalf("opening fileblob bucket: %v", err)
	}
	t.Cleanup(func() { _ = bucket.Close() })

	uploader := NewGoCDKUploader(bucket)
	ctx := context.Background()

	// Upload multiple blobs
	blobs := map[string]string{
		"migrations/migration.001_move.a1b2c3d4.tf":   "content1",
		"migrations/migration.001_move.e5f6g7h8.tf":   "content2",
		"migrations/migration.002_rename.abcd1234.tf": "content3",
		"other/unrelated.txt":                         "content4",
	}
	for name, content := range blobs {
		if err := uploader.Upload(ctx, name, []byte(content)); err != nil {
			t.Fatalf("Upload %q: %v", name, err)
		}
	}

	// List with prefix matching only 001_move
	names, err := uploader.ListBlobs(ctx, "migrations/migration.001_move.")
	if err != nil {
		t.Fatalf("ListBlobs: %v", err)
	}
	sort.Strings(names)
	want := []string{
		"migrations/migration.001_move.a1b2c3d4.tf",
		"migrations/migration.001_move.e5f6g7h8.tf",
	}
	if len(names) != len(want) {
		t.Fatalf("ListBlobs count = %d, want %d: %v", len(names), len(want), names)
	}
	for i, name := range names {
		if name != want[i] {
			t.Errorf("ListBlobs[%d] = %q, want %q", i, name, want[i])
		}
	}
}

func TestGoCDKUploader_UploadOverwrite(t *testing.T) {
	dir := t.TempDir()

	bucket, err := fileblob.OpenBucket(dir, nil)
	if err != nil {
		t.Fatalf("opening fileblob bucket: %v", err)
	}
	t.Cleanup(func() { _ = bucket.Close() })

	uploader := NewGoCDKUploader(bucket)
	ctx := context.Background()

	// Upload
	if err := uploader.Upload(ctx, "test.txt", []byte("version1")); err != nil {
		t.Fatalf("Upload v1: %v", err)
	}

	// Overwrite
	if err := uploader.Upload(ctx, "test.txt", []byte("version2")); err != nil {
		t.Fatalf("Upload v2: %v", err)
	}

	// Verify overwrite
	got, err := uploader.DownloadBlob(ctx, "test.txt")
	if err != nil {
		t.Fatalf("DownloadBlob: %v", err)
	}
	if string(got) != "version2" {
		t.Errorf("DownloadBlob = %q, want %q", got, "version2")
	}
}

func TestGoCDKUploader_DownloadNonExistent(t *testing.T) {
	dir := t.TempDir()

	bucket, err := fileblob.OpenBucket(dir, nil)
	if err != nil {
		t.Fatalf("opening fileblob bucket: %v", err)
	}
	t.Cleanup(func() { _ = bucket.Close() })

	uploader := NewGoCDKUploader(bucket)
	ctx := context.Background()

	_, err = uploader.DownloadBlob(ctx, "does/not/exist.txt")
	if err == nil {
		t.Fatal("expected error downloading non-existent blob")
	}
}

func TestOpenLocalBucket(t *testing.T) {
	dir := t.TempDir()

	bucket, err := openLocalBucket(context.Background(), &LocalBackendConfig{Path: dir})
	if err != nil {
		t.Fatalf("openLocalBucket: %v", err)
	}
	defer func() { _ = bucket.Close() }()

	// Write through bucket, verify file exists on disk
	ctx := context.Background()
	if err := bucket.WriteAll(ctx, "hello.txt", []byte("world"), nil); err != nil {
		t.Fatalf("WriteAll: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "hello.txt"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "world" {
		t.Errorf("file content = %q, want %q", data, "world")
	}
}

func TestDefaultUploaderFactory_Local(t *testing.T) {
	dir := t.TempDir()

	uploader, err := DefaultUploaderFactory(&LocalBackendConfig{Path: dir})
	if err != nil {
		t.Fatalf("DefaultUploaderFactory: %v", err)
	}

	ctx := context.Background()
	if err := uploader.Upload(ctx, "test.txt", []byte("hello")); err != nil {
		t.Fatalf("Upload: %v", err)
	}

	got, err := uploader.DownloadBlob(ctx, "test.txt")
	if err != nil {
		t.Fatalf("DownloadBlob: %v", err)
	}
	if string(got) != "hello" {
		t.Errorf("DownloadBlob = %q, want %q", got, "hello")
	}
}

func TestDefaultUploaderFactory_AzurermRequiresCredential(t *testing.T) {
	_, err := DefaultUploaderFactory(&AzurermBackendConfig{
		StorageAccountName: "test",
		ContainerName:      "test",
	})
	if err == nil {
		t.Fatal("expected error for azurerm without credential")
	}
}

func TestBucketUploaderFactory_NilCredAzurerm(t *testing.T) {
	factory := BucketUploaderFactory(nil)
	_, err := factory(&AzurermBackendConfig{
		StorageAccountName: "test",
		ContainerName:      "test",
	})
	if err == nil {
		t.Fatal("expected error for azurerm with nil credential")
	}
}

func TestBucketUploaderFactory_Local(t *testing.T) {
	dir := t.TempDir()
	factory := BucketUploaderFactory(nil)

	uploader, err := factory(&LocalBackendConfig{Path: dir})
	if err != nil {
		t.Fatalf("BucketUploaderFactory local: %v", err)
	}

	ctx := context.Background()
	if err := uploader.Upload(ctx, "test.txt", []byte("hello")); err != nil {
		t.Fatalf("Upload: %v", err)
	}

	got, err := uploader.DownloadBlob(ctx, "test.txt")
	if err != nil {
		t.Fatalf("DownloadBlob: %v", err)
	}
	if string(got) != "hello" {
		t.Errorf("DownloadBlob = %q, want %q", got, "hello")
	}
}

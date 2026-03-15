package upload

import (
	"context"
	"fmt"
	"io"

	"gocloud.dev/blob"
)

// GoCDKUploader implements BlobUploader using gocloud.dev/blob.Bucket.
// It provides a unified interface across Azure, S3, GCS, and local filesystem.
type GoCDKUploader struct {
	bucket *blob.Bucket
}

// NewGoCDKUploader creates an uploader backed by the given gocloud.dev bucket.
func NewGoCDKUploader(bucket *blob.Bucket) *GoCDKUploader {
	return &GoCDKUploader{bucket: bucket}
}

// Upload creates or overwrites a blob with the given content.
func (u *GoCDKUploader) Upload(ctx context.Context, blobName string, content []byte) error {
	if err := u.bucket.WriteAll(ctx, blobName, content, nil); err != nil {
		return fmt.Errorf("uploading blob %q: %w", blobName, err)
	}
	return nil
}

// ListBlobs returns blob names matching the given prefix.
func (u *GoCDKUploader) ListBlobs(ctx context.Context, prefix string) ([]string, error) {
	var names []string
	iter := u.bucket.List(&blob.ListOptions{Prefix: prefix})
	for {
		obj, err := iter.Next(ctx)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("listing blobs with prefix %q: %w", prefix, err)
		}
		names = append(names, obj.Key)
	}
	return names, nil
}

// DeleteBlob deletes a single blob by name.
func (u *GoCDKUploader) DeleteBlob(ctx context.Context, blobName string) error {
	if err := u.bucket.Delete(ctx, blobName); err != nil {
		return fmt.Errorf("deleting blob %q: %w", blobName, err)
	}
	return nil
}

// DownloadBlob downloads a blob's content by name.
func (u *GoCDKUploader) DownloadBlob(ctx context.Context, blobName string) ([]byte, error) {
	data, err := u.bucket.ReadAll(ctx, blobName)
	if err != nil {
		return nil, fmt.Errorf("downloading blob %q: %w", blobName, err)
	}
	return data, nil
}

// Close releases resources held by the underlying bucket.
func (u *GoCDKUploader) Close() error {
	return u.bucket.Close()
}

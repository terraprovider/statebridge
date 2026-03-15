package upload

import (
	"context"
)

// BlobUploader abstracts blob storage operations for testability.
type BlobUploader interface {
	// Upload creates or overwrites a blob with the given content.
	Upload(ctx context.Context, blobName string, content []byte) error

	// ListBlobs returns blob names matching the given prefix.
	ListBlobs(ctx context.Context, prefix string) ([]string, error)

	// DeleteBlob deletes a single blob by name.
	DeleteBlob(ctx context.Context, blobName string) error

	// DownloadBlob downloads a blob's content by name.
	DownloadBlob(ctx context.Context, blobName string) ([]byte, error)
}

package upload

import (
	"context"
	"fmt"
	"io"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
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

// AzureBlobUploader implements BlobUploader using the Azure Blob Storage SDK.
type AzureBlobUploader struct {
	client        *azblob.Client
	containerName string
}

// NewAzureBlobUploader creates an uploader targeting the given storage account
// and container, authenticated with the provided credential.
func NewAzureBlobUploader(
	storageAccountName string,
	containerName string,
	cred azcore.TokenCredential,
) (*AzureBlobUploader, error) {
	serviceURL := fmt.Sprintf("https://%s.blob.core.windows.net", storageAccountName)
	client, err := azblob.NewClient(serviceURL, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("creating blob client for %s: %w", serviceURL, err)
	}
	return &AzureBlobUploader{client: client, containerName: containerName}, nil
}

// Upload creates or overwrites a blob with the given content.
func (u *AzureBlobUploader) Upload(ctx context.Context, blobName string, content []byte) error {
	_, err := u.client.UploadBuffer(ctx, u.containerName, blobName, content, nil)
	if err != nil {
		return fmt.Errorf("uploading blob %q to container %q: %w", blobName, u.containerName, err)
	}
	return nil
}

// ListBlobs returns all blob names matching the given prefix.
func (u *AzureBlobUploader) ListBlobs(ctx context.Context, prefix string) ([]string, error) {
	var names []string
	pager := u.client.NewListBlobsFlatPager(u.containerName, &azblob.ListBlobsFlatOptions{
		Prefix: &prefix,
	})
	for pager.More() {
		resp, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("listing blobs with prefix %q in container %q: %w", prefix, u.containerName, err)
		}
		for _, item := range resp.Segment.BlobItems {
			if item.Name != nil {
				names = append(names, *item.Name)
			}
		}
	}
	return names, nil
}

// DeleteBlob deletes a single blob by name.
func (u *AzureBlobUploader) DeleteBlob(ctx context.Context, blobName string) error {
	_, err := u.client.DeleteBlob(ctx, u.containerName, blobName, nil)
	if err != nil {
		return fmt.Errorf("deleting blob %q from container %q: %w", blobName, u.containerName, err)
	}
	return nil
}

// DownloadBlob downloads a blob's content by name.
func (u *AzureBlobUploader) DownloadBlob(ctx context.Context, blobName string) ([]byte, error) {
	resp, err := u.client.DownloadStream(ctx, u.containerName, blobName, nil)
	if err != nil {
		return nil, fmt.Errorf("downloading blob %q from container %q: %w", blobName, u.containerName, err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading blob %q content: %w", blobName, err)
	}
	return data, nil
}

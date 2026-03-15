package upload

import (
	"context"
	"fmt"
	"net/http"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/container"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	s3v2 "github.com/aws/aws-sdk-go-v2/service/s3"
	"gocloud.dev/blob"
	"gocloud.dev/blob/azureblob"
	"gocloud.dev/blob/fileblob"
	"gocloud.dev/blob/gcsblob"
	"gocloud.dev/blob/s3blob"
	"gocloud.dev/gcp"
)

// openAzureBucket opens an Azure Blob Storage bucket via gocloud.dev.
func openAzureBucket(ctx context.Context, cfg *AzurermBackendConfig, cred azcore.TokenCredential) (*blob.Bucket, error) {
	containerURL := fmt.Sprintf("https://%s.blob.core.windows.net/%s", cfg.StorageAccountName, cfg.ContainerName)
	client, err := container.NewClient(containerURL, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("creating Azure container client for %s: %w", containerURL, err)
	}
	bucket, err := azureblob.OpenBucket(ctx, client, nil)
	if err != nil {
		return nil, fmt.Errorf("opening Azure bucket %s/%s: %w", cfg.StorageAccountName, cfg.ContainerName, err)
	}
	return bucket, nil
}

// openS3Bucket opens an AWS S3 bucket via gocloud.dev.
// Credentials are resolved via the default AWS SDK credential chain
// (env vars, shared config, IAM role, etc.).
func openS3Bucket(ctx context.Context, cfg *S3BackendConfig) (*blob.Bucket, error) {
	opts := []func(*awsconfig.LoadOptions) error{}
	if cfg.Region != "" {
		opts = append(opts, awsconfig.WithRegion(cfg.Region))
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("loading AWS config: %w", err)
	}

	s3Client := s3v2.NewFromConfig(awsCfg, func(o *s3v2.Options) {
		if cfg.Endpoint != "" {
			o.BaseEndpoint = &cfg.Endpoint
			o.UsePathStyle = true
		}
	})

	bucket, err := s3blob.OpenBucketV2(ctx, s3Client, cfg.Bucket, nil)
	if err != nil {
		return nil, fmt.Errorf("opening S3 bucket %q: %w", cfg.Bucket, err)
	}
	return bucket, nil
}

// openGCSBucket opens a Google Cloud Storage bucket via gocloud.dev.
// Credentials are resolved via the default Google SDK credential chain
// (GOOGLE_APPLICATION_CREDENTIALS, gcloud CLI, metadata server, etc.).
func openGCSBucket(ctx context.Context, cfg *GCSBackendConfig) (*blob.Bucket, error) {
	creds, err := gcp.DefaultCredentials(ctx)
	if err != nil {
		return nil, fmt.Errorf("obtaining GCP credentials: %w", err)
	}

	client, err := gcp.NewHTTPClient(http.DefaultTransport, gcp.CredentialsTokenSource(creds))
	if err != nil {
		return nil, fmt.Errorf("creating GCS HTTP client: %w", err)
	}

	bucket, err := gcsblob.OpenBucket(ctx, client, cfg.Bucket, nil)
	if err != nil {
		return nil, fmt.Errorf("opening GCS bucket %q: %w", cfg.Bucket, err)
	}
	return bucket, nil
}

// openLocalBucket opens a local filesystem bucket via gocloud.dev/blob/fileblob.
func openLocalBucket(_ context.Context, cfg *LocalBackendConfig) (*blob.Bucket, error) {
	bucket, err := fileblob.OpenBucket(cfg.Path, nil)
	if err != nil {
		return nil, fmt.Errorf("opening local bucket at %q: %w", cfg.Path, err)
	}
	return bucket, nil
}

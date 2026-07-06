// Package download fetches migration files from Azure Blob Storage,
// evaluates their conditions, and writes applicable ones to disk.
package download

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"

	"github.com/terraprovider/statebridge/pkg/conditions"
	"github.com/terraprovider/statebridge/pkg/generator"
	"github.com/terraprovider/statebridge/pkg/state"
	"github.com/terraprovider/statebridge/pkg/upload"
)

// Downloader fetches migration files from Azure Blob Storage, evaluates
// conditions against the layer's state, and writes applicable ones to disk.
type Downloader struct {
	cred            azcore.TokenCredential
	initArgs        []string
	tofuPath        string
	dryRun          bool
	uploaderFactory upload.UploaderFactory
}

// DownloaderOption configures optional Downloader behaviour.
type DownloaderOption func(*Downloader)

// WithTofuPath sets the path to the tofu binary.
func WithTofuPath(p string) DownloaderOption {
	return func(d *Downloader) { d.tofuPath = p }
}

// WithDryRun enables dry-run mode (no files written).
func WithDryRun(v bool) DownloaderOption {
	return func(d *Downloader) { d.dryRun = v }
}

// WithUploaderFactory replaces the default uploader factory (for testing).
func WithUploaderFactory(f upload.UploaderFactory) DownloaderOption {
	return func(d *Downloader) { d.uploaderFactory = f }
}

// NewDownloader creates a Downloader with the given credential and options.
func NewDownloader(cred azcore.TokenCredential, initArgs []string, opts ...DownloaderOption) *Downloader {
	d := &Downloader{
		cred:            cred,
		initArgs:        initArgs,
		uploaderFactory: upload.DefaultUploaderFactory,
	}
	for _, opt := range opts {
		opt(d)
	}
	return d
}

// Download fetches migrations from the layer's blob container, evaluates
// conditions against the layer's state, and writes applicable files to targetDir.
// Returns the list of written file paths.
func (d *Downloader) Download(ctx context.Context, targetDir string) ([]string, error) {
	config, err := upload.DiscoverBackendConfig(targetDir, d.initArgs)
	if err != nil {
		return nil, fmt.Errorf("discovering backend config: %w", err)
	}

	cred, err := upload.ResolveCredential(d.cred, config)
	if err != nil {
		return nil, fmt.Errorf("resolving credentials: %w", err)
	}

	uploader, err := d.uploaderFactory(config.StorageAccountName, config.ContainerName, cred)
	if err != nil {
		return nil, fmt.Errorf("creating blob client: %w", err)
	}

	blobs, err := uploader.ListBlobs(ctx, "migrations/")
	if err != nil {
		return nil, fmt.Errorf("listing migration blobs: %w", err)
	}

	// Filter to migration.*.tf files
	var migrationBlobs []string
	for _, b := range blobs {
		name := filepath.Base(b)
		if strings.HasPrefix(name, "migration.") && strings.HasSuffix(name, ".tf") {
			migrationBlobs = append(migrationBlobs, b)
		}
	}

	if len(migrationBlobs) == 0 {
		fmt.Fprintf(os.Stderr, "No migration files found in container %q\n", config.ContainerName)
		return nil, nil
	}

	// Create state reader eagerly for condition evaluation.
	stateReader := state.NewTofuStateReader(d.tofuPath, d.initArgs)

	// Clean up all existing migration.*.tf files in the target directory.
	// Blob storage is the source of truth, so stale local files must be
	// removed before writing fresh copies to avoid interfering with tofu.
	existingPattern := filepath.Join(targetDir, "migration.*.tf")
	existingFiles, err := filepath.Glob(existingPattern)
	if err != nil {
		return nil, fmt.Errorf("scanning existing migration files: %w", err)
	}
	for _, f := range existingFiles {
		if d.dryRun {
			fmt.Fprintf(os.Stderr, "Would remove old migration: %s\n", f)
			continue
		}
		if err := os.Remove(f); err != nil {
			return nil, fmt.Errorf("removing old migration %q: %w", f, err)
		}
		fmt.Fprintf(os.Stderr, "Removed old migration: %s\n", f)
	}

	var written []string
	for _, blobName := range migrationBlobs {
		content, err := uploader.DownloadBlob(ctx, blobName)
		if err != nil {
			return written, fmt.Errorf("downloading %q: %w", blobName, err)
		}

		meta, err := generator.ParseMetadataComment(string(content))
		if err != nil {
			fmt.Fprintf(os.Stderr, "Skipping %q: could not parse metadata: %v\n", blobName, err)
			continue
		}

		filename := filepath.Base(blobName)

		// Scope by source layer: in a shared container, a blob may belong to a
		// different layer. Skip blobs that provably belong elsewhere; when the
		// ownership is undecidable (this layer's key can't be determined), warn
		// and fall back to condition-based applicability.
		if meta != nil && meta.SourceLayer != nil {
			match, determinable := meta.SourceLayer.Matches(
				config.StorageAccountName, config.ContainerName, config.Key)
			switch {
			case !determinable:
				fmt.Fprintf(os.Stderr,
					"Warning: cannot determine this layer's backend key; falling back to conditions for %s\n",
					filename)
			case !match:
				fmt.Fprintf(os.Stderr, "Skipping %s: belongs to another layer\n", filename)
				continue
			}
		}

		// Evaluate conditions if metadata is present
		if meta != nil && meta.Conditions != nil {
			applicable, err := d.evaluateConditions(ctx, meta, stateReader, targetDir)
			if err != nil {
				return written, fmt.Errorf("evaluating conditions for %q: %w", filename, err)
			}
			if !applicable {
				fmt.Fprintf(os.Stderr, "Skipping %s: conditions not met\n", filename)
				continue
			}
		}

		outPath := filepath.Join(targetDir, filename)
		if d.dryRun {
			fmt.Fprintf(os.Stdout, "Would write: %s\n", outPath)
			written = append(written, outPath)
			continue
		}

		if err := os.WriteFile(outPath, content, 0o644); err != nil {
			return written, fmt.Errorf("writing %q: %w", outPath, err)
		}
		fmt.Fprintf(os.Stdout, "Downloaded: %s\n", outPath)
		written = append(written, outPath)
	}

	return written, nil
}

// evaluateConditions checks whether a migration's conditions are met.
// Delegates to the shared conditions package.
func (d *Downloader) evaluateConditions(
	ctx context.Context,
	meta *generator.MigrationMetadata,
	reader state.StateReader,
	targetDir string,
) (bool, error) {
	readState := conditions.NewStateReaderFunc(reader)
	return conditions.EvaluateMetadataConditions(ctx, meta, readState, targetDir)
}

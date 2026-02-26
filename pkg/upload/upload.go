package upload

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"

	"github.com/redtenant/tfmigrate/pkg/conditions"
	"github.com/redtenant/tfmigrate/pkg/generator"
	"github.com/redtenant/tfmigrate/pkg/state"
)

// UploaderFactory creates BlobUploader instances for a given storage account
// and container. Used for dependency injection in tests.
type UploaderFactory func(storageAccountName, containerName string, cred azcore.TokenCredential) (BlobUploader, error)

// DefaultUploaderFactory creates AzureBlobUploader instances.
func DefaultUploaderFactory(storageAccountName, containerName string, cred azcore.TokenCredential) (BlobUploader, error) {
	return NewAzureBlobUploader(storageAccountName, containerName, cred)
}

// GuardChecker evaluates whether an existing blob's migration conditions are
// still met. Returns true if the blob is still active (should NOT be overwritten).
type GuardChecker func(ctx context.Context, blobContent []byte, layerPath string) (bool, error)

// Manager orchestrates upload of generated migration files to Azure Blob Storage.
// It discovers backend configuration per layer and manages uploader lifecycle.
type Manager struct {
	cred            azcore.TokenCredential
	uploaderFactory UploaderFactory
	initArgs        []string
	uploaderCache   map[string]BlobUploader // keyed by "account|container"
	force           bool
	guardChecker    GuardChecker
}

// NewManager creates an upload Manager with the given credential and
// init args (used for backend config discovery in all layers).
func NewManager(cred azcore.TokenCredential, initArgs []string, opts ...ManagerOption) *Manager {
	m := &Manager{
		cred:            cred,
		uploaderFactory: DefaultUploaderFactory,
		initArgs:        initArgs,
		uploaderCache:   make(map[string]BlobUploader),
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// ManagerOption configures optional Manager behaviour.
type ManagerOption func(*Manager)

// WithForce disables the upload guard, allowing overwrite of active migrations.
func WithForce(force bool) ManagerOption {
	return func(m *Manager) { m.force = force }
}

// WithTofuPath enables the upload guard using the given tofu binary.
// The guard evaluates existing blob metadata conditions against the
// layer's state to prevent overwriting still-active migrations.
func WithTofuPath(tofuPath string, initArgs []string) ManagerOption {
	return func(m *Manager) {
		reader := state.NewTofuStateReader(tofuPath, initArgs)
		m.guardChecker = defaultGuardChecker(reader)
	}
}

// WithGuardChecker injects a custom guard checker (primarily for testing).
func WithGuardChecker(gc GuardChecker) ManagerOption {
	return func(m *Manager) { m.guardChecker = gc }
}

// WithUploaderFactory replaces the default uploader factory.
// Used in tests to inject a mock. Returns m for chaining.
func (m *Manager) WithUploaderFactory(factory UploaderFactory) *Manager {
	m.uploaderFactory = factory
	return m
}

// defaultGuardChecker creates a GuardChecker that parses migration metadata
// and evaluates conditions against real layer state.
func defaultGuardChecker(reader state.StateReader) GuardChecker {
	return func(ctx context.Context, blobContent []byte, layerPath string) (bool, error) {
		meta, err := generator.ParseMetadataComment(string(blobContent))
		if err != nil {
			return false, fmt.Errorf("parsing metadata: %w", err)
		}
		if meta == nil || meta.Conditions == nil {
			return false, nil // no metadata = not guarded
		}
		readState := conditions.NewStateReaderFunc(reader)
		return conditions.EvaluateMetadataConditions(ctx, meta, readState, layerPath)
	}
}

// UploadRendered uploads generated files from Writer.RenderAll() output.
// The rendered map keys are output file paths (e.g., "layers/net/migration.001.a1b2c3d4.tf")
// and values are the rendered HCL content strings.
func (m *Manager) UploadRendered(ctx context.Context, rendered map[string]string) error {
	// Group files by layer directory
	type layerFile struct {
		filename string
		content  []byte
	}
	layerFiles := make(map[string][]layerFile)

	for outPath, content := range rendered {
		layerPath := filepath.Dir(outPath)
		filename := filepath.Base(outPath)
		layerFiles[layerPath] = append(layerFiles[layerPath], layerFile{
			filename: filename,
			content:  []byte(content),
		})
	}

	for layerPath, files := range layerFiles {
		uploader, err := m.getUploader(layerPath)
		if err != nil {
			return fmt.Errorf("layer %q: %w", layerPath, err)
		}

		for _, f := range files {
			if err := m.uploadFile(ctx, uploader, f.filename, f.content, layerPath); err != nil {
				return fmt.Errorf("layer %q, file %q: %w", layerPath, f.filename, err)
			}
		}
	}

	return nil
}

// UploadFromDisk uploads pre-generated migration files from layer directories.
// Each path in layerPaths is scanned for migration.*.tf files.
func (m *Manager) UploadFromDisk(ctx context.Context, layerPaths []string) error {
	for _, layerPath := range layerPaths {
		pattern := filepath.Join(layerPath, "migration.*.tf")
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return fmt.Errorf("scanning migration files in %q: %w", layerPath, err)
		}

		if len(matches) == 0 {
			fmt.Fprintf(os.Stderr, "No migration files found in %s\n", layerPath)
			continue
		}

		uploader, err := m.getUploader(layerPath)
		if err != nil {
			return fmt.Errorf("layer %q: %w", layerPath, err)
		}

		for _, match := range matches {
			content, err := os.ReadFile(match)
			if err != nil {
				return fmt.Errorf("reading %q: %w", match, err)
			}

			filename := filepath.Base(match)
			if err := m.uploadFile(ctx, uploader, filename, content, layerPath); err != nil {
				return fmt.Errorf("layer %q, file %q: %w", layerPath, filename, err)
			}
		}
	}

	return nil
}

// getUploader returns a cached BlobUploader for the given layer path.
// On first call for a layer, it discovers backend config and creates an uploader.
func (m *Manager) getUploader(layerPath string) (BlobUploader, error) {
	config, err := DiscoverBackendConfig(layerPath, m.initArgs)
	if err != nil {
		return nil, err
	}

	cacheKey := config.StorageAccountName + "|" + config.ContainerName
	if uploader, ok := m.uploaderCache[cacheKey]; ok {
		return uploader, nil
	}

	uploader, err := m.uploaderFactory(config.StorageAccountName, config.ContainerName, m.cred)
	if err != nil {
		return nil, fmt.Errorf("creating uploader for %s/%s: %w", config.StorageAccountName, config.ContainerName, err)
	}

	m.uploaderCache[cacheKey] = uploader
	return uploader, nil
}

// uploadFile handles guard check, cleanup of old versions, and upload of a single file.
func (m *Manager) uploadFile(ctx context.Context, uploader BlobUploader, filename string, content []byte, layerPath string) error {
	if err := m.checkActiveBlobs(ctx, uploader, filename, layerPath); err != nil {
		return err
	}

	if err := cleanupOldVersions(ctx, uploader, filename); err != nil {
		return err
	}

	blobName := "migrations/" + filename
	if err := uploader.Upload(ctx, blobName, content); err != nil {
		return err
	}

	fmt.Fprintf(os.Stdout, "Uploaded: %s\n", blobName)
	return nil
}

// checkActiveBlobs checks whether existing migration blobs with the same stem
// are still active (their metadata conditions still pass). If so, uploading
// would overwrite a still-needed migration, and the upload is refused.
//
// This guard can be bypassed with the --force flag.
// It is a no-op if no guardChecker is configured (e.g., when tofu binary
// is not available).
func (m *Manager) checkActiveBlobs(ctx context.Context, uploader BlobUploader, filename, layerPath string) error {
	if m.force || m.guardChecker == nil {
		return nil
	}

	stem, err := YamlStemFromFilename(filename)
	if err != nil {
		return err
	}

	prefix := "migrations/migration." + stem + "."
	existing, err := uploader.ListBlobs(ctx, prefix)
	if err != nil {
		return fmt.Errorf("listing existing blobs for guard check: %w", err)
	}

	newBlobName := "migrations/" + filename
	for _, blobName := range existing {
		if !strings.HasSuffix(blobName, ".tf") || blobName == newBlobName {
			continue
		}

		blobContent, err := uploader.DownloadBlob(ctx, blobName)
		if err != nil {
			// If we can't download the blob, warn but don't block
			fmt.Fprintf(os.Stderr, "Warning: could not download %q for guard check: %v\n", blobName, err)
			continue
		}

		active, err := m.guardChecker(ctx, blobContent, layerPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not evaluate guard for %q: %v\n", blobName, err)
			continue
		}

		if active {
			return fmt.Errorf(
				"refusing to overwrite %q: migration is still active in layer %q (conditions pass); use --force to override",
				blobName, layerPath,
			)
		}
	}

	return nil
}

// cleanupOldVersions finds and deletes blobs matching the pattern for old
// versions of the same migration file. Given a filename like
// "migration.001_move.a1b2c3d4.tf", it lists blobs with prefix
// "migrations/migration.001_move." and deletes any that end with ".tf"
// but differ from the new filename.
func cleanupOldVersions(ctx context.Context, uploader BlobUploader, filename string) error {
	stem, err := YamlStemFromFilename(filename)
	if err != nil {
		return err
	}

	prefix := "migrations/migration." + stem + "."
	existing, err := uploader.ListBlobs(ctx, prefix)
	if err != nil {
		return fmt.Errorf("listing old versions for %q: %w", filename, err)
	}

	newBlobName := "migrations/" + filename
	for _, blobName := range existing {
		if strings.HasSuffix(blobName, ".tf") && blobName != newBlobName {
			if err := uploader.DeleteBlob(ctx, blobName); err != nil {
				return fmt.Errorf("removing old version %q: %w", blobName, err)
			}
			fmt.Fprintf(os.Stderr, "Removed old version: %s\n", blobName)
		}
	}

	return nil
}

// YamlStemFromFilename extracts the YAML stem from a generated migration filename.
// "migration.001_move.a1b2c3d4.tf" -> "001_move"
func YamlStemFromFilename(filename string) (string, error) {
	base := strings.TrimSuffix(filename, ".tf")
	base = strings.TrimPrefix(base, "migration.")
	lastDot := strings.LastIndex(base, ".")
	if lastDot <= 0 {
		return "", fmt.Errorf("unexpected filename format %q: cannot extract yaml stem", filename)
	}
	return base[:lastDot], nil
}

// PruneStems removes migration blobs matching the given stems from the specified layers.
// For each stem, it lists blobs with prefix "migrations/migration.<stem>." and deletes
// all matching .tf files. Returns the total number of blobs pruned.
func (m *Manager) PruneStems(ctx context.Context, stems []string, layerPaths []string) (int, error) {
	var totalPruned int
	for _, layerPath := range layerPaths {
		uploader, err := m.getUploader(layerPath)
		if err != nil {
			return totalPruned, fmt.Errorf("getting uploader for %q: %w", layerPath, err)
		}
		for _, stem := range stems {
			prefix := "migrations/migration." + stem + "."
			blobs, err := uploader.ListBlobs(ctx, prefix)
			if err != nil {
				return totalPruned, fmt.Errorf("listing blobs for stem %q in %q: %w", stem, layerPath, err)
			}
			for _, blob := range blobs {
				if !strings.HasSuffix(blob, ".tf") {
					continue
				}
				if err := uploader.DeleteBlob(ctx, blob); err != nil {
					return totalPruned, fmt.Errorf("deleting blob %q: %w", blob, err)
				}
				fmt.Fprintf(os.Stderr, "  Auto-pruned: %s\n", filepath.Base(blob))
				totalPruned++
			}
		}
	}
	return totalPruned, nil
}

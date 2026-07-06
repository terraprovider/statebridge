package upload

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
	cred              azcore.TokenCredential
	uploaderFactory   UploaderFactory
	initArgs          []string
	uploaderCache     map[string]BlobUploader           // keyed by "account|container"
	configCache       map[string]*BackendConfig         // keyed by layer path
	credCache         map[string]azcore.TokenCredential // keyed by layer path
	uploadedInSession map[string]bool                   // track blobs uploaded in this session to avoid cleanup
	force             bool
	guardChecker      GuardChecker
}

// NewManager creates an upload Manager with the given credential and
// init args (used for backend config discovery in all layers).
func NewManager(cred azcore.TokenCredential, initArgs []string, opts ...ManagerOption) *Manager {
	m := &Manager{
		cred:              cred,
		uploaderFactory:   DefaultUploaderFactory,
		initArgs:          initArgs,
		uploaderCache:     make(map[string]BlobUploader),
		configCache:       make(map[string]*BackendConfig),
		credCache:         make(map[string]azcore.TokenCredential),
		uploadedInSession: make(map[string]bool),
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
	config, err := m.getBackendConfig(layerPath)
	if err != nil {
		return nil, err
	}

	cacheKey := config.StorageAccountName + "|" + config.ContainerName
	if uploader, ok := m.uploaderCache[cacheKey]; ok {
		return uploader, nil
	}

	cred, err := m.getCredential(layerPath, config)
	if err != nil {
		return nil, err
	}

	uploader, err := m.uploaderFactory(config.StorageAccountName, config.ContainerName, cred)
	if err != nil {
		return nil, fmt.Errorf("creating uploader for %s/%s: %w", config.StorageAccountName, config.ContainerName, err)
	}

	m.uploaderCache[cacheKey] = uploader
	return uploader, nil
}

// getBackendConfig discovers and caches the backend config for a layer path.
func (m *Manager) getBackendConfig(layerPath string) (*BackendConfig, error) {
	if config, ok := m.configCache[layerPath]; ok {
		return config, nil
	}
	config, err := DiscoverBackendConfig(layerPath, m.initArgs)
	if err != nil {
		return nil, err
	}
	m.configCache[layerPath] = config
	return config, nil
}

// getCredential resolves and caches, per layer path, the credential to use
// for that layer's blob storage operations. When the layer's backend
// configuration carries no credential values, this is simply the Manager's
// shared base credential (see ResolveCredential).
func (m *Manager) getCredential(layerPath string, config *BackendConfig) (azcore.TokenCredential, error) {
	if cred, ok := m.credCache[layerPath]; ok {
		return cred, nil
	}
	cred, err := ResolveCredential(m.cred, config)
	if err != nil {
		return nil, fmt.Errorf("layer %q: %w", layerPath, err)
	}
	m.credCache[layerPath] = cred
	return cred, nil
}

// blobOwnedByOtherLayer reports whether the blob provably belongs to a layer
// other than the one identified by config. It downloads and parses the blob's
// metadata; on any download/parse error, missing metadata, or missing
// source_layer descriptor it returns false (fail-open: act as in a
// single-layer container). This scopes cross-layer operations in a shared
// container so one layer never deletes/guards another layer's blobs.
func (m *Manager) blobOwnedByOtherLayer(ctx context.Context, uploader BlobUploader, blobName string, config *BackendConfig) bool {
	if config == nil {
		return false // no coordinates to scope against ⇒ act as single-layer
	}
	content, err := uploader.DownloadBlob(ctx, blobName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not download %q for layer-scope check: %v\n", blobName, err)
		return false
	}
	return BlobContentOwnedByOtherLayer(content, config)
}

// BlobContentOwnedByOtherLayer is the pure predicate behind blobOwnedByOtherLayer,
// operating on already-downloaded content so callers that have the bytes need not
// re-download. It reports whether the blob's embedded source_layer descriptor
// provably identifies a layer other than the one described by config. Returns
// false on parse errors, missing metadata, a missing source_layer descriptor, or
// a nil config (fail-open: act as in a single-layer container).
func BlobContentOwnedByOtherLayer(content []byte, config *BackendConfig) bool {
	if config == nil {
		return false
	}
	meta, err := generator.ParseMetadataComment(string(content))
	if err != nil || meta == nil || meta.SourceLayer == nil {
		return false
	}
	return meta.SourceLayer.OwnedByOther(config.StorageAccountName, config.ContainerName, config.Key)
}

// uploadFile handles guard check, cleanup of old versions, and upload of a single file.
func (m *Manager) uploadFile(ctx context.Context, uploader BlobUploader, filename string, content []byte, layerPath string) error {
	blobName := "migrations/" + filename

	// Discover this layer's backend coordinates so guard/cleanup can scope
	// blobs to this layer in a shared container. Non-fatal on failure: fall
	// back to unscoped behaviour (config == nil ⇒ act as today).
	config, cfgErr := m.getBackendConfig(layerPath)
	if cfgErr != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not discover backend config for %q, blob scoping disabled: %v\n", layerPath, cfgErr)
		config = nil
	}

	// Track this blob as uploaded in current session BEFORE cleanup,
	// so cleanupOldVersions won't delete a file we're about to upload.
	m.uploadedInSession[blobName] = true

	if err := m.checkActiveBlobs(ctx, uploader, filename, layerPath, config); err != nil {
		delete(m.uploadedInSession, blobName)
		return err
	}

	if err := m.cleanupOldVersions(ctx, uploader, filename, config); err != nil {
		delete(m.uploadedInSession, blobName)
		return err
	}

	if err := uploader.Upload(ctx, blobName, content); err != nil {
		delete(m.uploadedInSession, blobName)
		return err
	}

	fmt.Fprintf(os.Stdout, "Uploaded: %s\n", blobName)
	return nil
}

// checkActiveBlobs checks whether existing migration blobs with the same stem
// are still active (their metadata conditions still pass). If so, uploading
// would overwrite a still-needed migration, and the upload is refused.
//
// In a shared container, blobs owned by a different layer are skipped so this
// layer's upload is never blocked by another layer's still-active migration.
//
// This guard can be bypassed with the --force flag.
// It is a no-op if no guardChecker is configured (e.g., when tofu binary
// is not available).
func (m *Manager) checkActiveBlobs(ctx context.Context, uploader BlobUploader, filename, layerPath string, config *BackendConfig) error {
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

		// Skip blobs that provably belong to a different layer (shared container).
		if BlobContentOwnedByOtherLayer(blobContent, config) {
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
//
// Blobs uploaded in the current session are skipped - they're part of the
// current migration set (e.g., different layers from same YAML), not old versions.
// In a shared container, blobs owned by a different layer are also skipped so
// this layer's cleanup never deletes another layer's same-stem migration.
func (m *Manager) cleanupOldVersions(ctx context.Context, uploader BlobUploader, filename string, config *BackendConfig) error {
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
		if !strings.HasSuffix(blobName, ".tf") || blobName == newBlobName {
			continue
		}

		// Skip if this blob was uploaded in the current session
		// (it's part of the current migration set, not an old version)
		if m.uploadedInSession[blobName] {
			continue
		}

		// Skip blobs that provably belong to a different layer (shared container).
		if m.blobOwnedByOtherLayer(ctx, uploader, blobName, config) {
			continue
		}

		if err := uploader.DeleteBlob(ctx, blobName); err != nil {
			return fmt.Errorf("removing old version %q: %w", blobName, err)
		}
		fmt.Fprintf(os.Stderr, "Removed old version: %s\n", blobName)
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
		// Discover this layer's coordinates so we only prune blobs that belong
		// to it (shared container). Non-fatal: nil config ⇒ act as today.
		config, cfgErr := m.getBackendConfig(layerPath)
		if cfgErr != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not discover backend config for %q, prune scoping disabled: %v\n", layerPath, cfgErr)
			config = nil
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
				// Skip blobs that provably belong to a different layer.
				if m.blobOwnedByOtherLayer(ctx, uploader, blob, config) {
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

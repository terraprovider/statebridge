// Package upload provides blob storage upload capabilities for generated
// migration files. It discovers backend configuration from Terraform layer
// directories and migration YAML init arguments, then uploads migration
// files to the appropriate storage containers.
//
// Supported backends: azurerm (Azure Blob Storage), s3 (AWS S3),
// gcs (Google Cloud Storage), local (filesystem).
package upload

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/hashicorp/hcl/v2/hclsyntax"
)

// BackendType identifies the storage backend kind.
type BackendType string

const (
	BackendAzurerm BackendType = "azurerm"
	BackendS3      BackendType = "s3"
	BackendGCS     BackendType = "gcs"
	BackendLocal   BackendType = "local"
)

// BackendConfig is the interface all backend configurations implement.
type BackendConfig interface {
	// Type returns the backend type identifier.
	Type() BackendType
	// Validate checks that all required fields are populated.
	Validate() error
	// CacheKey returns a string that uniquely identifies this storage target
	// for uploader caching (e.g., "azurerm|acct|container").
	CacheKey() string
}

// ---------------------------------------------------------------------------
// Azure (azurerm) backend
// ---------------------------------------------------------------------------

// AzurermBackendConfig holds Azure Blob Storage backend configuration.
type AzurermBackendConfig struct {
	StorageAccountName string
	ContainerName      string
	ResourceGroupName  string
}

func (c *AzurermBackendConfig) Type() BackendType { return BackendAzurerm }

func (c *AzurermBackendConfig) Validate() error {
	if c.StorageAccountName == "" {
		return fmt.Errorf("storage_account_name is required but not set")
	}
	if c.ContainerName == "" {
		return fmt.Errorf("container_name is required but not set")
	}
	return nil
}

func (c *AzurermBackendConfig) CacheKey() string {
	return "azurerm|" + c.StorageAccountName + "|" + c.ContainerName
}

// BlobServiceURL returns the Azure Blob Storage service URL for the account.
func (c *AzurermBackendConfig) BlobServiceURL() string {
	return fmt.Sprintf("https://%s.blob.core.windows.net", c.StorageAccountName)
}

// ---------------------------------------------------------------------------
// AWS S3 backend
// ---------------------------------------------------------------------------

// S3BackendConfig holds AWS S3 backend configuration.
type S3BackendConfig struct {
	Bucket   string
	Region   string
	Endpoint string // custom endpoint (e.g., MinIO)
}

func (c *S3BackendConfig) Type() BackendType { return BackendS3 }

func (c *S3BackendConfig) Validate() error {
	if c.Bucket == "" {
		return fmt.Errorf("bucket is required but not set")
	}
	return nil
}

func (c *S3BackendConfig) CacheKey() string {
	return "s3|" + c.Bucket + "|" + c.Region
}

// ---------------------------------------------------------------------------
// Google Cloud Storage (gcs) backend
// ---------------------------------------------------------------------------

// GCSBackendConfig holds Google Cloud Storage backend configuration.
type GCSBackendConfig struct {
	Bucket string
	Prefix string
}

func (c *GCSBackendConfig) Type() BackendType { return BackendGCS }

func (c *GCSBackendConfig) Validate() error {
	if c.Bucket == "" {
		return fmt.Errorf("bucket is required but not set")
	}
	return nil
}

func (c *GCSBackendConfig) CacheKey() string {
	return "gcs|" + c.Bucket
}

// ---------------------------------------------------------------------------
// Local filesystem backend
// ---------------------------------------------------------------------------

// LocalBackendConfig holds local filesystem backend configuration.
// Migration files are stored in a "migrations" subdirectory under Path.
type LocalBackendConfig struct {
	Path string
}

func (c *LocalBackendConfig) Type() BackendType { return BackendLocal }

func (c *LocalBackendConfig) Validate() error {
	if c.Path == "" {
		return fmt.Errorf("path is required but not set")
	}
	return nil
}

func (c *LocalBackendConfig) CacheKey() string {
	return "local|" + c.Path
}

// ---------------------------------------------------------------------------
// Backend discovery
// ---------------------------------------------------------------------------

// DiscoverBackendConfig finds the backend configuration for a Terraform layer
// by parsing .tf files in layerPath and merging with values extracted from
// init args. Init args take precedence over inline HCL values.
func DiscoverBackendConfig(layerPath string, initArgs []string) (BackendConfig, error) {
	inline, backendType, err := ParseHCLBackend(layerPath)
	if err != nil {
		return nil, fmt.Errorf("parsing backend config in %q: %w", layerPath, err)
	}

	if backendType == "" {
		return nil, fmt.Errorf("backend config for layer %q: no supported backend block found in .tf files", layerPath)
	}

	overrides := ParseInitArgs(layerPath, backendType, initArgs)
	config := MergeBackendConfig(inline, backendType, overrides)

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("backend config for layer %q: %w", layerPath, err)
	}

	return config, nil
}

// supportedBackends lists the backend types we can handle.
var supportedBackends = map[string]bool{
	"azurerm": true,
	"s3":      true,
	"gcs":     true,
	"local":   true,
}

// ParseHCLBackend scans all .tf files in layerPath for a
// terraform { backend "<type>" { ... } } block and extracts the
// configuration for the first supported backend found.
// Returns the config, detected backend type, and any error.
// Returns ("", nil) type if no supported backend block is found.
func ParseHCLBackend(layerPath string) (BackendConfig, BackendType, error) {
	tfFiles, err := filepath.Glob(filepath.Join(layerPath, "*.tf"))
	if err != nil {
		return nil, "", fmt.Errorf("globbing .tf files in %s: %w", layerPath, err)
	}

	parser := hclparse.NewParser()

	terraformSchema := &hcl.BodySchema{
		Blocks: []hcl.BlockHeaderSchema{
			{Type: "terraform"},
		},
	}
	backendSchema := &hcl.BodySchema{
		Blocks: []hcl.BlockHeaderSchema{
			{Type: "backend", LabelNames: []string{"type"}},
		},
	}

	for _, tfFile := range tfFiles {
		file, diags := parser.ParseHCLFile(tfFile)
		if diags.HasErrors() {
			continue // skip unparseable files
		}

		content, _, diags := file.Body.PartialContent(terraformSchema)
		if diags.HasErrors() {
			continue
		}

		for _, tfBlock := range content.Blocks {
			innerContent, _, diags := tfBlock.Body.PartialContent(backendSchema)
			if diags.HasErrors() {
				continue
			}

			for _, backendBlock := range innerContent.Blocks {
				if len(backendBlock.Labels) == 0 {
					continue
				}
				backendLabel := backendBlock.Labels[0]
				if !supportedBackends[backendLabel] {
					continue
				}
				cfg, err := extractBackendConfig(BackendType(backendLabel), backendBlock)
				if err != nil {
					return nil, "", err
				}
				return cfg, BackendType(backendLabel), nil
			}
		}
	}

	return nil, "", nil
}

// extractBackendConfig dispatches to the per-backend config extractor.
func extractBackendConfig(bt BackendType, block *hcl.Block) (BackendConfig, error) {
	switch bt {
	case BackendAzurerm:
		return extractAzurermConfig(block)
	case BackendS3:
		return extractS3Config(block)
	case BackendGCS:
		return extractGCSConfig(block)
	case BackendLocal:
		return extractLocalConfig(block)
	default:
		return nil, fmt.Errorf("unsupported backend type %q", bt)
	}
}

// extractAzurermConfig reads recognized attributes from an azurerm backend block.
func extractAzurermConfig(block *hcl.Block) (*AzurermBackendConfig, error) {
	attrs, diags := block.Body.JustAttributes()
	if diags.HasErrors() {
		return nil, fmt.Errorf("parsing azurerm backend attributes: %s", diags.Error())
	}

	config := &AzurermBackendConfig{}
	if attr, ok := attrs["storage_account_name"]; ok {
		val, diags := attr.Expr.Value(nil)
		if !diags.HasErrors() {
			config.StorageAccountName = val.AsString()
		}
	}
	if attr, ok := attrs["container_name"]; ok {
		val, diags := attr.Expr.Value(nil)
		if !diags.HasErrors() {
			config.ContainerName = val.AsString()
		}
	}
	if attr, ok := attrs["resource_group_name"]; ok {
		val, diags := attr.Expr.Value(nil)
		if !diags.HasErrors() {
			config.ResourceGroupName = val.AsString()
		}
	}
	return config, nil
}

// extractS3Config reads recognized attributes from an s3 backend block.
func extractS3Config(block *hcl.Block) (*S3BackendConfig, error) {
	attrs, diags := block.Body.JustAttributes()
	if diags.HasErrors() {
		return nil, fmt.Errorf("parsing s3 backend attributes: %s", diags.Error())
	}

	config := &S3BackendConfig{}
	if attr, ok := attrs["bucket"]; ok {
		val, diags := attr.Expr.Value(nil)
		if !diags.HasErrors() {
			config.Bucket = val.AsString()
		}
	}
	if attr, ok := attrs["region"]; ok {
		val, diags := attr.Expr.Value(nil)
		if !diags.HasErrors() {
			config.Region = val.AsString()
		}
	}
	for _, key := range []string{"endpoint", "endpoints"} {
		if attr, ok := attrs[key]; ok {
			val, diags := attr.Expr.Value(nil)
			if !diags.HasErrors() {
				config.Endpoint = val.AsString()
			}
		}
	}
	return config, nil
}

// extractGCSConfig reads recognized attributes from a gcs backend block.
func extractGCSConfig(block *hcl.Block) (*GCSBackendConfig, error) {
	attrs, diags := block.Body.JustAttributes()
	if diags.HasErrors() {
		return nil, fmt.Errorf("parsing gcs backend attributes: %s", diags.Error())
	}

	config := &GCSBackendConfig{}
	if attr, ok := attrs["bucket"]; ok {
		val, diags := attr.Expr.Value(nil)
		if !diags.HasErrors() {
			config.Bucket = val.AsString()
		}
	}
	if attr, ok := attrs["prefix"]; ok {
		val, diags := attr.Expr.Value(nil)
		if !diags.HasErrors() {
			config.Prefix = val.AsString()
		}
	}
	return config, nil
}

// extractLocalConfig reads recognized attributes from a local backend block.
func extractLocalConfig(block *hcl.Block) (*LocalBackendConfig, error) {
	attrs, diags := block.Body.JustAttributes()
	if diags.HasErrors() {
		return nil, fmt.Errorf("parsing local backend attributes: %s", diags.Error())
	}

	config := &LocalBackendConfig{}
	if attr, ok := attrs["path"]; ok {
		val, diags := attr.Expr.Value(nil)
		if !diags.HasErrors() {
			config.Path = val.AsString()
		}
	}
	return config, nil
}

// ---------------------------------------------------------------------------
// Init args & merge
// ---------------------------------------------------------------------------

// recognizedFields maps backend types to the set of fields that are relevant
// for migration file storage.
var recognizedFields = map[BackendType]map[string]bool{
	BackendAzurerm: {
		"storage_account_name": true,
		"container_name":       true,
		"resource_group_name":  true,
	},
	BackendS3: {
		"bucket":   true,
		"region":   true,
		"endpoint": true,
	},
	BackendGCS: {
		"bucket": true,
		"prefix": true,
	},
	BackendLocal: {
		"path": true,
	},
}

// ParseInitArgs extracts backend-config key=value pairs from init arguments.
// It recognizes the format "-backend-config=key=value" for inline values and
// "-backend-config=path/to/file" for file-based configuration. When the value
// after -backend-config= does not contain "=" it is treated as a file path.
// Relative file paths are resolved against layerPath.
// Only fields relevant to the given backendType are extracted.
func ParseInitArgs(layerPath string, backendType BackendType, args []string) map[string]string {
	recognized := recognizedFields[backendType]
	if recognized == nil {
		return nil
	}
	result := make(map[string]string)

	for _, arg := range args {
		val := strings.TrimPrefix(arg, "-backend-config=")
		if val == arg {
			// No -backend-config= prefix; skip unless it's a raw key=value
			if !strings.Contains(val, "=") {
				continue
			}
		}

		// If val contains "=" it's a key=value pair; otherwise it's a file path
		if strings.Contains(val, "=") {
			parts := strings.SplitN(val, "=", 2)
			if len(parts) == 2 && recognized[parts[0]] {
				result[parts[0]] = parts[1]
			}
		} else {
			// Treat as a file path — resolve relative to the layer directory
			filePath := val
			if !filepath.IsAbs(filePath) {
				filePath = filepath.Join(layerPath, filePath)
			}
			fileKVs, err := parseBackendConfigFile(filePath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: could not read backend-config file %q: %v\n", val, err)
				continue
			}
			for k, v := range fileKVs {
				if recognized[k] {
					result[k] = v
				}
			}
		}
	}

	return result
}

// MergeBackendConfig applies overrides onto a base config, returning a new
// BackendConfig. Non-empty override values replace the corresponding base values.
func MergeBackendConfig(base BackendConfig, bt BackendType, overrides map[string]string) BackendConfig {
	if len(overrides) == 0 && base != nil {
		return base
	}

	switch bt {
	case BackendAzurerm:
		cfg := &AzurermBackendConfig{}
		if az, ok := base.(*AzurermBackendConfig); ok && az != nil {
			*cfg = *az
		}
		if v, ok := overrides["storage_account_name"]; ok && v != "" {
			cfg.StorageAccountName = v
		}
		if v, ok := overrides["container_name"]; ok && v != "" {
			cfg.ContainerName = v
		}
		if v, ok := overrides["resource_group_name"]; ok && v != "" {
			cfg.ResourceGroupName = v
		}
		return cfg

	case BackendS3:
		cfg := &S3BackendConfig{}
		if s3, ok := base.(*S3BackendConfig); ok && s3 != nil {
			*cfg = *s3
		}
		if v, ok := overrides["bucket"]; ok && v != "" {
			cfg.Bucket = v
		}
		if v, ok := overrides["region"]; ok && v != "" {
			cfg.Region = v
		}
		if v, ok := overrides["endpoint"]; ok && v != "" {
			cfg.Endpoint = v
		}
		return cfg

	case BackendGCS:
		cfg := &GCSBackendConfig{}
		if gcs, ok := base.(*GCSBackendConfig); ok && gcs != nil {
			*cfg = *gcs
		}
		if v, ok := overrides["bucket"]; ok && v != "" {
			cfg.Bucket = v
		}
		if v, ok := overrides["prefix"]; ok && v != "" {
			cfg.Prefix = v
		}
		return cfg

	case BackendLocal:
		cfg := &LocalBackendConfig{}
		if l, ok := base.(*LocalBackendConfig); ok && l != nil {
			*cfg = *l
		}
		if v, ok := overrides["path"]; ok && v != "" {
			cfg.Path = v
		}
		return cfg
	}

	// Unknown backend: return base unchanged.
	return base
}

// parseBackendConfigFile reads a Terraform/OpenTofu backend config file and
// returns the key=value pairs it contains. It supports two formats:
//
//  1. HCL format: key = "value" assignments (standard .hcl/.tf style)
//  2. Plain text format: key=value per line (simple key-value pairs)
//
// HCL parsing is attempted first; if it fails, plain text is used as fallback.
func parseBackendConfigFile(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	// Try HCL parsing first
	if kvs, err := parseBackendConfigHCL(path, data); err == nil {
		return kvs, nil
	}

	// Fallback to plain text key=value parsing
	return parseBackendConfigPlainText(data), nil
}

// parseBackendConfigHCL parses backend config as HCL attribute assignments.
// Returns an error if parsing fails or if no string values could be extracted.
func parseBackendConfigHCL(filename string, data []byte) (map[string]string, error) {
	file, diags := hclsyntax.ParseConfig(data, filename, hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		return nil, fmt.Errorf("HCL parse error: %s", diags.Error())
	}

	attrs, diags := file.Body.JustAttributes()
	if diags.HasErrors() {
		return nil, fmt.Errorf("HCL attributes error: %s", diags.Error())
	}

	result := make(map[string]string)
	for name, attr := range attrs {
		val, diags := attr.Expr.Value(nil)
		if diags.HasErrors() {
			// Value couldn't be resolved (e.g., unquoted identifier treated as variable ref).
			// Fall through to plain text parsing for the whole file.
			continue
		}
		if val.Type().FriendlyName() != "string" {
			continue
		}
		result[name] = val.AsString()
	}

	// If we parsed attributes but couldn't extract any string values,
	// let the caller fall through to plain text parsing.
	if len(attrs) > 0 && len(result) == 0 {
		return nil, fmt.Errorf("no string values extracted from HCL")
	}

	return result, nil
}

// parseBackendConfigPlainText parses backend config as plain key=value lines.
// Empty lines and lines starting with # are skipped.
func parseBackendConfigPlainText(data []byte) map[string]string {
	result := make(map[string]string)
	content := string(data)

	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			val := strings.TrimSpace(parts[1])
			// Strip surrounding quotes if present
			val = strings.Trim(val, "\"'")
			if key != "" {
				result[key] = val
			}
		}
	}

	return result
}

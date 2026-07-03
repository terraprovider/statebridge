// Package upload provides Azure Blob Storage upload capabilities for
// generated migration files. It discovers backend configuration from
// Terraform layer directories and migration YAML init arguments, then
// uploads migration files to the appropriate storage containers.
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

// BackendConfig holds Azure Blob Storage backend configuration
// discovered from a Terraform layer's backend block and/or init args.
type BackendConfig struct {
	StorageAccountName string
	ContainerName      string
	ResourceGroupName  string
	// Key is the state blob path within the container. When multiple layers
	// share a single container it is the field that distinguishes their state
	// (and, via embedded metadata, their migration blobs) from one another.
	Key string
}

// Validate checks that the required fields are populated.
func (b *BackendConfig) Validate() error {
	if b.StorageAccountName == "" {
		return fmt.Errorf("storage_account_name is required but not set")
	}
	if b.ContainerName == "" {
		return fmt.Errorf("container_name is required but not set")
	}
	return nil
}

// BlobServiceURL returns the Azure Blob Storage service URL for the account.
func (b *BackendConfig) BlobServiceURL() string {
	return fmt.Sprintf("https://%s.blob.core.windows.net", b.StorageAccountName)
}

// DiscoverBackendConfig finds the azurerm backend configuration for a
// Terraform layer by parsing .tf files in layerPath and merging with
// values extracted from init args. Init args take precedence over inline values.
func DiscoverBackendConfig(layerPath string, initArgs []string) (*BackendConfig, error) {
	inline, err := ParseHCLBackend(layerPath)
	if err != nil {
		return nil, fmt.Errorf("parsing backend config in %q: %w", layerPath, err)
	}

	overrides := ParseInitArgs(layerPath, initArgs)
	config := MergeBackendConfig(inline, overrides)

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("backend config for layer %q: %w", layerPath, err)
	}

	return config, nil
}

// ParseHCLBackend scans all .tf files in layerPath for a
// terraform { backend "azurerm" { ... } } block and extracts
// storage_account_name, container_name, and resource_group_name.
// Returns a zero-value BackendConfig (not an error) if no azurerm
// backend block is found.
func ParseHCLBackend(layerPath string) (*BackendConfig, error) {
	tfFiles, err := filepath.Glob(filepath.Join(layerPath, "*.tf"))
	if err != nil {
		return &BackendConfig{}, fmt.Errorf("globbing .tf files in %s: %w", layerPath, err)
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
				if len(backendBlock.Labels) > 0 && backendBlock.Labels[0] == "azurerm" {
					return extractAzurermConfig(backendBlock)
				}
			}
		}
	}

	return &BackendConfig{}, nil
}

// extractAzurermConfig reads recognized attributes from an azurerm backend block.
func extractAzurermConfig(block *hcl.Block) (*BackendConfig, error) {
	attrs, diags := block.Body.JustAttributes()
	if diags.HasErrors() {
		return nil, fmt.Errorf("parsing azurerm backend attributes: %s", diags.Error())
	}

	config := &BackendConfig{}

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
	if attr, ok := attrs["key"]; ok {
		val, diags := attr.Expr.Value(nil)
		if !diags.HasErrors() {
			config.Key = val.AsString()
		}
	}

	return config, nil
}

// ParseInitArgs extracts backend-config key=value pairs from init arguments.
// It recognizes the format "-backend-config=key=value" for inline values and
// "-backend-config=path/to/file" for file-based configuration. When the value
// after -backend-config= does not contain "=" it is treated as a file path.
// Relative file paths are resolved against layerPath so that backend config
// files placed alongside .tf files in the layer directory are found correctly.
// Backend config files follow the Terraform/OpenTofu format: HCL assignments
// (key = "value") or plain key=value lines.
// Only known azurerm backend fields are extracted (storage_account_name,
// container_name, resource_group_name). Non-backend-config args are silently ignored.
func ParseInitArgs(layerPath string, args []string) map[string]string {
	recognized := map[string]bool{
		"storage_account_name": true,
		"container_name":       true,
		"resource_group_name":  true,
		"key":                  true,
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

// MergeBackendConfig applies overrides onto base. Non-empty override
// values replace the corresponding base values.
func MergeBackendConfig(base *BackendConfig, overrides map[string]string) *BackendConfig {
	result := *base
	if v, ok := overrides["storage_account_name"]; ok && v != "" {
		result.StorageAccountName = v
	}
	if v, ok := overrides["container_name"]; ok && v != "" {
		result.ContainerName = v
	}
	if v, ok := overrides["resource_group_name"]; ok && v != "" {
		result.ResourceGroupName = v
	}
	if v, ok := overrides["key"]; ok && v != "" {
		result.Key = v
	}
	return &result
}

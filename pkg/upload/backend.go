// Package upload provides Azure Blob Storage upload capabilities for
// generated migration files. It discovers backend configuration from
// Terraform layer directories and migration YAML init arguments, then
// uploads migration files to the appropriate storage containers.
package upload

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclparse"
)

// BackendConfig holds Azure Blob Storage backend configuration
// discovered from a Terraform layer's backend block and/or init args.
type BackendConfig struct {
	StorageAccountName string
	ContainerName      string
	ResourceGroupName  string
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

	overrides := ParseInitArgs(initArgs)
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

	return config, nil
}

// ParseInitArgs extracts backend-config key=value pairs from init arguments.
// It recognizes the format "-backend-config=key=value" and extracts only
// known azurerm backend fields (storage_account_name, container_name,
// resource_group_name). Non-backend-config args are silently ignored.
func ParseInitArgs(args []string) map[string]string {
	recognized := map[string]bool{
		"storage_account_name": true,
		"container_name":       true,
		"resource_group_name":  true,
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
		parts := strings.SplitN(val, "=", 2)
		if len(parts) == 2 && recognized[parts[0]] {
			result[parts[0]] = parts[1]
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
	return &result
}

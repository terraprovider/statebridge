// Package state provides functionality for reading and querying Terraform/OpenTofu
// state via the terraform-exec library.
package state

import (
	"fmt"
	"strings"

	tfjson "github.com/hashicorp/terraform-json"
)

// ResourceInfo is a flattened, convenience view of a single resource instance
// extracted from Terraform state.
type ResourceInfo struct {
	// Address is the full resource address (e.g., "aws_instance.web",
	// "aws_s3_bucket.data[\"my-key\"]", "module.vpc.aws_subnet.public[0]").
	Address string

	// Type is the resource type (e.g., "aws_s3_bucket").
	Type string

	// Name is the resource name (e.g., "data").
	Name string

	// Mode is the resource mode ("managed" or "data").
	Mode string

	// Index is the for_each key (string) or count index (float64), or nil if neither.
	Index interface{}

	// Key is a string representation of Index, suitable for use in templates.
	Key string

	// Provider is the provider name from state.
	Provider string

	// Attributes contains all attribute values from the state.
	Attributes map[string]interface{}
}

// FlattenState recursively walks the state module tree and returns all
// resource instances as a flat slice of ResourceInfo.
// Returns nil if the state or root module is nil.
func FlattenState(s *tfjson.State) []*ResourceInfo {
	if s == nil || s.Values == nil || s.Values.RootModule == nil {
		return nil
	}
	return flattenModule(s.Values.RootModule)
}

// flattenModule recursively extracts resources from a module and its children.
func flattenModule(mod *tfjson.StateModule) []*ResourceInfo {
	var result []*ResourceInfo

	for _, r := range mod.Resources {
		result = append(result, stateResourceToInfo(r))
	}

	for _, child := range mod.ChildModules {
		result = append(result, flattenModule(child)...)
	}

	return result
}

// stateResourceToInfo converts a terraform-json StateResource to our ResourceInfo.
func stateResourceToInfo(r *tfjson.StateResource) *ResourceInfo {
	return &ResourceInfo{
		Address:    r.Address,
		Type:       r.Type,
		Name:       r.Name,
		Mode:       string(r.Mode),
		Index:      r.Index,
		Key:        formatIndex(r.Index),
		Provider:   r.ProviderName,
		Attributes: r.AttributeValues,
	}
}

// formatIndex converts an index value to its string representation.
// For string keys, returns the key directly.
// For numeric indices, formats as an integer.
// For nil, returns an empty string.
func formatIndex(index interface{}) string {
	if index == nil {
		return ""
	}
	switch v := index.(type) {
	case string:
		return v
	case float64:
		return fmt.Sprintf("%d", int(v))
	default:
		return fmt.Sprintf("%v", v)
	}
}

// LookupResource finds a single resource by exact address in the flattened state.
// Returns an error if the resource is not found.
func LookupResource(s *tfjson.State, address string) (*ResourceInfo, error) {
	resources := FlattenState(s)
	for _, r := range resources {
		if r.Address == address {
			return r, nil
		}
	}
	return nil, fmt.Errorf("resource %q not found in state", address)
}

// LookupResourcesByPrefix finds all resource instances that match a base address.
// The baseAddress should not include any index notation — e.g., "aws_s3_bucket.data"
// will match "aws_s3_bucket.data[\"key1\"]", "aws_s3_bucket.data[\"key2\"]", etc.
// It also matches the exact address "aws_s3_bucket.data" (no index).
func LookupResourcesByPrefix(s *tfjson.State, baseAddress string) ([]*ResourceInfo, error) {
	resources := FlattenState(s)
	var matches []*ResourceInfo

	for _, r := range resources {
		if r.Address == baseAddress || strings.HasPrefix(r.Address, baseAddress+"[") {
			matches = append(matches, r)
		}
	}

	if len(matches) == 0 {
		return nil, fmt.Errorf("no resources matching %q found in state", baseAddress)
	}

	return matches, nil
}

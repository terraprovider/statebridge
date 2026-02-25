// Package state provides functionality for reading and querying Terraform/OpenTofu
// state via the terraform-exec library.
package state

import (
	"fmt"
	"sort"
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

// StateIndex provides an indexed view of flattened state for fast lookups.
// It is safe to use with nil or empty state (lookups will return not found).
type StateIndex struct {
	resources []*ResourceInfo
	byAddress map[string]*ResourceInfo
	byBase    map[string][]*ResourceInfo
}

// NewStateIndex builds a StateIndex from the given state.
func NewStateIndex(s *tfjson.State) *StateIndex {
	idx := &StateIndex{
		byAddress: make(map[string]*ResourceInfo),
		byBase:    make(map[string][]*ResourceInfo),
	}

	resources := FlattenState(s)
	idx.resources = resources
	for _, r := range resources {
		idx.byAddress[r.Address] = r
		base := baseAddress(r.Address)
		idx.byBase[base] = append(idx.byBase[base], r)
	}

	return idx
}

// LookupResource finds a single resource by exact address in the index.
// Returns an error if the resource is not found.
func (i *StateIndex) LookupResource(address string) (*ResourceInfo, error) {
	if i == nil {
		return nil, fmt.Errorf("resource %q not found in state", address)
	}
	if r, ok := i.byAddress[address]; ok {
		return r, nil
	}
	return nil, fmt.Errorf("resource %q not found in state", address)
}

// LookupResourcesByPrefix finds all resource instances that match a base address.
// The baseAddress should not include any index notation.
func (i *StateIndex) LookupResourcesByPrefix(baseAddress string) ([]*ResourceInfo, error) {
	if i == nil {
		return nil, fmt.Errorf("no resources matching %q found in state", baseAddress)
	}
	if matches := i.byBase[baseAddress]; len(matches) > 0 {
		return matches, nil
	}
	return nil, fmt.Errorf("no resources matching %q found in state", baseAddress)
}

// ResourceExists checks whether any resource matching the given address exists
// in the indexed state. Base addresses match any for_each instance.
// Module paths (e.g., "module.foo") are checked by prefix — returns true if
// any resource exists under that module.
func (i *StateIndex) ResourceExists(address string) bool {
	if i == nil {
		return false
	}

	if strings.Contains(address, "[") {
		_, ok := i.byAddress[address]
		return ok
	}

	base := baseAddress(address)
	if len(i.byBase[base]) > 0 {
		return true
	}

	// Fall back to module prefix check: "module.foo" matches any resource
	// whose address starts with "module.foo."
	return i.HasResourcesWithPrefix(address)
}

// HasResourcesWithPrefix reports whether any resources exist in the index
// with addresses starting with the given prefix followed by a dot.
// Used for module-aware existence checks.
func (i *StateIndex) HasResourcesWithPrefix(prefix string) bool {
	if i == nil {
		return false
	}
	pfx := prefix + "."
	for baseAddr := range i.byBase {
		if strings.HasPrefix(baseAddr, pfx) {
			return true
		}
	}
	return false
}

// ManagedBaseAddresses returns all unique base addresses of managed resources
// in the index (excluding data sources), sorted alphabetically.
func (i *StateIndex) ManagedBaseAddresses() []string {
	if i == nil {
		return nil
	}
	var result []string
	for baseAddr, resources := range i.byBase {
		if len(resources) > 0 && resources[0].Mode == "managed" {
			result = append(result, baseAddr)
		}
	}
	sort.Strings(result)
	return result
}

// ManagedResourcesUnderModule returns all managed resource instances whose
// address starts with modulePrefix followed by a dot. Returns resources at
// any nesting depth, including individual for_each instances.
// Data sources are excluded.
func (i *StateIndex) ManagedResourcesUnderModule(modulePrefix string) []*ResourceInfo {
	if i == nil {
		return nil
	}
	pfx := modulePrefix + "."
	var result []*ResourceInfo
	for _, r := range i.resources {
		if r.Mode == "managed" && strings.HasPrefix(r.Address, pfx) {
			result = append(result, r)
		}
	}
	return result
}

// AllManagedResources returns all managed resource instances in the index,
// excluding data sources. Includes individual for_each instances.
func (i *StateIndex) AllManagedResources() []*ResourceInfo {
	if i == nil {
		return nil
	}
	var result []*ResourceInfo
	for _, r := range i.resources {
		if r.Mode == "managed" {
			result = append(result, r)
		}
	}
	return result
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

func baseAddress(address string) string {
	if idx := strings.Index(address, "["); idx >= 0 {
		return address[:idx]
	}
	return address
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
	idx := NewStateIndex(s)
	return idx.LookupResource(address)
}

// LookupResourcesByPrefix finds all resource instances that match a base address.
// The baseAddress should not include any index notation — e.g., "aws_s3_bucket.data"
// will match "aws_s3_bucket.data[\"key1\"]", "aws_s3_bucket.data[\"key2\"]", etc.
// It also matches the exact address "aws_s3_bucket.data" (no index).
func LookupResourcesByPrefix(s *tfjson.State, baseAddress string) ([]*ResourceInfo, error) {
	idx := NewStateIndex(s)
	return idx.LookupResourcesByPrefix(baseAddress)
}

// ResourceExists checks whether any resource matching the given address exists
// in state. For a base address (e.g., "aws_instance.web"), it matches any
// for_each instance. For an address with an index (e.g., "aws_instance.web[\"key\"]"),
// it matches only that specific instance. Returns false for nil or empty state.
func ResourceExists(s *tfjson.State, address string) bool {
	idx := NewStateIndex(s)
	return idx.ResourceExists(address)
}

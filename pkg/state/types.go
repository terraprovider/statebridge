// Package state provides functionality for reading and querying Terraform/OpenTofu
// state via the terraform-exec library.
package state

import (
	"encoding/json"
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
	byConfig  map[string][]*ResourceInfo
}

// NewStateIndex builds a StateIndex from the given state.
func NewStateIndex(s *tfjson.State) *StateIndex {
	idx := &StateIndex{
		byAddress: make(map[string]*ResourceInfo),
		byBase:    make(map[string][]*ResourceInfo),
		byConfig:  make(map[string][]*ResourceInfo),
	}

	resources := FlattenState(s)
	idx.resources = resources
	for _, r := range resources {
		idx.byAddress[r.Address] = r
		base := BaseAddress(r.Address)
		idx.byBase[base] = append(idx.byBase[base], r)
		cfg := ConfigAddress(r.Address)
		idx.byConfig[cfg] = append(idx.byConfig[cfg], r)
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

	// An address carrying a resource-level for_each/count key (a trailing
	// "[...]" after the resource type.name) refers to one specific instance
	// and is matched exactly. Module-instance indices such as module.foo[0]
	// are deliberately not treated as resource keys here.
	if hasResourceKey(address) {
		_, ok := i.byAddress[address]
		return ok
	}

	// A base resource address matches any of its for_each/count instances.
	if len(i.byBase[address]) > 0 {
		return true
	}

	// A bracket-free configuration address matches any resource instance sharing
	// that config address. This lets a module-config address such as
	// "module.cp.random_id.items" match indexed state instances like
	// "module.cp[0].random_id.items[\"a\"]" — the form emitted for removed
	// blocks when a resource lives inside an indexed module instance. We only
	// apply this when the query itself carries no instance keys, so an explicit
	// instance reference like "module.cp[1].random_id.items" is NOT satisfied by
	// a different instance ("module.cp[0]...").
	if !strings.Contains(address, "[") {
		if len(i.byConfig[address]) > 0 {
			return true
		}
	}

	// A module path (possibly indexed, e.g. "module.foo" or "module.foo[0]")
	// matches when any resource exists beneath it.
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

// ModuleInstanceBases returns the distinct base addresses (module-instance
// indices preserved, resource for_each/count keys stripped) of all resource
// instances that share the given configuration address — an address with ALL
// instance keys removed, as produced by ConfigAddress.
//
// When a resource lives inside a multi-instance (count/for_each) module, each
// module instance yields a distinct base address here — for example
// "module.cp[0].random_id.items" and "module.cp[1].random_id.items" — even
// though both collapse to the single configuration address
// "module.cp.random_id.items". This is used to detect whether a resource
// referenced via one module instance actually spans several module instances
// in state.
//
// The result is deduplicated and sorted. Returns nil for a nil index or when no
// instances share the configuration address.
func (i *StateIndex) ModuleInstanceBases(configAddr string) []string {
	if i == nil {
		return nil
	}
	seen := make(map[string]bool)
	var result []string
	for _, r := range i.byConfig[configAddr] {
		b := BaseAddress(r.Address)
		if !seen[b] {
			seen[b] = true
			result = append(result, b)
		}
	}
	sort.Strings(result)
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

// BaseAddress strips a trailing resource-level for_each/count key from a
// Terraform resource address, leaving any module-instance indices intact.
// If the address has no resource key, it is returned unchanged.
//
//	aws_s3_bucket.data["k"]      → aws_s3_bucket.data
//	module.m[0].type.name["k"]   → module.m[0].type.name
//	module.m[0].type.name        → module.m[0].type.name
//	aws_instance.web             → aws_instance.web
//
// The resource key, when present, is always the final bracketed segment of the
// address because the resource type.name is the last component. Module-instance
// indices such as module.m[0] are therefore preserved: they are never at the
// very end of a full resource address.
func BaseAddress(address string) string {
	if !strings.HasSuffix(address, "]") {
		return address
	}
	if idx := strings.LastIndex(address, "["); idx >= 0 {
		return address[:idx]
	}
	return address
}

// ConfigAddress removes every instance key from an address — both
// module-instance indices (e.g. `module.foo[0]`) and resource for_each/count
// keys (e.g. `res.name["k"]`) — yielding the pure module-and-resource
// configuration address.
//
//	module.cp[0].random_id.items["a"] → module.cp.random_id.items
//	module.cp[0]                       → module.cp
//	aws_instance.web[2]                → aws_instance.web
//	aws_instance.web                   → aws_instance.web
//
// This is the address form required by OpenTofu `removed` blocks, which forbid
// instance keys of any kind. It is also used to match a config address against
// the indexed instances that share it.
func ConfigAddress(address string) string {
	if !strings.Contains(address, "[") {
		return address
	}
	var b strings.Builder
	b.Grow(len(address))
	depth := 0
	for i := 0; i < len(address); i++ {
		switch address[i] {
		case '[':
			depth++
		case ']':
			if depth > 0 {
				depth--
			}
		default:
			if depth == 0 {
				b.WriteByte(address[i])
			}
		}
	}
	return b.String()
}

// hasResourceKey reports whether the address ends with a resource-level
// for_each/count key (e.g. `type.name["k"]` or `module.m[0].type.name[2]`),
// as opposed to a bare module-instance index (e.g. `module.m[0]`) or no index.
func hasResourceKey(address string) bool {
	if !strings.HasSuffix(address, "]") {
		return false
	}
	idx := strings.LastIndex(address, "[")
	if idx < 0 {
		return false
	}
	// If everything up to the final bracket is a pure module path, the bracket
	// is a module-instance index rather than a resource key.
	return !isModulePath(address[:idx])
}

// isModulePath reports whether address is a pure module path — a sequence of
// `module.<name>` steps (each optionally carrying an index) with no trailing
// resource type/name. For example: "module.foo", "module.foo[0]",
// "module.foo.module.bar". Returns false for "module.foo.aws_x.y" or "aws_x.y".
func isModulePath(address string) bool {
	if !strings.HasPrefix(address, "module.") {
		return false
	}
	parts := strings.Split(address, ".")
	if len(parts)%2 != 0 {
		return false
	}
	for i := 0; i < len(parts); i += 2 {
		if parts[i] != "module" {
			return false
		}
	}
	return true
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

// FormatInstanceKey renders a resource instance index as a bracketed address
// suffix, using the syntax Terraform/OpenTofu expects for each key kind:
//
//	for_each key (string) → ["key"]   (quoted)
//	count index (numeric) → [0]        (bare integer)
//	no index (nil)        → ""         (empty)
//
// This distinction matters when constructing destination addresses for moved
// and import blocks: a count index must stay a bare integer. Quoting it (e.g.
// ["0"]) makes Terraform treat it as a for_each key, producing an address that
// does not match the resource in state.
//
// Note: state read via terraform-exec's Show decodes JSON numbers as
// json.Number (it enables UseJSONNumber), so count indices arrive as
// json.Number rather than float64. Both are handled.
func FormatInstanceKey(index interface{}) string {
	switch v := index.(type) {
	case nil:
		return ""
	case string:
		return fmt.Sprintf("[%q]", v)
	case json.Number:
		return fmt.Sprintf("[%s]", v.String())
	case float64:
		return fmt.Sprintf("[%d]", int64(v))
	case int:
		return fmt.Sprintf("[%d]", v)
	default:
		return fmt.Sprintf("[%q]", formatIndex(index))
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
	case json.Number:
		return v.String()
	case float64:
		return fmt.Sprintf("%d", int(v))
	default:
		return fmt.Sprintf("%v", v)
	}
}

// LookupResource finds a single resource by exact address in the flattened state.
// Returns an error if the resource is not found.
//
// Deprecated: This rebuilds the StateIndex on every call. For multiple lookups
// against the same state, use NewStateIndex and call methods on it directly.
func LookupResource(s *tfjson.State, address string) (*ResourceInfo, error) {
	idx := NewStateIndex(s)
	return idx.LookupResource(address)
}

// LookupResourcesByPrefix finds all resource instances that match a base address.
// The baseAddress should not include any index notation — e.g., "aws_s3_bucket.data"
// will match "aws_s3_bucket.data[\"key1\"]", "aws_s3_bucket.data[\"key2\"]", etc.
// It also matches the exact address "aws_s3_bucket.data" (no index).
//
// Deprecated: This rebuilds the StateIndex on every call. For multiple lookups
// against the same state, use NewStateIndex and call methods on it directly.
func LookupResourcesByPrefix(s *tfjson.State, baseAddress string) ([]*ResourceInfo, error) {
	idx := NewStateIndex(s)
	return idx.LookupResourcesByPrefix(baseAddress)
}

// ResourceExists checks whether any resource matching the given address exists
// in state. For a base address (e.g., "aws_instance.web"), it matches any
// for_each instance. For an address with an index (e.g., "aws_instance.web[\"key\"]"),
// it matches only that specific instance. Returns false for nil or empty state.
//
// Deprecated: This rebuilds the StateIndex on every call. For multiple lookups
// against the same state, use NewStateIndex and call methods on it directly.
func ResourceExists(s *tfjson.State, address string) bool {
	idx := NewStateIndex(s)
	return idx.ResourceExists(address)
}

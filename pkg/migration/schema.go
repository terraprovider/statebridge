// Package migration defines the YAML schema types for tfmigrate migration files
// and provides parsing and validation functionality.
package migration

import (
	"path/filepath"
	"strings"
)

// OperationType enumerates the supported migration operation types.
type OperationType string

const (
	// OpMove moves a resource from one layer to another.
	// Generates a removed block in the source layer and an import block in the destination.
	OpMove OperationType = "move"

	// OpRename renames a resource or module within a single layer.
	// Generates a moved block.
	OpRename OperationType = "rename"

	// OpRemove removes a resource from state without destroying it.
	// Generates a removed block with destroy = false (by default).
	OpRemove OperationType = "remove"

	// OpImport imports an existing cloud resource into Terraform state.
	// Generates an import block.
	OpImport OperationType = "import"
)

const (
	// StatusActive is the default migration file status (empty string).
	// Active files are processed normally.
	StatusActive = ""

	// StatusRetired marks a migration file as completed/retired.
	// Retired files are skipped entirely during processing — no validation,
	// no state reads, no condition evaluation.
	StatusRetired = "retired"
)

// MigrationFile represents a parsed YAML migration file.
type MigrationFile struct {
	// Description is a human-readable summary of what this migration does.
	Description string `yaml:"description"`

	// SchemaVersion identifies the schema version (currently "2").
	// Used for forward compatibility: future schema changes will increment
	// this value, allowing the tool to handle multiple versions.
	SchemaVersion string `yaml:"schema_version,omitempty"`

	// Status controls the lifecycle state of this migration file.
	// Valid values: "" (active, default), "retired" (skip entirely).
	// Retired files are skipped without any validation, state reads, or
	// condition evaluation — the cheapest way to disable an old migration.
	Status string `yaml:"status,omitempty"`

	// Condition defines optional preconditions that must all be met for this
	// migration file to be processed. If any condition is not met, the entire
	// file is silently skipped with an informational log message.
	Condition *Condition `yaml:"condition,omitempty"`

	// Operations is the ordered list of migration operations to perform.
	Operations []Operation `yaml:"operations"`

	// FilePath is the filesystem path this file was loaded from.
	// Set by the parser after loading; not present in YAML.
	FilePath string `yaml:"-"`
}

// Condition defines preconditions that must all be met for a migration file
// to be processed. All checks are ANDed — every check must pass.
type Condition struct {
	// ResourcesExist requires that ALL listed addresses exist in their
	// respective layer's state. If any address is missing, the condition fails.
	ResourcesExist []ResourceCheck `yaml:"resources_exist,omitempty"`

	// ResourcesNotExist requires that NONE of the listed addresses exist in
	// their respective layer's state. If any address is found, the condition fails.
	ResourcesNotExist []ResourceCheck `yaml:"resources_not_exist,omitempty"`

	// LayerExists requires that ALL listed layer paths exist on disk.
	// If any path is missing, the condition fails. This is cheaper than
	// resources_exist because it does not read state — just checks directory existence.
	LayerExists []string `yaml:"layer_exists,omitempty"`

	// LayerNotExists requires that NONE of the listed layer paths exist on disk.
	// If any path exists, the condition fails. Useful for skipping migrations
	// after a source layer has been intentionally deleted.
	LayerNotExists []string `yaml:"layer_not_exists,omitempty"`
}

// ResourceCheck specifies a layer path and a set of resource addresses to
// check against that layer's state.
type ResourceCheck struct {
	// Layer is the filesystem path to the Terraform root module whose state
	// will be queried.
	Layer string `yaml:"layer"`

	// Addresses lists the resource addresses to check. A base address
	// (e.g., "aws_instance.web") matches if any for_each instance exists.
	// A fully-qualified address (e.g., "aws_instance.web[\"key\"]") matches
	// only that specific instance.
	Addresses []string `yaml:"addresses"`
}

// Operation represents a single migration operation.
// The Type field determines which other fields are relevant:
//   - move:   SourceLayer, DestinationLayer, Resources (or AllResources + Overrides)
//   - rename: Layer, Renames
//   - remove: Layer, Entries, Destroy (optional)
//   - import: Layer, Imports, Provider (optional)
type Operation struct {
	// Type determines the kind of migration operation.
	Type OperationType `yaml:"type"`

	// Description is an optional human-readable description of this operation.
	Description string `yaml:"description,omitempty"`

	// AddressPrefix is an optional prefix prepended (with a dot separator) to
	// all resource addresses in this operation. Useful for factoring out common
	// module paths (e.g., "module.identity_governance").
	// Cannot be combined with all_resources.
	AddressPrefix string `yaml:"address_prefix,omitempty"`

	// SourceLayer is the filesystem path to the source Terraform root module (move operations).
	SourceLayer string `yaml:"source_layer,omitempty"`

	// DestinationLayer is the filesystem path to the destination Terraform root module (move operations).
	DestinationLayer string `yaml:"destination_layer,omitempty"`

	// Resources lists the resources to move between layers (move operations).
	// When all_resources is false, this is the primary list of resources to move.
	// Not used when all_resources is true — use Overrides instead.
	Resources []ResourceMove `yaml:"resources,omitempty"`

	// AllResources, when true, discovers all managed resources in the source
	// layer's state and moves them. Use Overrides to customize destination
	// addresses or import IDs for specific resources.
	// Only valid for move operations.
	AllResources bool `yaml:"all_resources,omitempty"`

	// Overrides provides per-resource customization when all_resources is true.
	// Each entry identifies a resource by From and specifies a To (rename) and/or
	// ImportID (custom import ID). Only valid with all_resources.
	Overrides []ResourceMove `yaml:"overrides,omitempty"`

	// Omit lists resources that should get removed blocks in the source layer
	// but NOT import blocks in the destination layer. Only valid with all_resources.
	// Useful for resources that cannot be imported and need to be recreated.
	Omit []OmitEntry `yaml:"omit,omitempty"`

	// Layer is the filesystem path to the Terraform root module (rename, remove, import operations).
	Layer string `yaml:"layer,omitempty"`

	// Renames lists the address renames to perform (rename operations).
	Renames []RenameEntry `yaml:"renames,omitempty"`

	// Entries lists the resources to remove from state (remove operations).
	// Each entry specifies an address and an optional per-entry destroy flag.
	Entries []RemoveEntry `yaml:"entries,omitempty"`

	// Destroy controls the default destroy behavior for remove operations.
	// Defaults to false (safe removal from state only). Per-entry Destroy
	// on RemoveEntry overrides this when set.
	Destroy *bool `yaml:"destroy,omitempty"`

	// Imports lists the resources to import into state (import operations).
	Imports []ImportEntry `yaml:"imports,omitempty"`

	// Provider is an optional default provider alias for import operations.
	// Applied to all import entries that don't specify their own provider.
	Provider string `yaml:"provider,omitempty"`
}

// ResourceMove describes a resource to move between layers, optionally with
// per-key routing for for_each resources.
//
// In a normal move (all_resources is false), From is the source resource address.
// In an override (all_resources is true), From identifies which resource to customize.
type ResourceMove struct {
	// From is the resource address in the source layer.
	// When AddressPrefix is set on the parent operation, it is prepended with a dot.
	From string `yaml:"from"`

	// To overrides the destination address when it differs from the source.
	// Defaults to From if omitted (resource keeps its address).
	// Also receives the AddressPrefix if set.
	To string `yaml:"to,omitempty"`

	// Keys maps source for_each keys (or patterns) to destination keys (or templates).
	// Key patterns:
	//   - "exact_key"  → matches exactly that for_each key
	//   - "prefix_*"   → matches all keys starting with "prefix_"
	//   - "*"           → catch-all for remaining unmatched keys
	// Values can be literal strings or Go template expressions.
	// When present, ALL state keys must be covered (completeness enforced).
	// Not valid with all_resources overrides.
	Keys map[string]string `yaml:"keys,omitempty"`

	// ImportID is an optional provider-specific identifier for importing resources.
	// Can be a literal string or a Go template expression.
	// If omitted, auto-resolved from the source state's "id" attribute.
	ImportID string `yaml:"import_id,omitempty"`
}

// RenameEntry describes a single address rename within a layer.
type RenameEntry struct {
	// From is the original resource address.
	From string `yaml:"from"`

	// To is the new resource address.
	To string `yaml:"to"`
}

// ImportEntry describes a single resource to import into state.
type ImportEntry struct {
	// Address is the Terraform resource address to import to.
	Address string `yaml:"address"`

	// ID is the provider-specific identifier for the existing resource.
	ID string `yaml:"id"`

	// Provider is an optional provider alias override.
	// If empty, uses the operation-level Provider (if set).
	Provider string `yaml:"provider,omitempty"`
}

// RemoveEntry describes a single resource to remove from state.
type RemoveEntry struct {
	// Address is the Terraform resource address to remove.
	Address string `yaml:"address"`

	// Destroy controls whether the resource is destroyed (true) or just
	// removed from state tracking (false). If nil, falls back to the
	// operation-level Destroy value.
	Destroy *bool `yaml:"destroy,omitempty"`
}

// OmitEntry specifies a resource to exclude from import generation during
// an all_resources move. A removed block is still generated in the source
// layer, but no import block is produced in the destination layer.
type OmitEntry struct {
	// Address is the base resource address to omit from import.
	Address string `yaml:"address"`

	// Destroy controls whether the removed block uses destroy = true or false.
	// Defaults to false (resource keeps existing in the cloud, just removed from state).
	Destroy *bool `yaml:"destroy,omitempty"`
}

// DestroyValue returns the effective value of the Destroy field,
// defaulting to false if not explicitly set.
func (o *Operation) DestroyValue() bool {
	if o.Destroy == nil {
		return false
	}
	return *o.Destroy
}

// DestroyValue returns the effective value of the Destroy field,
// defaulting to false if not explicitly set.
func (e *RemoveEntry) DestroyValue() bool {
	if e.Destroy == nil {
		return false
	}
	return *e.Destroy
}

// DestroyValue returns the effective value of the Destroy field,
// defaulting to false if not explicitly set.
func (e *OmitEntry) DestroyValue() bool {
	if e.Destroy == nil {
		return false
	}
	return *e.Destroy
}

// FullAddress prepends the address prefix (with a dot separator) if non-empty.
func FullAddress(prefix, addr string) string {
	if prefix == "" {
		return addr
	}
	return prefix + "." + addr
}

// IsModuleAddress returns true if the address is a pure module path with no
// resource type/name segments. For example:
//   - "module.foo" → true
//   - "module.foo.module.bar" → true
//   - "module.foo.aws_instance.web" → false (contains resource type)
//   - "aws_instance.web" → false (not a module path)
func IsModuleAddress(address string) bool {
	if !strings.HasPrefix(address, "module.") {
		return false
	}
	parts := strings.Split(address, ".")
	if len(parts)%2 != 0 {
		return false // must be even (module.name pairs)
	}
	for i := 0; i < len(parts); i += 2 {
		if parts[i] != "module" {
			return false
		}
	}
	return true
}

// YamlStem extracts the stem (base name without extension) from a migration file path.
// For example, "migrations/001_move.yaml" → "001_move".
func YamlStem(filePath string) string {
	base := filepath.Base(filePath)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// Package migration defines the YAML schema types for tfmigrate migration files
// and provides parsing and validation functionality.
package migration

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

// MigrationFile represents a parsed YAML migration file.
type MigrationFile struct {
	// Description is a human-readable summary of what this migration does.
	Description string `yaml:"description"`

	// SchemaVersion identifies the schema version for forward compatibility.
	SchemaVersion string `yaml:"schema_version,omitempty"`

	// Operations is the ordered list of migration operations to perform.
	Operations []Operation `yaml:"operations"`

	// FilePath is the filesystem path this file was loaded from.
	// Set by the parser after loading; not present in YAML.
	FilePath string `yaml:"-"`
}

// Operation represents a single migration operation.
// The Type field determines which other fields are relevant:
//   - move:   SourceLayer, DestinationLayer, Resources
//   - rename: Layer, Renames
//   - remove: Layer, Addresses, Destroy (optional)
//   - import: Layer, Imports
type Operation struct {
	// Type determines the kind of migration operation.
	Type OperationType `yaml:"type"`

	// Description is an optional human-readable description of this operation.
	Description string `yaml:"description,omitempty"`

	// AddressPrefix is an optional prefix prepended (with a dot separator) to
	// all resource addresses in this operation. Useful for factoring out common
	// module paths (e.g., "module.identity_governance").
	AddressPrefix string `yaml:"address_prefix,omitempty"`

	// SourceLayer is the filesystem path to the source Terraform root module (move operations).
	SourceLayer string `yaml:"source_layer,omitempty"`

	// DestinationLayer is the filesystem path to the destination Terraform root module (move operations).
	DestinationLayer string `yaml:"destination_layer,omitempty"`

	// Resources lists the resources to move between layers (move operations).
	Resources []ResourceMove `yaml:"resources,omitempty"`

	// Layer is the filesystem path to the Terraform root module (rename, remove, import operations).
	Layer string `yaml:"layer,omitempty"`

	// Renames lists the address renames to perform (rename operations).
	Renames []RenameEntry `yaml:"renames,omitempty"`

	// Addresses lists the resource addresses to remove from state (remove operations).
	Addresses []string `yaml:"addresses,omitempty"`

	// Destroy controls whether resources are destroyed when removed.
	// Defaults to false (safe removal from state only).
	Destroy *bool `yaml:"destroy,omitempty"`

	// Imports lists the resources to import into state (import operations).
	Imports []ImportEntry `yaml:"imports,omitempty"`
}

// ResourceMove describes a resource to move between layers, optionally with
// per-key routing for for_each resources.
type ResourceMove struct {
	// Address is the base resource address in the source layer.
	// When AddressPrefix is set on the parent operation, it is prepended with a dot.
	Address string `yaml:"address"`

	// DestinationAddress overrides the destination base address when it differs
	// from the source Address. Defaults to Address if omitted.
	// Also receives the AddressPrefix if set.
	DestinationAddress string `yaml:"destination_address,omitempty"`

	// Keys maps source for_each keys (or patterns) to destination keys (or templates).
	// Key patterns:
	//   - "exact_key"  → matches exactly that for_each key
	//   - "prefix_*"   → matches all keys starting with "prefix_"
	//   - "*"           → catch-all for remaining unmatched keys
	// Values can be literal strings or Go template expressions.
	// When present, ALL state keys must be covered (completeness enforced).
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

	// ImportID is the provider-specific identifier for the existing resource.
	ImportID string `yaml:"import_id"`

	// Provider is an optional provider alias override.
	Provider string `yaml:"provider,omitempty"`
}

// DestroyValue returns the effective value of the Destroy field,
// defaulting to false if not explicitly set.
func (o *Operation) DestroyValue() bool {
	if o.Destroy == nil {
		return false
	}
	return *o.Destroy
}

// FullAddress prepends the address prefix (with a dot separator) if non-empty.
func FullAddress(prefix, addr string) string {
	if prefix == "" {
		return addr
	}
	return prefix + "." + addr
}

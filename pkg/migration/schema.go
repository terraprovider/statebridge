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
//   - move:   Source, Destination, ImportID (optional)
//   - rename: Layer, From, To
//   - remove: Layer, Address, Destroy (optional)
//   - import: Layer, Address, ImportID, Provider (optional)
type Operation struct {
	// Type determines the kind of migration operation.
	Type OperationType `yaml:"type"`

	// Description is an optional human-readable description of this operation.
	Description string `yaml:"description,omitempty"`

	// Source identifies the resource and layer to move from (move operations).
	Source *Endpoint `yaml:"source,omitempty"`

	// Destination identifies the target resource and layer to move to (move operations).
	Destination *Endpoint `yaml:"destination,omitempty"`

	// Layer is the filesystem path to the Terraform root module (rename, remove, import operations).
	Layer string `yaml:"layer,omitempty"`

	// From is the original resource address (rename operations).
	From string `yaml:"from,omitempty"`

	// To is the new resource address (rename operations).
	To string `yaml:"to,omitempty"`

	// Address is the resource address (remove and import operations).
	Address string `yaml:"address,omitempty"`

	// Destroy controls whether the resource is destroyed when removed.
	// Defaults to false (safe removal from state only).
	Destroy *bool `yaml:"destroy,omitempty"`

	// ImportID is the provider-specific identifier for importing a resource.
	// Can be a literal string or a Go template expression.
	// For move operations, if omitted, it is auto-resolved from the source state.
	ImportID string `yaml:"import_id,omitempty"`

	// Provider is an optional provider alias override for import blocks.
	Provider string `yaml:"provider,omitempty"`
}

// Endpoint identifies a resource within a specific Terraform layer.
type Endpoint struct {
	// Layer is the filesystem path to the Terraform root module.
	Layer string `yaml:"layer"`

	// Address is the full Terraform resource address (e.g., "aws_instance.web",
	// "module.vpc", or "aws_s3_bucket.data[*]" for wildcard expansion).
	Address string `yaml:"address"`
}

// DestroyValue returns the effective value of the Destroy field,
// defaulting to false if not explicitly set.
func (o *Operation) DestroyValue() bool {
	if o.Destroy == nil {
		return false
	}
	return *o.Destroy
}

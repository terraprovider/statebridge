package generator

import "fmt"

// ImportBlock represents a Terraform/OpenTofu import block.
// It generates HCL that instructs Terraform to import an existing resource
// into the state.
type ImportBlock struct {
	// To is the Terraform address to import the resource to.
	To string

	// ID is the provider-specific import identifier (e.g., AWS resource ID or ARN).
	ID string

	// Provider is an optional provider alias override.
	Provider string

	// Layer is the filesystem path of the layer this block belongs to.
	Layer string

	// Description is an optional human-readable description.
	Description string

	// Source is the migration YAML file path that created this block.
	Source string
}

// Render returns the HCL representation of the import block.
func (b *ImportBlock) Render() string {
	if b.Provider != "" {
		return fmt.Sprintf(`import {
  to       = %s
  id       = %q
  provider = %s
}`, b.To, b.ID, b.Provider)
	}
	return fmt.Sprintf(`import {
  to = %s
  id = %q
}`, b.To, b.ID)
}

// LayerPath returns the target layer directory.
func (b *ImportBlock) LayerPath() string {
	return b.Layer
}

// Comment returns the block's description.
func (b *ImportBlock) Comment() string {
	return b.Description
}

// SourceFile returns the migration YAML file that created this block.
func (b *ImportBlock) SourceFile() string {
	return b.Source
}

// SortAddress returns the import target address for sorting.
func (b *ImportBlock) SortAddress() string {
	return b.To
}

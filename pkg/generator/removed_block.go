package generator

import "fmt"

// RemovedBlock represents a Terraform/OpenTofu removed block.
// It generates HCL that instructs Terraform to stop tracking a resource,
// optionally destroying the underlying infrastructure.
type RemovedBlock struct {
	// From is the resource address being removed from state.
	From string

	// Destroy controls whether the resource is destroyed (true) or just
	// removed from state tracking (false).
	Destroy bool

	// Layer is the filesystem path of the layer this block belongs to.
	Layer string

	// Description is an optional human-readable description.
	Description string

	// Source is the migration YAML file path that created this block.
	Source string

	// SkipConsolidation prevents the engine from replacing this block with a
	// module-level removed block. It is internal generation metadata and isn't
	// rendered into HCL.
	SkipConsolidation bool
}

// Render returns the HCL representation of the removed block.
func (b *RemovedBlock) Render() string {
	return fmt.Sprintf(`removed {
  from = %s

  lifecycle {
    destroy = %t
  }
}`, b.From, b.Destroy)
}

// LayerPath returns the target layer directory.
func (b *RemovedBlock) LayerPath() string {
	return b.Layer
}

// Comment returns the block's description.
func (b *RemovedBlock) Comment() string {
	return b.Description
}

// SourceFile returns the migration YAML file that created this block.
func (b *RemovedBlock) SourceFile() string {
	return b.Source
}

// SortAddress returns the removed resource address for sorting.
func (b *RemovedBlock) SortAddress() string {
	return b.From
}

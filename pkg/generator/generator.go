// Package generator produces Terraform/OpenTofu HCL blocks (import, moved, removed)
// and writes them to layer directories.
package generator

// Block represents a single HCL block to be generated and written to a layer.
type Block interface {
	// Render returns the HCL string representation of the block.
	Render() string

	// LayerPath returns the target layer directory this block belongs to.
	LayerPath() string

	// Comment returns an optional human-readable comment for the block.
	Comment() string
}

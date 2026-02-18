package generator

import "fmt"

// MovedBlock represents a Terraform/OpenTofu moved block.
// It generates HCL that instructs Terraform to track a resource under a new address.
type MovedBlock struct {
	// From is the original resource address.
	From string

	// To is the new resource address.
	To string

	// Layer is the filesystem path of the layer this block belongs to.
	Layer string

	// Description is an optional human-readable description.
	Description string
}

// Render returns the HCL representation of the moved block.
func (b *MovedBlock) Render() string {
	return fmt.Sprintf(`moved {
  from = %s
  to   = %s
}`, b.From, b.To)
}

// LayerPath returns the target layer directory.
func (b *MovedBlock) LayerPath() string {
	return b.Layer
}

// Comment returns the block's description.
func (b *MovedBlock) Comment() string {
	return b.Description
}

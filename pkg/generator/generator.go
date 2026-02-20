// Package generator produces Terraform/OpenTofu HCL blocks (import, moved, removed)
// and writes them to layer directories.
package generator

import "sort"

// Block represents a single HCL block to be generated and written to a layer.
type Block interface {
	// Render returns the HCL string representation of the block.
	Render() string

	// LayerPath returns the target layer directory this block belongs to.
	LayerPath() string

	// Comment returns an optional human-readable comment for the block.
	Comment() string

	// SourceFile returns the migration YAML file path that created this block.
	SourceFile() string

	// SortAddress returns the primary address used for deterministic sorting
	// within blocks of the same type.
	SortAddress() string
}

// blockTypePriority returns a numeric priority for sorting by block type.
// Removed blocks come first, then moved, then import.
func blockTypePriority(b Block) int {
	switch b.(type) {
	case *RemovedBlock:
		return 1
	case *MovedBlock:
		return 2
	case *ImportBlock:
		return 3
	default:
		return 9
	}
}

// SortBlocks sorts blocks deterministically: by block type priority first,
// then alphabetically by sort address within each type.
func SortBlocks(blocks []Block) {
	sort.SliceStable(blocks, func(i, j int) bool {
		pi, pj := blockTypePriority(blocks[i]), blockTypePriority(blocks[j])
		if pi != pj {
			return pi < pj
		}
		return blocks[i].SortAddress() < blocks[j].SortAddress()
	})
}

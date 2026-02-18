// Package engine orchestrates the migration pipeline: parsing migration files,
// reading state, resolving import IDs, expanding wildcards, and generating HCL output.
package engine

import (
	"strings"

	"github.com/redtenant/tfmigrate/pkg/state"
)

// IsWildcard returns true if the address contains the [*] wildcard notation,
// indicating that it should be expanded against state to enumerate all instances.
func IsWildcard(address string) bool {
	return strings.HasSuffix(address, "[*]")
}

// BaseAddress strips the [*] wildcard suffix from an address, returning the
// base resource address suitable for prefix-based state lookups.
// For example, "aws_s3_bucket.data[*]" becomes "aws_s3_bucket.data".
func BaseAddress(address string) string {
	return strings.TrimSuffix(address, "[*]")
}

// ExpandedInstance represents a single resource instance produced by expanding
// a wildcard address against state.
type ExpandedInstance struct {
	// SourceResource is the resource info from the source state.
	SourceResource *state.ResourceInfo

	// DestAddress is the rendered destination address for this instance.
	DestAddress string

	// ImportID is the resolved import ID for this instance.
	ImportID string
}

package engine

import (
	"fmt"
	"sort"
	"strings"
)

// wildcardSourceKey identifies a unique for_each move source by its layer
// path and base resource address.
type wildcardSourceKey struct {
	layer    string
	baseAddr string
}

// wildcardGroup tracks state for a group of wildcard move operations that
// target the same source resource in the same layer.
type wildcardGroup struct {
	// allKeys contains all for_each keys from state for this resource.
	allKeys map[string]bool

	// claimedKeys maps each claimed key to the operation index that claimed it.
	claimedKeys map[string]int

	// removedEmitted tracks whether a removed block has already been emitted
	// for this source, preventing duplicates when multiple prefix-filtered
	// operations target the same resource.
	removedEmitted bool

	// prefixFiltered indicates that at least one operation in this group
	// uses a keys map, which activates the completeness check.
	prefixFiltered bool
}

// wildcardTracker coordinates multiple keyed move operations that target
// the same source resource. It handles:
//   - Deduplication of removed blocks (only one per source resource)
//   - Key claim tracking to detect overlapping key patterns
//   - Completeness verification to ensure all state keys are covered
//   - Destination-side deduplication when merge_duplicates is enabled
type wildcardTracker struct {
	groups map[wildcardSourceKey]*wildcardGroup

	// destinationImports tracks import blocks by (layer, destAddr) to detect
	// and deduplicate cases where multiple source resources produce the same
	// destination address.
	destinationImports map[string]*destinationClaim
}

// destinationClaim records the first import block claim for a destination address.
type destinationClaim struct {
	importID   string
	sourceAddr string
	opIndex    int
}

// newWildcardTracker creates a new tracker for coordinating wildcard moves.
func newWildcardTracker() *wildcardTracker {
	return &wildcardTracker{
		groups:             make(map[wildcardSourceKey]*wildcardGroup),
		destinationImports: make(map[string]*destinationClaim),
	}
}

// getOrCreateGroup returns the group for the given source key, creating it
// if it doesn't exist yet.
func (t *wildcardTracker) getOrCreateGroup(key wildcardSourceKey) *wildcardGroup {
	if g, ok := t.groups[key]; ok {
		return g
	}
	g := &wildcardGroup{
		allKeys:     make(map[string]bool),
		claimedKeys: make(map[string]int),
	}
	t.groups[key] = g
	return g
}

// setAllKeys records all for_each keys from state for the given source.
// This is called once per source when the source is first encountered.
func (t *wildcardTracker) setAllKeys(key wildcardSourceKey, keys []string) {
	g := t.getOrCreateGroup(key)
	for _, k := range keys {
		g.allKeys[k] = true
	}
}

// markPrefixFiltered flags that the given source uses keyed moves,
// which activates the completeness check for that source.
func (t *wildcardTracker) markPrefixFiltered(key wildcardSourceKey) {
	t.getOrCreateGroup(key).prefixFiltered = true
}

// claimKeys registers keys as claimed by the given operation. Returns an
// error if any key has already been claimed by a different operation
// (overlapping prefixes).
func (t *wildcardTracker) claimKeys(key wildcardSourceKey, keys []string, opIndex int) error {
	g := t.getOrCreateGroup(key)
	for _, k := range keys {
		if existingOp, ok := g.claimedKeys[k]; ok {
			return fmt.Errorf(
				"key %q is claimed by both operation[%d] and operation[%d] for %s in layer %q",
				k, existingOp, opIndex, key.baseAddr, key.layer,
			)
		}
		g.claimedKeys[k] = opIndex
	}
	return nil
}

// shouldEmitRemoved returns true on the first call for a given source key,
// and false on subsequent calls. This ensures only one removed block is
// emitted per source resource even when multiple prefix-filtered operations
// target the same source.
func (t *wildcardTracker) shouldEmitRemoved(key wildcardSourceKey) bool {
	g := t.getOrCreateGroup(key)
	if g.removedEmitted {
		return false
	}
	g.removedEmitted = true
	return true
}

// checkCompleteness verifies that all keys in state are covered by at least
// one operation for every prefix-filtered source. Returns an error listing
// any uncovered keys.
func (t *wildcardTracker) checkCompleteness() error {
	for key, g := range t.groups {
		if !g.prefixFiltered {
			continue
		}

		var uncovered []string
		for k := range g.allKeys {
			if _, ok := g.claimedKeys[k]; !ok {
				uncovered = append(uncovered, k)
			}
		}

		if len(uncovered) > 0 {
			sort.Strings(uncovered)
			return fmt.Errorf(
				"completeness check failed for %s[*] in layer %q: %d key(s) not covered by any move operation:\n  - %s",
				key.baseAddr, key.layer, len(uncovered), strings.Join(uncovered, "\n  - "),
			)
		}
	}

	return nil
}

// claimDestination attempts to register an import block at the given destination
// address. When mergeDuplicates is true:
//   - If no previous claim exists, the import is registered and skip=false.
//   - If a previous claim exists with the same importID, skip=true (deduplicated).
//   - If a previous claim exists with a different importID, an error is returned.
//
// When mergeDuplicates is false:
//   - If a previous claim exists, an error is returned (duplicate destination).
//   - Otherwise, the import is registered and skip=false.
func (t *wildcardTracker) claimDestination(layer, destAddr, importID string, opIndex int, sourceAddr string, mergeDuplicates bool) (skip bool, err error) {
	key := layer + "\x00" + destAddr
	existing, ok := t.destinationImports[key]
	if !ok {
		t.destinationImports[key] = &destinationClaim{
			importID:   importID,
			sourceAddr: sourceAddr,
			opIndex:    opIndex,
		}
		return false, nil
	}

	if !mergeDuplicates {
		return false, fmt.Errorf(
			"duplicate import for %q in layer %q: first from %q (operation[%d]), again from %q (operation[%d]); set merge_duplicates: true on both resources to deduplicate",
			destAddr, layer, existing.sourceAddr, existing.opIndex, sourceAddr, opIndex,
		)
	}

	// merge_duplicates is true: check import ID consistency
	if existing.importID != importID {
		return false, fmt.Errorf(
			"merge_duplicates conflict for %q in layer %q: import ID %q (from %q, operation[%d]) differs from %q (from %q, operation[%d])",
			destAddr, layer, existing.importID, existing.sourceAddr, existing.opIndex, importID, sourceAddr, opIndex,
		)
	}

	// Same destination, same import ID, merge_duplicates enabled → skip
	return true, nil
}

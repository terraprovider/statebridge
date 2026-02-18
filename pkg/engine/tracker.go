package engine

import (
	"fmt"
	"sort"
	"strings"
)

// wildcardSourceKey identifies a unique wildcard move source by its layer
// path and base resource address (without the [*] suffix).
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
	// uses key_prefix filtering, which activates the completeness check.
	prefixFiltered bool
}

// wildcardTracker coordinates multiple wildcard move operations that target
// the same source resource. It handles:
//   - Deduplication of removed blocks (only one per source resource)
//   - Key claim tracking to detect overlapping prefixes
//   - Completeness verification to ensure all state keys are covered
type wildcardTracker struct {
	groups map[wildcardSourceKey]*wildcardGroup
}

// newWildcardTracker creates a new tracker for coordinating wildcard moves.
func newWildcardTracker() *wildcardTracker {
	return &wildcardTracker{
		groups: make(map[wildcardSourceKey]*wildcardGroup),
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

// markPrefixFiltered flags that the given source uses prefix-based filtering,
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

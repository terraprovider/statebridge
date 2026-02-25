package engine

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/redtenant/tfmigrate/pkg/generator"
	"github.com/redtenant/tfmigrate/pkg/state"
)

// consolidateModuleRemovals replaces individual removed blocks with a single
// module-level removed block when ALL managed resources within a module are
// being removed. Works at any nesting depth, consolidating from the deepest
// module upward.
//
// For example, if a layer's state contains:
//
//	module.foo.aws_instance.web
//	module.foo.aws_s3_bucket.data
//
// and both resources have removed blocks, they are replaced with a single:
//
//	removed { from = module.foo ... }
func (e *Engine) consolidateModuleRemovals(ctx context.Context, blocks []generator.Block) []generator.Block {
	// Separate removed blocks from other blocks, grouped by layer
	type layerRemovals struct {
		blocks  []*generator.RemovedBlock
		indices []int // original indices in the blocks slice
	}
	removedByLayer := make(map[string]*layerRemovals)
	for i, b := range blocks {
		rb, ok := b.(*generator.RemovedBlock)
		if !ok {
			continue
		}
		layer := rb.Layer
		if removedByLayer[layer] == nil {
			removedByLayer[layer] = &layerRemovals{}
		}
		removedByLayer[layer].blocks = append(removedByLayer[layer].blocks, rb)
		removedByLayer[layer].indices = append(removedByLayer[layer].indices, i)
	}

	if len(removedByLayer) == 0 {
		return blocks
	}

	// For each layer, check if any modules can be consolidated
	consolidated := make(map[int]bool) // indices of blocks to remove
	var newBlocks []generator.Block     // consolidated module-level blocks to add

	for layerPath, lr := range removedByLayer {
		// Build set of removed base addresses
		removedSet := make(map[string]bool)
		for _, rb := range lr.blocks {
			removedSet[rb.From] = true
		}

		// Extract all unique module paths from removed addresses
		modulePathSet := make(map[string]bool)
		for addr := range removedSet {
			for _, mp := range allModulePrefixes(addr) {
				modulePathSet[mp] = true
			}
		}

		if len(modulePathSet) == 0 {
			continue // no module-prefixed resources
		}

		// Read state for this layer to get all managed resources
		s, err := e.resolver.ReadState(ctx, layerPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not read state for module consolidation in %q: %v\n", layerPath, err)
			continue
		}
		idx := state.NewStateIndex(s)
		allManaged := idx.ManagedBaseAddresses()

		// Sort module paths from deepest (longest) to shallowest
		modulePaths := make([]string, 0, len(modulePathSet))
		for mp := range modulePathSet {
			modulePaths = append(modulePaths, mp)
		}
		sort.Slice(modulePaths, func(i, j int) bool {
			// Deeper modules first (more dots = deeper)
			di := strings.Count(modulePaths[i], ".")
			dj := strings.Count(modulePaths[j], ".")
			if di != dj {
				return di > dj
			}
			return modulePaths[i] < modulePaths[j]
		})

		// Track which addresses are covered by consolidated modules
		coveredSet := make(map[string]bool)
		consolidatedModules := make(map[string]bool)

		for _, mp := range modulePaths {
			prefix := mp + "."

			// Get all managed resources in state under this module
			var stateUnder []string
			for _, addr := range allManaged {
				if strings.HasPrefix(addr, prefix) {
					stateUnder = append(stateUnder, addr)
				}
			}

			if len(stateUnder) == 0 {
				continue
			}

			// Check if ALL state resources under this module are covered
			allCovered := true
			for _, addr := range stateUnder {
				if !removedSet[addr] && !coveredSet[addr] {
					allCovered = false
					break
				}
			}

			if allCovered {
				consolidatedModules[mp] = true
				for _, addr := range stateUnder {
					coveredSet[addr] = true
				}
			}
		}

		if len(consolidatedModules) == 0 {
			continue
		}

		// Filter redundant consolidations: if module.foo is consolidated,
		// don't also emit module.foo.module.bar
		finalModules := make(map[string]bool)
		for mp := range consolidatedModules {
			redundant := false
			for other := range consolidatedModules {
				if other != mp && strings.HasPrefix(mp, other+".") {
					redundant = true
					break
				}
			}
			if !redundant {
				finalModules[mp] = true
			}
		}

		// Mark individual removed blocks that are now covered
		for i, rb := range lr.blocks {
			if coveredSet[rb.From] {
				consolidated[lr.indices[i]] = true
			}
		}

		// Create module-level removed blocks
		// Inherit properties from the first removed block in this layer
		template := lr.blocks[0]
		for mp := range finalModules {
			newBlocks = append(newBlocks, &generator.RemovedBlock{
				From:        mp,
				Destroy:     template.Destroy,
				Layer:       template.Layer,
				Description: fmt.Sprintf("Remove entire module %s from state", mp),
				Source:      template.Source,
			})
			fmt.Fprintf(os.Stderr, "Consolidated removed blocks: %s (all managed resources moved)\n", mp)
		}
	}

	if len(consolidated) == 0 {
		return blocks
	}

	// Build final block list: non-consolidated originals + new module-level blocks
	result := make([]generator.Block, 0, len(blocks)-len(consolidated)+len(newBlocks))
	for i, b := range blocks {
		if !consolidated[i] {
			result = append(result, b)
		}
	}
	result = append(result, newBlocks...)

	return result
}

// extractModulePath returns the innermost module path from a resource address.
// "module.foo.aws_instance.web" → "module.foo"
// "module.foo.module.bar.aws_instance.web" → "module.foo.module.bar"
// "aws_instance.web" → "" (root module, no module path)
func extractModulePath(address string) string {
	if !strings.HasPrefix(address, "module.") {
		return ""
	}

	// Walk the address in pairs of segments to find where modules end
	// and the resource type begins. Module segments are "module.<name>",
	// resource segments are "<type>.<name>".
	parts := strings.Split(address, ".")
	moduleEnd := 0
	for i := 0; i+1 < len(parts); i += 2 {
		if parts[i] == "module" {
			moduleEnd = i + 2 // include "module" and "<name>"
		} else {
			break // hit a resource type
		}
	}

	if moduleEnd == 0 {
		return ""
	}

	return strings.Join(parts[:moduleEnd], ".")
}

// allModulePrefixes returns all module prefixes for an address, from deepest to shallowest.
// "module.foo.module.bar.aws_instance.web" → ["module.foo.module.bar", "module.foo"]
// "module.foo.aws_instance.web" → ["module.foo"]
// "aws_instance.web" → []
func allModulePrefixes(address string) []string {
	if !strings.HasPrefix(address, "module.") {
		return nil
	}

	parts := strings.Split(address, ".")
	var prefixes []string

	// Walk pairs, collecting module prefixes as we go
	for i := 0; i+1 < len(parts); i += 2 {
		if parts[i] != "module" {
			break
		}
		prefixes = append(prefixes, strings.Join(parts[:i+2], "."))
	}

	// Reverse: deepest first
	for i, j := 0, len(prefixes)-1; i < j; i, j = i+1, j-1 {
		prefixes[i], prefixes[j] = prefixes[j], prefixes[i]
	}

	return prefixes
}

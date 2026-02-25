package engine

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/redtenant/tfmigrate/pkg/generator"
	"github.com/redtenant/tfmigrate/pkg/migration"
	"github.com/redtenant/tfmigrate/pkg/state"
)

// Config holds the configuration for the Engine.
type Config struct {
	// StateReader reads Terraform/OpenTofu state for layers.
	StateReader state.StateReader

	// DryRun if true prints output but does not write files.
	DryRun bool
}

// Engine orchestrates the full migration pipeline: parse YAML, read state,
// resolve imports, expand wildcards, generate HCL, and write output files.
type Engine struct {
	config            Config
	writer            *generator.Writer
	resolver          *Resolver
	parser            *migration.Parser
	currentSourceFile string
}

// New creates a new Engine with the given configuration.
func New(cfg Config) *Engine {
	w := generator.NewWriter()
	w.DryRun = cfg.DryRun

	return &Engine{
		config:   cfg,
		writer:   w,
		resolver: NewResolver(cfg.StateReader),
		parser:   migration.NewParser(),
	}
}

// Writer returns the underlying Writer for accessing rendered content
// (e.g., for dry-run output).
func (e *Engine) Writer() *generator.Writer {
	return e.writer
}

// ProcessFiles parses and processes a list of migration file paths (or directories),
// generates HCL blocks, and writes them to the appropriate layer directories.
// Returns the list of generated file paths.
//
// When a migration file fails during condition evaluation or processing (e.g.,
// because a source resource no longer exists in state), it is skipped with an
// informational message to stderr. This allows unrelated migration files to
// still be generated. Parse errors and validation errors remain fatal.
func (e *Engine) ProcessFiles(ctx context.Context, paths []string) ([]string, error) {
	files, err := e.parser.ParseFiles(paths)
	if err != nil {
		return nil, fmt.Errorf("parsing migration files: %w", err)
	}

	var skipped []string
	var allBlocks []generator.Block

	for _, mf := range files {
		if errs := migration.Validate(mf); len(errs) > 0 {
			var msgs []string
			for _, ve := range errs {
				msgs = append(msgs, ve.Error())
			}
			return nil, fmt.Errorf("validation errors in %q:\n  %s", mf.FilePath, strings.Join(msgs, "\n  "))
		}

		proceed, err := e.evaluateCondition(ctx, mf)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Skipping %q: %v\n", mf.FilePath, err)
			skipped = append(skipped, mf.FilePath)
			continue
		}
		if !proceed {
			continue
		}

		blocks, err := e.processMigration(ctx, mf)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Skipping %q: %v\n", mf.FilePath, err)
			skipped = append(skipped, mf.FilePath)
			continue
		}

		allBlocks = append(allBlocks, blocks...)

		// Store metadata (conditions, init args) for this migration file.
		// The Writer uses this at render time to embed metadata comments
		// in the generated .tf files.
		e.writer.SetFileMetadata(mf.FilePath, buildFileMetadata(mf))
	}

	if len(skipped) > 0 && len(allBlocks) == 0 {
		return nil, fmt.Errorf("all migration files were skipped: %s", strings.Join(skipped, ", "))
	}

	// Consolidate module-level removals: when all managed resources within
	// a module are being removed, replace individual removed blocks with
	// a single module-level removed block.
	allBlocks = e.consolidateModuleRemovals(ctx, allBlocks)

	e.writer.AddBlocks(allBlocks)

	return e.writer.WriteAll()
}

// processMigration processes a single parsed migration file, generating
// HCL blocks for each operation. For keyed moves, it coordinates across
// operations to ensure completeness and prevent duplicate removed blocks.
// Returns the collected blocks; the caller decides when to commit them
// to the Writer.
func (e *Engine) processMigration(ctx context.Context, mf *migration.MigrationFile) ([]generator.Block, error) {
	e.currentSourceFile = mf.FilePath
	tracker := newWildcardTracker()

	var allBlocks []generator.Block
	for i, op := range mf.Operations {
		blocks, err := e.processOperation(ctx, &op, i, tracker)
		if err != nil {
			return nil, fmt.Errorf("operation[%d] (%s): %w", i, op.Type, err)
		}
		allBlocks = append(allBlocks, blocks...)
	}

	if err := tracker.checkCompleteness(); err != nil {
		return nil, err
	}

	return allBlocks, nil
}

// processOperation dispatches a single operation to the appropriate handler.
func (e *Engine) processOperation(ctx context.Context, op *migration.Operation, opIndex int, tracker *wildcardTracker) ([]generator.Block, error) {
	switch op.Type {
	case migration.OpMove:
		return e.processMove(ctx, op, opIndex, tracker)
	case migration.OpRename:
		return e.processRename(op)
	case migration.OpRemove:
		return e.processRemove(op)
	case migration.OpImport:
		return e.processImport(op)
	default:
		return nil, fmt.Errorf("unknown operation type %q", op.Type)
	}
}

// processMove handles move operations, iterating over each resource entry
// and generating removed blocks in the source layer and import blocks in
// the destination layer. Supports keyed moves with key matching and cross-
// operation completeness tracking.
func (e *Engine) processMove(ctx context.Context, op *migration.Operation, opIndex int, tracker *wildcardTracker) ([]generator.Block, error) {
	srcLayer := op.SourceLayer
	dstLayer := op.DestinationLayer

	if op.AllResources {
		return e.processMoveAllResources(ctx, srcLayer, dstLayer, op.Resources, op.Description)
	}

	var blocks []generator.Block

	for i, res := range op.Resources {
		srcAddr := migration.FullAddress(op.AddressPrefix, res.Address)
		dstBaseAddr := res.DestinationAddress
		if dstBaseAddr == "" {
			dstBaseAddr = res.Address
		}
		dstAddr := migration.FullAddress(op.AddressPrefix, dstBaseAddr)

		resBlocks, err := e.processMoveResource(ctx, srcLayer, dstLayer, srcAddr, dstAddr, &res, opIndex, i, tracker, op.Description)
		if err != nil {
			return nil, fmt.Errorf("resource %q: %w", res.Address, err)
		}
		blocks = append(blocks, resBlocks...)
	}

	return blocks, nil
}

// processMoveResource handles a single resource entry within a move operation.
// If the address is a module path, it discovers all resources under the module
// and generates blocks for each. If the resource has a keys map, it performs
// keyed expansion with pattern matching. Otherwise, it moves the resource as-is.
func (e *Engine) processMoveResource(
	ctx context.Context,
	srcLayer, dstLayer, srcAddr, dstAddr string,
	res *migration.ResourceMove,
	opIndex, resIndex int,
	tracker *wildcardTracker,
	description string,
) ([]generator.Block, error) {
	// Module-level move: discover all resources under the module prefix
	if migration.IsModuleAddress(srcAddr) {
		return e.processMoveModule(ctx, srcLayer, dstLayer, srcAddr, dstAddr, description)
	}

	if len(res.Keys) > 0 {
		return e.processMoveKeyed(ctx, srcLayer, dstLayer, srcAddr, dstAddr, res, opIndex, tracker, description)
	}

	return e.processMoveSimple(ctx, srcLayer, dstLayer, srcAddr, dstAddr, res, tracker, description)
}

// processMoveModule handles a module-level move by discovering all managed
// resources under the source module prefix and generating removed + import
// blocks for each. Resources are grouped by base address so that for_each
// resources get a single removed block per base and individual import blocks
// per instance.
//
// The existing consolidateModuleRemovals post-processing step will collapse
// the individual removed blocks into a single module-level removed block.
func (e *Engine) processMoveModule(
	ctx context.Context,
	srcLayer, dstLayer, srcAddr, dstAddr string,
	description string,
) ([]generator.Block, error) {
	resources, err := e.resolver.LookupModuleResources(ctx, srcLayer, srcAddr)
	if err != nil {
		return nil, err
	}

	// Group resources by base address (strip key suffixes like ["key"])
	// so for_each resources get one removed block per base, not per instance.
	type baseGroup struct {
		baseAddr  string
		resources []*state.ResourceInfo
	}
	groupMap := make(map[string]*baseGroup)
	var groupOrder []string
	for _, r := range resources {
		base := stripKeyFromAddress(r.Address)
		if _, ok := groupMap[base]; !ok {
			groupMap[base] = &baseGroup{baseAddr: base}
			groupOrder = append(groupOrder, base)
		}
		groupMap[base].resources = append(groupMap[base].resources, r)
	}

	srcPrefix := srcAddr + "."
	var blocks []generator.Block

	for _, base := range groupOrder {
		g := groupMap[base]

		// Compute destination base by swapping module prefix
		suffix := strings.TrimPrefix(g.baseAddr, srcPrefix)
		destBase := dstAddr + "." + suffix

		// Emit removed block for this base address
		blocks = append(blocks, &generator.RemovedBlock{
			From:        g.baseAddr,
			Destroy:     false,
			Layer:       srcLayer,
			Description: description,
			Source:      e.currentSourceFile,
		})

		// Emit import blocks for each instance
		for _, r := range g.resources {
			importID, err := e.resolver.ResolveImportID(r, "")
			if err != nil {
				return nil, fmt.Errorf("resolving import ID for %q: %w", r.Address, err)
			}

			destAddr := destBase
			if r.Key != "" {
				destAddr = fmt.Sprintf("%s[\"%s\"]", destBase, r.Key)
			}

			blocks = append(blocks, &generator.ImportBlock{
				To:          destAddr,
				ID:          importID,
				Layer:       dstLayer,
				Description: description,
				Source:      e.currentSourceFile,
			})
		}
	}

	return blocks, nil
}

// stripKeyFromAddress removes the key/index suffix from a resource address.
// "aws_s3_bucket.data[\"key\"]" → "aws_s3_bucket.data"
// "aws_instance.web" → "aws_instance.web"
func stripKeyFromAddress(address string) string {
	if idx := strings.Index(address, "["); idx >= 0 {
		return address[:idx]
	}
	return address
}

// processMoveAllResources handles an all_resources move by discovering all
// managed resources in the source layer and generating removed + import blocks
// for each. Optional overrides allow renaming specific resources during the move.
func (e *Engine) processMoveAllResources(
	ctx context.Context,
	srcLayer, dstLayer string,
	overrides []migration.ResourceMove,
	description string,
) ([]generator.Block, error) {
	resources, err := e.resolver.LookupAllManagedResources(ctx, srcLayer)
	if err != nil {
		return nil, err
	}

	// Build override map: base address → destination address
	overrideMap := make(map[string]string)
	for _, res := range overrides {
		overrideMap[res.Address] = res.DestinationAddress
	}

	// Group resources by base address (strip key suffixes)
	type baseGroup struct {
		baseAddr  string
		resources []*state.ResourceInfo
	}
	groupMap := make(map[string]*baseGroup)
	var groupOrder []string
	for _, r := range resources {
		base := stripKeyFromAddress(r.Address)
		if _, ok := groupMap[base]; !ok {
			groupMap[base] = &baseGroup{baseAddr: base}
			groupOrder = append(groupOrder, base)
		}
		groupMap[base].resources = append(groupMap[base].resources, r)
	}

	var blocks []generator.Block

	for _, base := range groupOrder {
		g := groupMap[base]

		// Determine destination base address (override or same)
		destBase := g.baseAddr
		if dst, ok := overrideMap[g.baseAddr]; ok {
			destBase = dst
		}

		// Emit removed block
		blocks = append(blocks, &generator.RemovedBlock{
			From:        g.baseAddr,
			Destroy:     false,
			Layer:       srcLayer,
			Description: description,
			Source:      e.currentSourceFile,
		})

		// Emit import blocks for each instance
		for _, r := range g.resources {
			importID, err := e.resolver.ResolveImportID(r, "")
			if err != nil {
				return nil, fmt.Errorf("resolving import ID for %q: %w", r.Address, err)
			}

			destAddr := destBase
			if r.Key != "" {
				destAddr = fmt.Sprintf("%s[\"%s\"]", destBase, r.Key)
			}

			blocks = append(blocks, &generator.ImportBlock{
				To:          destAddr,
				ID:          importID,
				Layer:       dstLayer,
				Description: description,
				Source:      e.currentSourceFile,
			})
		}
	}

	return blocks, nil
}

// processMoveSimple handles a resource move without a keys map.
// It looks up the resource in state and generates appropriate blocks:
//   - For for_each resources: expands all instances, imports each, removes the base resource
//   - For single resources: removes and imports the single resource
func (e *Engine) processMoveSimple(
	ctx context.Context,
	srcLayer, dstLayer, srcAddr, dstAddr string,
	res *migration.ResourceMove,
	tracker *wildcardTracker,
	description string,
) ([]generator.Block, error) {
	// Look up all instances of this resource from state
	resources, err := e.resolver.LookupResources(ctx, srcLayer, srcAddr)
	if err != nil {
		return nil, err
	}

	var blocks []generator.Block

	// Check if this is a for_each resource (multiple instances or instances with keys)
	isForEach := len(resources) > 1 || (len(resources) == 1 && resources[0].Key != "")

	if isForEach {
		// For_each resource without keys map: move all instances with same keys
		srcKey := wildcardSourceKey{layer: srcLayer, baseAddr: srcAddr}

		allKeys := make([]string, len(resources))
		for i, r := range resources {
			allKeys[i] = r.Key
		}
		tracker.setAllKeys(srcKey, allKeys)

		if tracker.shouldEmitRemoved(srcKey) {
			blocks = append(blocks, &generator.RemovedBlock{
				From:        srcAddr,
				Destroy:     false,
				Layer:       srcLayer,
				Description: description,
				Source:      e.currentSourceFile,
			})
		}

		for _, r := range resources {
			destFullAddr := fmt.Sprintf("%s[\"%s\"]", dstAddr, r.Key)
			importID, err := e.resolver.ResolveImportID(r, res.ImportID)
			if err != nil {
				return nil, fmt.Errorf("resolving import ID for %q: %w", r.Address, err)
			}
			blocks = append(blocks, &generator.ImportBlock{
				To:          destFullAddr,
				ID:          importID,
				Layer:       dstLayer,
				Description: description,
				Source:      e.currentSourceFile,
			})
		}
	} else {
		// Single resource (non-for_each)
		r := resources[0]
		importID, err := e.resolver.ResolveImportID(r, res.ImportID)
		if err != nil {
			return nil, fmt.Errorf("resolving import ID for %q: %w", r.Address, err)
		}

		blocks = append(blocks,
			&generator.RemovedBlock{
				From:        srcAddr,
				Destroy:     false,
				Layer:       srcLayer,
				Description: description,
				Source:      e.currentSourceFile,
			},
			&generator.ImportBlock{
				To:          dstAddr,
				ID:          importID,
				Layer:       dstLayer,
				Description: description,
				Source:      e.currentSourceFile,
			},
		)
	}

	return blocks, nil
}

// processMoveKeyed handles a resource move with a keys map, performing pattern
// matching against state keys and generating import blocks for each match.
// Uses the wildcard tracker for cross-operation coordination, overlap detection,
// and completeness checking.
func (e *Engine) processMoveKeyed(
	ctx context.Context,
	srcLayer, dstLayer, srcAddr, dstAddr string,
	res *migration.ResourceMove,
	opIndex int,
	tracker *wildcardTracker,
	description string,
) ([]generator.Block, error) {
	srcKey := wildcardSourceKey{layer: srcLayer, baseAddr: srcAddr}

	// Look up all for_each instances from state
	resources, err := e.resolver.LookupResources(ctx, srcLayer, srcAddr)
	if err != nil {
		return nil, err
	}

	// On first encounter of this source, register all keys with tracker
	if _, exists := tracker.groups[srcKey]; !exists {
		allKeys := make([]string, len(resources))
		for i, r := range resources {
			allKeys[i] = r.Key
		}
		tracker.setAllKeys(srcKey, allKeys)
	}
	tracker.markPrefixFiltered(srcKey)

	// Build key matcher from the keys map
	matcher, err := newKeyMatcher(res.Keys)
	if err != nil {
		return nil, fmt.Errorf("building key matcher: %w", err)
	}

	// Match state keys and generate import blocks
	var blocks []generator.Block
	var claimedKeys []string

	for _, r := range resources {
		destTemplate, matched := matcher.Match(r.Key)
		if !matched {
			continue // unmatched keys handled by completeness check
		}

		claimedKeys = append(claimedKeys, r.Key)

		// Evaluate the destination key template
		destKey, err := e.resolver.EvaluateTemplate(destTemplate, r)
		if err != nil {
			return nil, fmt.Errorf("evaluating destination key template for key %q: %w", r.Key, err)
		}

		// Construct full destination address
		destFullAddr := fmt.Sprintf("%s[\"%s\"]", dstAddr, destKey)

		// Resolve import ID
		importID, err := e.resolver.ResolveImportID(r, res.ImportID)
		if err != nil {
			return nil, fmt.Errorf("resolving import ID for key %q: %w", r.Key, err)
		}

		blocks = append(blocks, &generator.ImportBlock{
			To:          destFullAddr,
			ID:          importID,
			Layer:       dstLayer,
			Description: description,
			Source:      e.currentSourceFile,
		})
	}

	// Claim matched keys in tracker (detects overlaps)
	if err := tracker.claimKeys(srcKey, claimedKeys, opIndex); err != nil {
		return nil, err
	}

	// Emit removed block (deduplicated across operations)
	if tracker.shouldEmitRemoved(srcKey) {
		// Prepend removed block before import blocks
		removedBlock := &generator.RemovedBlock{
			From:        srcAddr,
			Destroy:     false,
			Layer:       srcLayer,
			Description: description,
			Source:      e.currentSourceFile,
		}
		blocks = append([]generator.Block{removedBlock}, blocks...)
	}

	return blocks, nil
}

// processRename handles rename operations, generating moved blocks for each
// rename entry within the operation.
func (e *Engine) processRename(op *migration.Operation) ([]generator.Block, error) {
	var blocks []generator.Block
	for _, entry := range op.Renames {
		blocks = append(blocks, &generator.MovedBlock{
			From:        migration.FullAddress(op.AddressPrefix, entry.From),
			To:          migration.FullAddress(op.AddressPrefix, entry.To),
			Layer:       op.Layer,
			Description: op.Description,
			Source:      e.currentSourceFile,
		})
	}
	return blocks, nil
}

// processRemove handles remove operations, generating removed blocks for each
// address in the operation.
func (e *Engine) processRemove(op *migration.Operation) ([]generator.Block, error) {
	var blocks []generator.Block
	for _, addr := range op.Addresses {
		blocks = append(blocks, &generator.RemovedBlock{
			From:        migration.FullAddress(op.AddressPrefix, addr),
			Destroy:     op.DestroyValue(),
			Layer:       op.Layer,
			Description: op.Description,
			Source:      e.currentSourceFile,
		})
	}
	return blocks, nil
}

// processImport handles import operations, generating import blocks for each
// import entry in the operation.
func (e *Engine) processImport(op *migration.Operation) ([]generator.Block, error) {
	var blocks []generator.Block
	for _, entry := range op.Imports {
		blocks = append(blocks, &generator.ImportBlock{
			To:          migration.FullAddress(op.AddressPrefix, entry.Address),
			ID:          entry.ImportID,
			Provider:    entry.Provider,
			Layer:       op.Layer,
			Description: op.Description,
			Source:      e.currentSourceFile,
		})
	}
	return blocks, nil
}

// evaluateCondition checks whether a migration file's preconditions are met.
// Returns (true, nil) if there is no condition or all checks pass.
// Returns (false, nil) if a check fails (logs skip reason to stderr).
// Returns (false, error) if a state read error occurs.
func (e *Engine) evaluateCondition(ctx context.Context, mf *migration.MigrationFile) (bool, error) {
	if mf.Condition == nil {
		return true, nil
	}

	indexCache := make(map[string]*state.StateIndex)
	getIndex := func(layer string) (*state.StateIndex, error) {
		if idx, ok := indexCache[layer]; ok {
			return idx, nil
		}
		s, err := e.resolver.ReadState(ctx, layer)
		if err != nil {
			return nil, err
		}
		idx := state.NewStateIndex(s)
		indexCache[layer] = idx
		return idx, nil
	}

	for _, check := range mf.Condition.ResourcesExist {
		idx, err := getIndex(check.Layer)
		if err != nil {
			return false, err
		}
		for _, addr := range check.Addresses {
			if !idx.ResourceExists(addr) {
				fmt.Fprintf(os.Stderr, "Skipping %q: resource %q not found in layer %q\n", mf.FilePath, addr, check.Layer)
				return false, nil
			}
		}
	}

	for _, check := range mf.Condition.ResourcesNotExist {
		idx, err := getIndex(check.Layer)
		if err != nil {
			return false, err
		}
		for _, addr := range check.Addresses {
			if idx.ResourceExists(addr) {
				fmt.Fprintf(os.Stderr, "Skipping %q: resource %q already exists in layer %q\n", mf.FilePath, addr, check.Layer)
				return false, nil
			}
		}
	}

	return true, nil
}

// buildFileMetadata creates a MigrationMetadata from a parsed MigrationFile.
// The Resources field is left empty here; it will be populated by the Writer
// at render time from the blocks in each (layer, sourceFile) group.
func buildFileMetadata(mf *migration.MigrationFile) *generator.MigrationMetadata {
	meta := &generator.MigrationMetadata{}

	if mf.Condition != nil {
		meta.Conditions = convertCondition(mf.Condition)
	}

	return meta
}

// convertCondition converts a migration.Condition to a generator.MetadataCondition.
func convertCondition(cond *migration.Condition) *generator.MetadataCondition {
	if cond == nil {
		return nil
	}

	mc := &generator.MetadataCondition{}

	for _, check := range cond.ResourcesExist {
		addrsCopy := make([]string, len(check.Addresses))
		copy(addrsCopy, check.Addresses)
		mc.ResourcesExist = append(mc.ResourcesExist, generator.MetadataResourceCheck{
			Layer:     check.Layer,
			Addresses: addrsCopy,
		})
	}

	for _, check := range cond.ResourcesNotExist {
		addrsCopy := make([]string, len(check.Addresses))
		copy(addrsCopy, check.Addresses)
		mc.ResourcesNotExist = append(mc.ResourcesNotExist, generator.MetadataResourceCheck{
			Layer:     check.Layer,
			Addresses: addrsCopy,
		})
	}

	if len(mc.ResourcesExist) == 0 && len(mc.ResourcesNotExist) == 0 {
		return nil
	}
	return mc
}

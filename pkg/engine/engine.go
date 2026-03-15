package engine

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/redtenant/tfmigrate/pkg/generator"
	"github.com/redtenant/tfmigrate/pkg/migration"
	"github.com/redtenant/tfmigrate/pkg/state"
	tmpl "github.com/redtenant/tfmigrate/pkg/template"
)

// SkipReason indicates why a migration file was skipped during processing.
type SkipReason int

const (
	SkipRetired      SkipReason = iota // status: retired
	SkipLayerMissing                   // source layer doesn't exist on disk
	SkipCondition                      // condition check failed
	SkipError                          // condition eval or processing error
)

// SkippedFile records a migration file that was skipped and why.
type SkippedFile struct {
	FilePath string
	Stem     string
	Reason   SkipReason
}

// ProcessResult contains the output of ProcessFiles.
type ProcessResult struct {
	OutputFiles  []string
	SkippedFiles []SkippedFile
}

// Config holds the configuration for the Engine.
type Config struct {
	// StateReader reads Terraform/OpenTofu state for layers.
	StateReader state.StateReader

	// DryRun if true prints output but does not write files.
	DryRun bool

	// Strict if true causes missing layer directories to be hard errors
	// instead of auto-skipping the migration file.
	Strict bool
}

// Engine orchestrates the full migration pipeline: parse YAML, read state,
// resolve imports, expand wildcards, generate HCL, and write output files.
type Engine struct {
	config   Config
	writer   *generator.Writer
	resolver *Resolver
	parser   *migration.Parser
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
func (e *Engine) ProcessFiles(ctx context.Context, paths []string) (*ProcessResult, error) {
	files, err := e.parser.ParseFiles(paths)
	if err != nil {
		return nil, fmt.Errorf("parsing migration files: %w", err)
	}

	var skippedFiles []SkippedFile
	var allBlocks []generator.Block

	for _, mf := range files {
		if errs := migration.Validate(mf); len(errs) > 0 {
			var msgs []string
			for _, ve := range errs {
				msgs = append(msgs, ve.Error())
			}
			return nil, fmt.Errorf("validation errors in %q:\n  %s", mf.FilePath, strings.Join(msgs, "\n  "))
		}

		// F1: Skip retired migration files immediately — no state reads needed.
		if mf.Status == migration.StatusRetired {
			fmt.Fprintf(os.Stderr, "Skipping %q: status is retired\n", mf.FilePath)
			skippedFiles = append(skippedFiles, SkippedFile{mf.FilePath, migration.YamlStem(mf.FilePath), SkipRetired})
			continue
		}

		// F2: Auto-skip when referenced layers don't exist on disk.
		if missing := checkLayerPaths(collectLayerPaths(mf)); missing != "" {
			if e.config.Strict {
				return nil, fmt.Errorf("layer %q does not exist (referenced by %q)", missing, mf.FilePath)
			}
			fmt.Fprintf(os.Stderr, "Skipping %q: layer %q does not exist\n", mf.FilePath, missing)
			skippedFiles = append(skippedFiles, SkippedFile{mf.FilePath, migration.YamlStem(mf.FilePath), SkipLayerMissing})
			continue
		}

		proceed, err := e.evaluateCondition(ctx, mf)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Skipping %q: %v\n", mf.FilePath, err)
			skippedFiles = append(skippedFiles, SkippedFile{mf.FilePath, migration.YamlStem(mf.FilePath), SkipError})
			continue
		}
		if !proceed {
			skippedFiles = append(skippedFiles, SkippedFile{mf.FilePath, migration.YamlStem(mf.FilePath), SkipCondition})
			continue
		}

		blocks, err := e.processMigration(ctx, mf)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Skipping %q: %v\n", mf.FilePath, err)
			skippedFiles = append(skippedFiles, SkippedFile{mf.FilePath, migration.YamlStem(mf.FilePath), SkipError})
			continue
		}

		allBlocks = append(allBlocks, blocks...)

		// Store metadata (conditions, init args) for this migration file.
		// The Writer uses this at render time to embed metadata comments
		// in the generated .tf files.
		e.writer.SetFileMetadata(mf.FilePath, buildFileMetadata(mf))
	}

	// Only error-skipped files count toward the "all files skipped" check.
	// Retired and layer-missing files are expected; condition-failed files are waiting.
	var errorSkipped []string
	for _, sf := range skippedFiles {
		if sf.Reason == SkipError {
			errorSkipped = append(errorSkipped, sf.FilePath)
		}
	}
	if len(errorSkipped) > 0 && len(allBlocks) == 0 {
		return nil, fmt.Errorf("all migration files were skipped: %s", strings.Join(errorSkipped, ", "))
	}

	// Consolidate module-level removals: when all managed resources within
	// a module are being removed, replace individual removed blocks with
	// a single module-level removed block.
	allBlocks, err = e.consolidateModuleRemovals(ctx, allBlocks)
	if err != nil {
		return nil, err
	}

	e.writer.AddBlocks(allBlocks)

	outputFiles, err := e.writer.WriteAll()
	if err != nil {
		return nil, err
	}
	return &ProcessResult{OutputFiles: outputFiles, SkippedFiles: skippedFiles}, nil
}

// processMigration processes a single parsed migration file, generating
// HCL blocks for each operation. For keyed moves, it coordinates across
// operations to ensure completeness and prevent duplicate removed blocks.
// Returns the collected blocks; the caller decides when to commit them
// to the Writer.
func (e *Engine) processMigration(ctx context.Context, mf *migration.MigrationFile) ([]generator.Block, error) {
	sourceFile := mf.FilePath
	tracker := newWildcardTracker()

	var allBlocks []generator.Block
	for i, op := range mf.Operations {
		blocks, err := e.processOperation(ctx, &op, i, tracker, sourceFile)
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
func (e *Engine) processOperation(ctx context.Context, op *migration.Operation, opIndex int, tracker *wildcardTracker, sourceFile string) ([]generator.Block, error) {
	switch op.Type {
	case migration.OpMove:
		return e.processMove(ctx, op, opIndex, tracker, sourceFile)
	case migration.OpRename:
		return e.processRename(op, sourceFile)
	case migration.OpRemove:
		return e.processRemove(op, sourceFile)
	case migration.OpImport:
		return e.processImport(ctx, op, sourceFile)
	default:
		return nil, fmt.Errorf("unknown operation type %q", op.Type)
	}
}

// processMove handles move operations, iterating over each resource entry
// and generating blocks. For cross-layer moves, generates removed blocks in the
// source layer and import blocks in the destination layer. For same-layer moves
// (source_layer == destination_layer), generates moved blocks instead.
// Supports keyed moves with key matching and cross-operation completeness tracking.
func (e *Engine) processMove(ctx context.Context, op *migration.Operation, opIndex int, tracker *wildcardTracker, sourceFile string) ([]generator.Block, error) {
	srcLayer := op.SourceLayer
	dstLayer := op.DestinationLayer
	sameLayer := srcLayer == dstLayer

	if op.AllResources {
		// For all_resources, use operation-level UseMovedBlocks (no per-resource override)
		useMovedBlocks := boolPtrDefault(op.UseMovedBlocks, true)
		effectiveSameLayer := sameLayer && useMovedBlocks
		return e.processMoveAllResources(ctx, srcLayer, dstLayer, effectiveSameLayer, op.Overrides, op.Omit, op.Description, sourceFile)
	}

	srcPrefix := op.EffectiveSourcePrefix()
	dstPrefix := op.EffectiveDestinationPrefix()

	var blocks []generator.Block

	for i, res := range op.Resources {
		srcAddr := migration.FullAddress(srcPrefix, res.From)
		dstBaseAddr := res.To
		if dstBaseAddr == "" {
			dstBaseAddr = res.From
		}
		dstAddr := migration.FullAddress(dstPrefix, dstBaseAddr)

		// Resolve per-resource use_moved_blocks (resource → operation → default true)
		useMovedBlocks := res.UseMovedBlocksValue(op.UseMovedBlocks)
		effectiveSameLayer := sameLayer && useMovedBlocks

		resBlocks, err := e.processMoveResource(ctx, srcLayer, dstLayer, effectiveSameLayer, srcAddr, dstAddr, &res, opIndex, i, tracker, op.Description, sourceFile)
		if err != nil {
			return nil, fmt.Errorf("resource %q: %w", res.From, err)
		}
		blocks = append(blocks, resBlocks...)
	}

	return blocks, nil
}

// processMoveResource handles a single resource entry within a move operation.
// If the address is a module path, it discovers all resources under the module
// and generates blocks for each. If the resource has a keys map, it performs
// keyed expansion with pattern matching. Otherwise, it moves the resource as-is.
// When sameLayer is true, generates moved blocks instead of removed+import.
func (e *Engine) processMoveResource(
	ctx context.Context,
	srcLayer, dstLayer string,
	sameLayer bool,
	srcAddr, dstAddr string,
	res *migration.ResourceMove,
	opIndex, resIndex int,
	tracker *wildcardTracker,
	description string,
	sourceFile string,
) ([]generator.Block, error) {
	// Module-level move: discover all resources under the module prefix
	if migration.IsModuleAddress(srcAddr) {
		return e.processMoveModule(ctx, srcLayer, dstLayer, sameLayer, srcAddr, dstAddr, description, sourceFile)
	}

	if len(res.Keys) > 0 {
		return e.processMoveKeyed(ctx, srcLayer, dstLayer, sameLayer, srcAddr, dstAddr, res, opIndex, tracker, description, sourceFile)
	}

	return e.processMoveSimple(ctx, srcLayer, dstLayer, sameLayer, srcAddr, dstAddr, res, tracker, description, sourceFile)
}

// processMoveModule handles a module-level move by discovering all managed
// resources under the source module prefix. For cross-layer moves, generates
// removed + import blocks. For same-layer moves, generates moved blocks.
// When same-layer and srcAddr == dstAddr (no rename), this is a no-op.
//
// The existing consolidateModuleRemovals post-processing step will collapse
// the individual removed blocks into a single module-level removed block
// (cross-layer only).
func (e *Engine) processMoveModule(
	ctx context.Context,
	srcLayer, dstLayer string,
	sameLayer bool,
	srcAddr, dstAddr string,
	description string,
	sourceFile string,
) ([]generator.Block, error) {
	// Same-layer module move with no rename is a no-op
	if sameLayer && srcAddr == dstAddr {
		return nil, nil
	}

	// Same-layer module rename: emit a single module-level moved block
	// OpenTofu/Terraform supports moved { from = module.foo; to = module.bar }
	if sameLayer {
		return []generator.Block{
			&generator.MovedBlock{
				From:        srcAddr,
				To:          dstAddr,
				Layer:       srcLayer,
				Description: description,
				Source:      sourceFile,
			},
		}, nil
	}

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
			Source:      sourceFile,
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
				Source:      sourceFile,
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
// managed resources in the source layer and generating blocks for each.
// For cross-layer moves: removed + import blocks.
// For same-layer moves: moved blocks (skipping identity moves).
// Optional overrides allow renaming specific resources during the move.
func (e *Engine) processMoveAllResources(
	ctx context.Context,
	srcLayer, dstLayer string,
	sameLayer bool,
	overrides []migration.ResourceMove,
	omitEntries []migration.OmitEntry,
	description string,
	sourceFile string,
) ([]generator.Block, error) {
	resources, err := e.resolver.LookupAllManagedResources(ctx, srcLayer)
	if err != nil {
		return nil, err
	}

	// Build override map: base address → override config (destination address and/or import ID)
	type overrideConfig struct {
		destinationAddress string
		importID           string
	}
	overrideMap := make(map[string]overrideConfig)
	for _, res := range overrides {
		overrideMap[res.From] = overrideConfig{
			destinationAddress: res.To,
			importID:           res.ImportID,
		}
	}

	// Build omit map: address → destroy flag
	type omitConfig struct {
		destroy bool
	}
	omitMap := make(map[string]omitConfig)
	for _, entry := range omitEntries {
		omitMap[entry.Address] = omitConfig{destroy: entry.DestroyValue()}
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

		// Check if this resource group is omitted from import
		if cfg, isOmitted := omitMap[g.baseAddr]; isOmitted {
			if sameLayer {
				// For same-layer moves, omitted resources are just skipped (no removed block needed)
				continue
			}
			blocks = append(blocks, &generator.RemovedBlock{
				From:        g.baseAddr,
				Destroy:     cfg.destroy,
				Layer:       srcLayer,
				Description: description,
				Source:      sourceFile,
			})
			continue
		}

		// Determine destination base address (override or same)
		destBase := g.baseAddr
		overrideCfg, hasOverride := overrideMap[g.baseAddr]
		if hasOverride && overrideCfg.destinationAddress != "" {
			destBase = overrideCfg.destinationAddress
		}

		if sameLayer {
			// Same-layer: emit moved blocks per instance, skip identity moves
			for _, r := range g.resources {
				srcFullAddr := r.Address
				destAddr := destBase
				if r.Key != "" {
					destAddr = fmt.Sprintf("%s[\"%s\"]", destBase, r.Key)
				}
				// Skip identity moves
				if srcFullAddr == destAddr {
					continue
				}
				blocks = append(blocks, &generator.MovedBlock{
					From:        srcFullAddr,
					To:          destAddr,
					Layer:       srcLayer,
					Description: description,
					Source:      sourceFile,
				})
			}
		} else {
			// Cross-layer: emit removed + import blocks
			blocks = append(blocks, &generator.RemovedBlock{
				From:        g.baseAddr,
				Destroy:     false,
				Layer:       srcLayer,
				Description: description,
				Source:      sourceFile,
			})

			for _, r := range g.resources {
				importIDExpr := ""
				if hasOverride {
					importIDExpr = overrideCfg.importID
				}
				importID, err := e.resolver.ResolveImportID(r, importIDExpr)
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
					Source:      sourceFile,
				})
			}
		}
	}

	return blocks, nil
}

// processMoveSimple handles a resource move without a keys map.
// It looks up the resource in state and generates appropriate blocks:
//   - Cross-layer: removed blocks in source, import blocks in destination
//   - Same-layer: moved blocks (skipping identity moves where from == to)
func (e *Engine) processMoveSimple(
	ctx context.Context,
	srcLayer, dstLayer string,
	sameLayer bool,
	srcAddr, dstAddr string,
	res *migration.ResourceMove,
	tracker *wildcardTracker,
	description string,
	sourceFile string,
) ([]generator.Block, error) {
	// Look up all instances of this resource from state
	resources, err := e.resolver.LookupResources(ctx, srcLayer, srcAddr)
	if err != nil {
		return nil, err
	}

	var blocks []generator.Block

	// Check if this is a for_each resource (multiple instances or instances with keys)
	isForEach := len(resources) > 1 || (len(resources) == 1 && resources[0].Key != "")

	if sameLayer {
		// Same-layer: emit moved blocks, skip identity moves
		if isForEach {
			srcKey := wildcardSourceKey{layer: srcLayer, baseAddr: srcAddr}
			allKeys := make([]string, len(resources))
			for i, r := range resources {
				allKeys[i] = r.Key
			}
			tracker.setAllKeys(srcKey, allKeys)

			for _, r := range resources {
				srcFullAddr := fmt.Sprintf("%s[\"%s\"]", srcAddr, r.Key)
				destFullAddr := fmt.Sprintf("%s[\"%s\"]", dstAddr, r.Key)
				if srcFullAddr == destFullAddr {
					continue
				}
				blocks = append(blocks, &generator.MovedBlock{
					From:        srcFullAddr,
					To:          destFullAddr,
					Layer:       srcLayer,
					Description: description,
					Source:      sourceFile,
				})
			}
		} else {
			// Single resource
			if srcAddr != dstAddr {
				blocks = append(blocks, &generator.MovedBlock{
					From:        srcAddr,
					To:          dstAddr,
					Layer:       srcLayer,
					Description: description,
					Source:      sourceFile,
				})
			}
		}
	} else if isForEach {
		// Cross-layer for_each resource without keys map: move all instances with same keys
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
				Source:      sourceFile,
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
				Source:      sourceFile,
			})
		}
	} else {
		// Cross-layer single resource (non-for_each)
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
				Source:      sourceFile,
			},
			&generator.ImportBlock{
				To:          dstAddr,
				ID:          importID,
				Layer:       dstLayer,
				Description: description,
				Source:      sourceFile,
			},
		)
	}

	return blocks, nil
}

// processMoveKeyed handles a resource move with a keys map, performing pattern
// matching against state keys and generating blocks for each match.
// For cross-layer moves: import blocks in destination, removed blocks in source.
// For same-layer moves: moved blocks (skipping identity moves).
// Uses the wildcard tracker for cross-operation coordination, overlap detection,
// and completeness checking.
func (e *Engine) processMoveKeyed(
	ctx context.Context,
	srcLayer, dstLayer string,
	sameLayer bool,
	srcAddr, dstAddr string,
	res *migration.ResourceMove,
	opIndex int,
	tracker *wildcardTracker,
	description string,
	sourceFile string,
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

	// Match state keys and generate blocks
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

		// Construct full source and destination addresses
		srcFullAddr := fmt.Sprintf("%s[\"%s\"]", srcAddr, r.Key)
		destFullAddr := fmt.Sprintf("%s[\"%s\"]", dstAddr, destKey)

		if sameLayer {
			// Same-layer: check for destination-side duplicates (merge_duplicates support)
			if res.MergeDuplicates {
				// Use destFullAddr as the "import ID" — for same-layer moves there is no
				// physical import ID, so duplicates are always compatible.
				skip, err := tracker.claimDestination(srcLayer, destFullAddr, destFullAddr, opIndex, srcAddr, res.MergeDuplicates)
				if err != nil {
					return nil, fmt.Errorf("key %q: %w", r.Key, err)
				}
				if skip {
					continue
				}
			}
			// Emit moved block, skip identity moves
			if srcFullAddr != destFullAddr {
				blocks = append(blocks, &generator.MovedBlock{
					From:        srcFullAddr,
					To:          destFullAddr,
					Layer:       srcLayer,
					Description: description,
					Source:      sourceFile,
				})
			}
		} else {
			// Cross-layer: resolve import ID and emit import block
			importID, err := e.resolver.ResolveImportID(r, res.ImportID)
			if err != nil {
				return nil, fmt.Errorf("resolving import ID for key %q: %w", r.Key, err)
			}

			// Check for destination-side duplicates (merge_duplicates support)
			skip, err := tracker.claimDestination(dstLayer, destFullAddr, importID, opIndex, srcAddr, res.MergeDuplicates)
			if err != nil {
				return nil, fmt.Errorf("key %q: %w", r.Key, err)
			}
			if skip {
				continue
			}

			blocks = append(blocks, &generator.ImportBlock{
				To:          destFullAddr,
				ID:          importID,
				Layer:       dstLayer,
				Description: description,
				Source:      sourceFile,
			})
		}
	}

	// Claim matched keys in tracker (detects overlaps)
	if err := tracker.claimKeys(srcKey, claimedKeys, opIndex); err != nil {
		return nil, err
	}

	// Emit removed block for cross-layer moves (deduplicated across operations)
	if !sameLayer && tracker.shouldEmitRemoved(srcKey) {
		// Prepend removed block before import blocks
		removedBlock := &generator.RemovedBlock{
			From:        srcAddr,
			Destroy:     false,
			Layer:       srcLayer,
			Description: description,
			Source:      sourceFile,
		}
		blocks = append([]generator.Block{removedBlock}, blocks...)
	}

	return blocks, nil
}

// processRename handles rename operations, generating moved blocks for each
// rename entry within the operation.
func (e *Engine) processRename(op *migration.Operation, sourceFile string) ([]generator.Block, error) {
	var blocks []generator.Block
	for _, entry := range op.Renames {
		blocks = append(blocks, &generator.MovedBlock{
			From:        migration.FullAddress(op.AddressPrefix, entry.From),
			To:          migration.FullAddress(op.AddressPrefix, entry.To),
			Layer:       op.Layer,
			Description: op.Description,
			Source:      sourceFile,
		})
	}
	return blocks, nil
}

// processRemove handles remove operations, generating removed blocks for each
// entry in the operation.
func (e *Engine) processRemove(op *migration.Operation, sourceFile string) ([]generator.Block, error) {
	opDestroy := op.DestroyValue()
	var blocks []generator.Block
	for _, entry := range op.Entries {
		destroy := opDestroy
		if entry.Destroy != nil {
			destroy = *entry.Destroy
		}
		blocks = append(blocks, &generator.RemovedBlock{
			From:        migration.FullAddress(op.AddressPrefix, entry.Address),
			Destroy:     destroy,
			Layer:       op.Layer,
			Description: op.Description,
			Source:      sourceFile,
		})
	}
	return blocks, nil
}

// processImport handles import operations, generating import blocks for each
// import entry in the operation. Supports optional source-based state lookups
// with attribute expansion for deriving import IDs from other resources.
func (e *Engine) processImport(ctx context.Context, op *migration.Operation, sourceFile string) ([]generator.Block, error) {
	var blocks []generator.Block
	for i, entry := range op.Imports {
		provider := entry.Provider
		if provider == "" {
			provider = op.Provider
		}

		if entry.Source != nil {
			sourceBlocks, err := e.processImportFromSource(ctx, op, &entry, i, provider, sourceFile)
			if err != nil {
				return nil, err
			}
			blocks = append(blocks, sourceBlocks...)
		} else {
			blocks = append(blocks, &generator.ImportBlock{
				To:          migration.FullAddress(op.AddressPrefix, entry.Address),
				ID:          entry.ID,
				Provider:    provider,
				Layer:       op.Layer,
				Description: op.Description,
				Source:      sourceFile,
			})
		}
	}
	return blocks, nil
}

// processImportFromSource handles a single import entry that has a Source block.
// It looks up the source resource(s) in state and generates import blocks by
// evaluating template expressions against the source resource context.
// When Source.Expand is set, each list element in the named attribute produces
// a separate import block with .Item and .ItemIndex available in templates.
func (e *Engine) processImportFromSource(
	ctx context.Context,
	op *migration.Operation,
	entry *migration.ImportEntry,
	entryIndex int,
	provider string,
	sourceFile string,
) ([]generator.Block, error) {
	src := entry.Source

	// Look up all for_each instances of the source resource.
	resources, err := e.resolver.LookupResources(ctx, src.Layer, src.Address)
	if err != nil {
		return nil, fmt.Errorf("imports[%d]: looking up source %q in layer %q: %w",
			entryIndex, src.Address, src.Layer, err)
	}

	var blocks []generator.Block

	for _, res := range resources {
		if src.Expand != "" {
			expandBlocks, err := e.expandImportAttribute(op, entry, res, provider, entryIndex, sourceFile)
			if err != nil {
				return nil, err
			}
			blocks = append(blocks, expandBlocks...)
		} else {
			// No expansion — one import per source instance.
			block, err := e.buildSourcedImportBlock(op, entry, res, nil, 0, provider, sourceFile)
			if err != nil {
				return nil, fmt.Errorf("imports[%d]: source %q key %q: %w",
					entryIndex, src.Address, res.Key, err)
			}
			blocks = append(blocks, block)
		}
	}

	return blocks, nil
}

// expandImportAttribute expands a list attribute from a source resource,
// generating one import block per list element.
func (e *Engine) expandImportAttribute(
	op *migration.Operation,
	entry *migration.ImportEntry,
	res *state.ResourceInfo,
	provider string,
	entryIndex int,
	sourceFile string,
) ([]generator.Block, error) {
	src := entry.Source

	if res.Attributes == nil {
		return nil, fmt.Errorf("imports[%d]: source %q key %q has no attributes",
			entryIndex, src.Address, res.Key)
	}

	attrVal, ok := res.Attributes[src.Expand]
	if !ok {
		return nil, fmt.Errorf("imports[%d]: source %q key %q has no attribute %q",
			entryIndex, src.Address, res.Key, src.Expand)
	}

	list, ok := attrVal.([]interface{})
	if !ok {
		return nil, fmt.Errorf("imports[%d]: source %q key %q attribute %q is not a list (got %T)",
			entryIndex, src.Address, res.Key, src.Expand, attrVal)
	}

	// Empty list produces zero imports (like for_each over empty set).
	var blocks []generator.Block
	for itemIdx, item := range list {
		block, err := e.buildSourcedImportBlock(op, entry, res, item, itemIdx, provider, sourceFile)
		if err != nil {
			return nil, fmt.Errorf("imports[%d]: source %q key %q expand %q item[%d]: %w",
				entryIndex, src.Address, res.Key, src.Expand, itemIdx, err)
		}
		blocks = append(blocks, block)
	}

	return blocks, nil
}

// buildSourcedImportBlock creates a single ImportBlock by evaluating template
// expressions in the entry's ID and Key fields against the source resource context.
// When item is non-nil, .Item and .ItemIndex are set in the template context.
func (e *Engine) buildSourcedImportBlock(
	op *migration.Operation,
	entry *migration.ImportEntry,
	res *state.ResourceInfo,
	item interface{},
	itemIndex int,
	provider string,
	sourceFile string,
) (*generator.ImportBlock, error) {
	var tmplCtx *tmpl.TemplateContext
	if item != nil {
		tmplCtx = buildExpandedTemplateContext(res, item, itemIndex)
	} else {
		tmplCtx = buildTemplateContext(res)
	}

	// Evaluate the import ID template.
	importID, err := tmpl.Evaluate(entry.ID, tmplCtx)
	if err != nil {
		return nil, fmt.Errorf("evaluating id template: %w", err)
	}

	// Determine the destination address.
	destAddr := migration.FullAddress(op.AddressPrefix, entry.Address)

	// If the source is a for_each resource or we're expanding, resolve the key.
	if res.Key != "" || item != nil {
		keyStr := res.Key // default: use source key
		if entry.Key != "" {
			keyStr, err = tmpl.Evaluate(entry.Key, tmplCtx)
			if err != nil {
				return nil, fmt.Errorf("evaluating key template: %w", err)
			}
		}
		destAddr = fmt.Sprintf("%s[\"%s\"]", destAddr, keyStr)
	}

	return &generator.ImportBlock{
		To:          destAddr,
		ID:          importID,
		Provider:    provider,
		Layer:       op.Layer,
		Description: op.Description,
		Source:      sourceFile,
	}, nil
}

// evaluateCondition checks whether a migration file's preconditions are met.
// Returns (true, nil) if there is no condition or all checks pass.
// Returns (false, nil) if a check fails (logs skip reason to stderr).
// Returns (false, error) if a state read error occurs.
func (e *Engine) evaluateCondition(ctx context.Context, mf *migration.MigrationFile) (bool, error) {
	if mf.Condition == nil {
		return true, nil
	}

	// F3: Layer existence conditions — cheap checks, no state reading.
	for _, path := range mf.Condition.LayerExists {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "Skipping %q: layer %q does not exist (layer_exists condition)\n", mf.FilePath, path)
			return false, nil
		}
	}

	for _, path := range mf.Condition.LayerNotExists {
		if _, err := os.Stat(path); err == nil {
			fmt.Fprintf(os.Stderr, "Skipping %q: layer %q exists (layer_not_exists condition)\n", mf.FilePath, path)
			return false, nil
		}
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

	if len(cond.LayerExists) > 0 {
		mc.LayerExists = make([]string, len(cond.LayerExists))
		copy(mc.LayerExists, cond.LayerExists)
	}

	if len(cond.LayerNotExists) > 0 {
		mc.LayerNotExists = make([]string, len(cond.LayerNotExists))
		copy(mc.LayerNotExists, cond.LayerNotExists)
	}

	if len(mc.ResourcesExist) == 0 && len(mc.ResourcesNotExist) == 0 && len(mc.LayerExists) == 0 && len(mc.LayerNotExists) == 0 {
		return nil
	}
	return mc
}

// collectLayerPaths extracts all layer directory paths referenced by a migration
// file's operations and resource conditions. Excludes destination_layer (may not
// exist yet) and layer_exists/layer_not_exists paths (explicit conditions).
func collectLayerPaths(mf *migration.MigrationFile) []string {
	seen := make(map[string]bool)
	var paths []string
	add := func(path string) {
		if path != "" && !seen[path] {
			seen[path] = true
			paths = append(paths, path)
		}
	}

	for _, op := range mf.Operations {
		switch op.Type {
		case migration.OpMove:
			add(op.SourceLayer)
		case migration.OpRename, migration.OpRemove:
			add(op.Layer)
		case migration.OpImport:
			add(op.Layer)
			// Source-based imports may reference other layers for state lookups.
			for _, entry := range op.Imports {
				if entry.Source != nil {
					add(entry.Source.Layer)
				}
			}
		}
	}

	if mf.Condition != nil {
		for _, check := range mf.Condition.ResourcesExist {
			add(check.Layer)
		}
		for _, check := range mf.Condition.ResourcesNotExist {
			add(check.Layer)
		}
	}

	return paths
}

// checkLayerPaths checks that all given layer paths exist on disk.
// Returns the first missing path, or "" if all exist.
func checkLayerPaths(paths []string) string {
	for _, path := range paths {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return path
		}
	}
	return ""
}

// boolPtrDefault returns the value of a *bool pointer, or defaultVal if nil.
func boolPtrDefault(p *bool, defaultVal bool) bool {
	if p != nil {
		return *p
	}
	return defaultVal
}

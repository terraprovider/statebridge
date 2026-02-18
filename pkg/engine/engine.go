package engine

import (
	"context"
	"fmt"
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

	// OutputFilename overrides the default migration HCL filename.
	OutputFilename string
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
	if cfg.OutputFilename != "" {
		w.OutputFilename = cfg.OutputFilename
	}

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
func (e *Engine) ProcessFiles(ctx context.Context, paths []string) ([]string, error) {
	files, err := e.parser.ParseFiles(paths)
	if err != nil {
		return nil, fmt.Errorf("parsing migration files: %w", err)
	}

	for _, mf := range files {
		if errs := migration.Validate(mf); len(errs) > 0 {
			var msgs []string
			for _, ve := range errs {
				msgs = append(msgs, ve.Error())
			}
			return nil, fmt.Errorf("validation errors in %q:\n  %s", mf.FilePath, strings.Join(msgs, "\n  "))
		}

		if err := e.processMigration(ctx, mf); err != nil {
			return nil, fmt.Errorf("processing %q: %w", mf.FilePath, err)
		}
	}

	return e.writer.WriteAll()
}

// processMigration processes a single parsed migration file, generating
// HCL blocks for each operation. For wildcard moves with key_prefix
// filtering, it coordinates across operations to ensure completeness
// and prevent duplicate removed blocks.
func (e *Engine) processMigration(ctx context.Context, mf *migration.MigrationFile) error {
	tracker := newWildcardTracker()

	for i, op := range mf.Operations {
		blocks, err := e.processOperation(ctx, &op, i, tracker)
		if err != nil {
			return fmt.Errorf("operation[%d] (%s): %w", i, op.Type, err)
		}
		e.writer.AddBlocks(blocks)
	}

	if err := tracker.checkCompleteness(); err != nil {
		return err
	}

	return nil
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

// processMove handles move operations, which generate a removed block in the
// source layer and an import block in the destination layer.
// Supports both single-resource moves and wildcard expansion.
func (e *Engine) processMove(ctx context.Context, op *migration.Operation, opIndex int, tracker *wildcardTracker) ([]generator.Block, error) {
	srcLayer := op.Source.Layer
	srcAddr := op.Source.Address
	dstLayer := op.Destination.Layer
	dstAddr := op.Destination.Address

	if IsWildcard(srcAddr) {
		return e.processMoveWildcard(ctx, op, srcLayer, srcAddr, dstLayer, dstAddr, opIndex, tracker)
	}

	return e.processMoveSingle(ctx, op, srcLayer, srcAddr, dstLayer, dstAddr)
}

// processMoveSingle handles a single (non-wildcard) move operation.
func (e *Engine) processMoveSingle(
	ctx context.Context,
	op *migration.Operation,
	srcLayer, srcAddr, dstLayer, dstAddr string,
) ([]generator.Block, error) {
	instance, err := e.resolver.ResolveSingleMove(ctx, srcLayer, srcAddr, dstAddr, op.ImportID)
	if err != nil {
		return nil, err
	}

	return []generator.Block{
		&generator.RemovedBlock{
			From:        srcAddr,
			Destroy:     false,
			Layer:       srcLayer,
			Description: op.Description,
		},
		&generator.ImportBlock{
			To:          instance.DestAddress,
			ID:          instance.ImportID,
			Layer:       dstLayer,
			Description: op.Description,
		},
	}, nil
}

// processMoveWildcard handles a wildcard move operation, expanding matching
// instances from state and generating import blocks in the destination layer.
// It uses the tracker to coordinate with other operations targeting the same
// source: deduplicating removed blocks, tracking claimed keys, and enabling
// completeness verification for prefix-filtered moves.
func (e *Engine) processMoveWildcard(
	ctx context.Context,
	op *migration.Operation,
	srcLayer, srcAddr, dstLayer, dstAddr string,
	opIndex int,
	tracker *wildcardTracker,
) ([]generator.Block, error) {
	srcKey := wildcardSourceKey{layer: srcLayer, baseAddr: BaseAddress(srcAddr)}
	keyPrefix := op.Source.KeyPrefix

	// On first encounter of this source, load all keys for completeness tracking.
	if _, exists := tracker.groups[srcKey]; !exists {
		allKeys, err := e.resolver.LookupWildcardKeys(ctx, srcLayer, srcAddr)
		if err != nil {
			return nil, err
		}
		tracker.setAllKeys(srcKey, allKeys)
	}

	// Expand with optional prefix filter.
	instances, err := e.resolver.ExpandWildcard(ctx, srcLayer, srcAddr, dstAddr, op.ImportID, keyPrefix)
	if err != nil {
		return nil, err
	}

	// Track prefix-filtered operations for completeness checking.
	if keyPrefix != "" {
		tracker.markPrefixFiltered(srcKey)
		keys := make([]string, len(instances))
		for i, inst := range instances {
			keys[i] = inst.SourceResource.Key
		}
		if err := tracker.claimKeys(srcKey, keys, opIndex); err != nil {
			return nil, err
		}
	}

	var blocks []generator.Block

	// Only emit removed block once per source resource.
	if tracker.shouldEmitRemoved(srcKey) {
		blocks = append(blocks, &generator.RemovedBlock{
			From:        BaseAddress(srcAddr),
			Destroy:     false,
			Layer:       srcLayer,
			Description: op.Description,
		})
	}

	for _, inst := range instances {
		blocks = append(blocks, &generator.ImportBlock{
			To:          inst.DestAddress,
			ID:          inst.ImportID,
			Layer:       dstLayer,
			Description: op.Description,
		})
	}

	return blocks, nil
}

// processRename handles rename operations, which generate a moved block.
func (e *Engine) processRename(op *migration.Operation) ([]generator.Block, error) {
	return []generator.Block{
		&generator.MovedBlock{
			From:        op.From,
			To:          op.To,
			Layer:       op.Layer,
			Description: op.Description,
		},
	}, nil
}

// processRemove handles remove operations, which generate a removed block.
func (e *Engine) processRemove(op *migration.Operation) ([]generator.Block, error) {
	return []generator.Block{
		&generator.RemovedBlock{
			From:        op.Address,
			Destroy:     op.DestroyValue(),
			Layer:       op.Layer,
			Description: op.Description,
		},
	}, nil
}

// processImport handles import operations, which generate an import block.
func (e *Engine) processImport(op *migration.Operation) ([]generator.Block, error) {
	return []generator.Block{
		&generator.ImportBlock{
			To:          op.Address,
			ID:          op.ImportID,
			Provider:    op.Provider,
			Layer:       op.Layer,
			Description: op.Description,
		},
	}, nil
}

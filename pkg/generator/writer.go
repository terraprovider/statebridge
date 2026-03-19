package generator

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Writer collects HCL blocks and writes them grouped by layer and source
// migration file. Each (layer, source file) pair produces a separate output
// file with deterministic block ordering and content-addressed naming.
type Writer struct {
	blocks       []Block
	fileMetadata map[string]*MigrationMetadata

	// DryRun controls whether files are actually written to disk.
	// When true, content is generated but not written.
	DryRun bool
}

// NewWriter creates a Writer with default settings.
func NewWriter() *Writer {
	return &Writer{
		fileMetadata: make(map[string]*MigrationMetadata),
	}
}

// SetFileMetadata associates metadata (conditions) with a source
// migration file. The resources field is computed automatically at render time
// from the blocks in each group. Layer-specific condition relativization is
// also applied at render time.
func (w *Writer) SetFileMetadata(sourceFile string, meta *MigrationMetadata) {
	if meta != nil {
		w.fileMetadata[sourceFile] = meta
	}
}

// AddBlock adds a single block to the writer's collection.
func (w *Writer) AddBlock(b Block) {
	w.blocks = append(w.blocks, b)
}

// AddBlocks adds multiple blocks at once.
func (w *Writer) AddBlocks(blocks []Block) {
	w.blocks = append(w.blocks, blocks...)
}

// HasBlocks reports whether any blocks have been added to the writer.
func (w *Writer) HasBlocks() bool {
	return len(w.blocks) > 0
}

// groupKey uniquely identifies a (layer, source file) pair.
type groupKey struct {
	Layer      string
	SourceFile string
}

// groupedByLayerAndSource returns blocks grouped by (layer, source file).
func (w *Writer) groupedByLayerAndSource() map[groupKey][]Block {
	grouped := make(map[groupKey][]Block)
	for _, b := range w.blocks {
		key := groupKey{Layer: b.LayerPath(), SourceFile: b.SourceFile()}
		grouped[key] = append(grouped[key], b)
	}
	return grouped
}

// sortedGroupKeys returns group keys in deterministic order:
// sorted by layer path first, then by source file path.
func sortedGroupKeys(groups map[groupKey][]Block) []groupKey {
	keys := make([]groupKey, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].Layer != keys[j].Layer {
			return keys[i].Layer < keys[j].Layer
		}
		return keys[i].SourceFile < keys[j].SourceFile
	})
	return keys
}

// RenderAll generates the full HCL content for all (layer, source file) groups.
// Returns a map of output file path to rendered content.
func (w *Writer) RenderAll() map[string]string {
	result := make(map[string]string)
	groups := w.groupedByLayerAndSource()

	for _, key := range sortedGroupKeys(groups) {
		blocks := groups[key]
		SortBlocks(blocks)
		meta := w.buildGroupMetadata(key, blocks)
		content := renderBlocks(blocks, meta)
		outPath := filepath.Join(key.Layer, outputFilename(key.SourceFile, content))
		result[outPath] = content
	}
	return result
}

// WriteAll writes all collected blocks to their respective layer directories.
// Each (layer, source migration file) pair gets its own output file with
// deterministic block ordering and a content-addressed filename.
// Before writing, old versions of the same migration stem are removed so that
// stale files do not interfere with tofu plan.
// Returns the sorted list of file paths written and any error encountered.
func (w *Writer) WriteAll() ([]string, error) {
	var written []string
	groups := w.groupedByLayerAndSource()

	for _, key := range sortedGroupKeys(groups) {
		blocks := groups[key]
		SortBlocks(blocks)
		meta := w.buildGroupMetadata(key, blocks)
		content := renderBlocks(blocks, meta)
		filename := outputFilename(key.SourceFile, content)
		outPath := filepath.Join(key.Layer, filename)

		// Clean up old versions of the same migration stem
		if err := w.cleanupOldVersions(key.Layer, filename); err != nil {
			return written, fmt.Errorf("cleaning old versions for %q: %w", filename, err)
		}

		if w.DryRun {
			written = append(written, outPath)
			continue
		}

		if err := os.WriteFile(outPath, []byte(content), 0o644); err != nil {
			return written, fmt.Errorf("writing migration file %q: %w", outPath, err)
		}
		written = append(written, outPath)
	}

	return written, nil
}

// cleanupOldVersions removes migration files in layerDir that share the same
// YAML stem as filename but have a different content hash.
// For example, if filename is "migration.001_move.a1b2c3d4.tf", any existing
// "migration.001_move.*.tf" files with a different hash are deleted.
func (w *Writer) cleanupOldVersions(layerDir, filename string) error {
	// Extract the YAML stem from "migration.<stem>.<hash>.tf"
	inner := strings.TrimPrefix(strings.TrimSuffix(filename, ".tf"), "migration.")
	lastDot := strings.LastIndex(inner, ".")
	if lastDot <= 0 {
		return nil // unexpected format, skip cleanup
	}
	stem := inner[:lastDot]

	pattern := filepath.Join(layerDir, fmt.Sprintf("migration.%s.*.tf", stem))
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return fmt.Errorf("globbing old versions: %w", err)
	}

	for _, match := range matches {
		if filepath.Base(match) == filename {
			continue // same file, keep it
		}
		if w.DryRun {
			fmt.Fprintf(os.Stderr, "Would remove old version: %s\n", match)
			continue
		}
		if err := os.Remove(match); err != nil {
			return fmt.Errorf("removing old version %q: %w", match, err)
		}
		fmt.Fprintf(os.Stderr, "Removed old version: %s\n", match)
	}

	return nil
}

// buildGroupMetadata constructs complete metadata for a (layer, sourceFile) group.
// It infers conditions from block types, merges with any explicit conditions from
// the YAML, computes resource addresses from blocks, and relativizes layer paths.
func (w *Writer) buildGroupMetadata(key groupKey, blocks []Block) *MigrationMetadata {
	stored := w.fileMetadata[key.SourceFile]

	resources := ExtractResourceAddresses(blocks)

	// If there's no stored metadata and no resources, skip metadata entirely
	if stored == nil && len(resources) == 0 {
		return nil
	}

	meta := &MigrationMetadata{
		Resources: resources,
	}

	// Infer conditions from block types and merge with explicit YAML conditions
	inferred := InferConditions(blocks)
	var explicit *MetadataCondition
	if stored != nil {
		explicit = RelativizeCondition(stored.Conditions, key.Layer)
	}
	meta.Conditions = MergeConditions(inferred, explicit)

	return meta
}

// outputFilename generates the content-addressed output filename:
// migration.<yaml_stem>.<sha256_8hex>.tf
func outputFilename(sourceFile, content string) string {
	stem := yamlStem(sourceFile)
	hash := sha256.Sum256([]byte(content))
	return fmt.Sprintf("migration.%s.%x.tf", stem, hash[:4])
}

// yamlStem extracts the filename without extension from a path.
// e.g., "/path/to/0001_move.yaml" → "0001_move"
func yamlStem(path string) string {
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	if ext != "" {
		base = strings.TrimSuffix(base, ext)
	}
	return base
}

// renderBlocks generates HCL content from a sorted list of blocks,
// including a header, optional metadata comment, and per-block comments.
func renderBlocks(blocks []Block, meta *MigrationMetadata) string {
	var sb strings.Builder

	sb.WriteString("# Generated by statebridge - do not edit manually\n")

	if metaComment := RenderMetadataComment(meta); metaComment != "" {
		sb.WriteString(metaComment)
	}

	for _, b := range blocks {
		sb.WriteString("\n")
		if comment := b.Comment(); comment != "" {
			fmt.Fprintf(&sb, "# %s\n", comment)
		}
		sb.WriteString(b.Render())
		sb.WriteString("\n")
	}

	return sb.String()
}

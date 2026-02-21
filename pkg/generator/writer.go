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

// SetFileMetadata associates metadata (conditions, init args) with a source
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
// Returns the sorted list of file paths written and any error encountered.
func (w *Writer) WriteAll() ([]string, error) {
	var written []string
	groups := w.groupedByLayerAndSource()

	for _, key := range sortedGroupKeys(groups) {
		blocks := groups[key]
		SortBlocks(blocks)
		meta := w.buildGroupMetadata(key, blocks)
		content := renderBlocks(blocks, meta)
		outPath := filepath.Join(key.Layer, outputFilename(key.SourceFile, content))

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

// buildGroupMetadata constructs complete metadata for a (layer, sourceFile) group.
// It looks up stored metadata by sourceFile, computes resource addresses from blocks,
// and relativizes condition layer paths.
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

	if stored != nil {
		meta.InitArgs = stored.InitArgs
		meta.Conditions = RelativizeCondition(stored.Conditions, key.Layer)
	}

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

	sb.WriteString("# Generated by tfmigrate - do not edit manually\n")

	if metaComment := RenderMetadataComment(meta); metaComment != "" {
		sb.WriteString(metaComment)
	}

	for _, b := range blocks {
		sb.WriteString("\n")
		if comment := b.Comment(); comment != "" {
			sb.WriteString(fmt.Sprintf("# %s\n", comment))
		}
		sb.WriteString(b.Render())
		sb.WriteString("\n")
	}

	return sb.String()
}

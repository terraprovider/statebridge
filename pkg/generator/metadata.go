package generator

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// MigrationMetadata holds structured information embedded in generated .tf files.
// It carries conditions and resource addresses needed by the download command
// to evaluate applicability and by plan for targeting.
type MigrationMetadata struct {
	Conditions *MetadataCondition `json:"conditions,omitempty"`
	Resources  []string           `json:"resources"`
}

// MetadataCondition mirrors migration.Condition with JSON tags for embedding
// in generated .tf file metadata comments.
type MetadataCondition struct {
	ResourcesExist    []MetadataResourceCheck `json:"resources_exist,omitempty"`
	ResourcesNotExist []MetadataResourceCheck `json:"resources_not_exist,omitempty"`
	LayerExists       []string                `json:"layer_exists,omitempty"`
	LayerNotExists    []string                `json:"layer_not_exists,omitempty"`
}

// MetadataResourceCheck mirrors migration.ResourceCheck with JSON tags.
type MetadataResourceCheck struct {
	Layer     string   `json:"layer"`
	Addresses []string `json:"addresses"`
}

const (
	metadataBeginMarker = "# tfmigrate:metadata:begin"
	metadataEndMarker   = "# tfmigrate:metadata:end"
)

// RenderMetadataComment serializes metadata as a commented JSON block
// with tfmigrate:metadata:begin/end delimiters.
func RenderMetadataComment(meta *MigrationMetadata) string {
	if meta == nil {
		return ""
	}

	data, err := json.Marshal(meta)
	if err != nil {
		// MigrationMetadata contains only basic types, so Marshal should
		// never fail. If it does, surface the error rather than silently
		// dropping metadata.
		fmt.Fprintf(os.Stderr, "BUG: failed to marshal migration metadata: %v\n", err)
		return ""
	}

	var sb strings.Builder
	sb.WriteString("#\n")
	sb.WriteString(metadataBeginMarker + "\n")
	sb.WriteString("# " + string(data) + "\n")
	sb.WriteString(metadataEndMarker + "\n")
	return sb.String()
}

// ParseMetadataComment extracts MigrationMetadata from a .tf file's content.
// Returns nil (not error) if no metadata block is found.
func ParseMetadataComment(content string) (*MigrationMetadata, error) {
	beginIdx := strings.Index(content, metadataBeginMarker)
	if beginIdx < 0 {
		return nil, nil
	}
	endIdx := strings.Index(content, metadataEndMarker)
	if endIdx < 0 || endIdx <= beginIdx {
		return nil, fmt.Errorf("found metadata begin marker but no end marker")
	}

	// Extract the JSON line between begin and end markers
	between := content[beginIdx+len(metadataBeginMarker) : endIdx]
	between = strings.TrimSpace(between)

	// Remove the leading "# " comment prefix
	between = strings.TrimPrefix(between, "# ")
	between = strings.TrimSpace(between)

	if between == "" {
		return nil, fmt.Errorf("empty metadata block")
	}

	var meta MigrationMetadata
	if err := json.Unmarshal([]byte(between), &meta); err != nil {
		return nil, fmt.Errorf("parsing metadata JSON: %w", err)
	}

	return &meta, nil
}

// ExtractResourceAddresses collects all resource addresses from a list of Blocks.
// Deduplicates and sorts for deterministic output.
func ExtractResourceAddresses(blocks []Block) []string {
	seen := make(map[string]bool)

	for _, b := range blocks {
		switch blk := b.(type) {
		case *ImportBlock:
			seen[blk.To] = true
		case *MovedBlock:
			seen[blk.From] = true
			seen[blk.To] = true
		case *RemovedBlock:
			seen[blk.From] = true
		}
	}

	addrs := make([]string, 0, len(seen))
	for addr := range seen {
		addrs = append(addrs, addr)
	}
	sort.Strings(addrs)
	return addrs
}

// RelativizeCondition returns a deep copy of the condition with any layer
// path matching layerPath replaced by ".". This makes conditions portable
// when the .tf file is downloaded into the layer directory.
func RelativizeCondition(cond *MetadataCondition, layerPath string) *MetadataCondition {
	if cond == nil {
		return nil
	}

	result := &MetadataCondition{}
	for _, check := range cond.ResourcesExist {
		layer := check.Layer
		if pathsEqual(layer, layerPath) {
			layer = "."
		}
		addrsCopy := make([]string, len(check.Addresses))
		copy(addrsCopy, check.Addresses)
		result.ResourcesExist = append(result.ResourcesExist, MetadataResourceCheck{
			Layer:     layer,
			Addresses: addrsCopy,
		})
	}
	for _, check := range cond.ResourcesNotExist {
		layer := check.Layer
		if pathsEqual(layer, layerPath) {
			layer = "."
		}
		addrsCopy := make([]string, len(check.Addresses))
		copy(addrsCopy, check.Addresses)
		result.ResourcesNotExist = append(result.ResourcesNotExist, MetadataResourceCheck{
			Layer:     layer,
			Addresses: addrsCopy,
		})
	}

	// Layer existence conditions are copied through unchanged — they reference
	// directory paths, not state-relative resources.
	if len(cond.LayerExists) > 0 {
		result.LayerExists = make([]string, len(cond.LayerExists))
		copy(result.LayerExists, cond.LayerExists)
	}
	if len(cond.LayerNotExists) > 0 {
		result.LayerNotExists = make([]string, len(cond.LayerNotExists))
		copy(result.LayerNotExists, cond.LayerNotExists)
	}

	if len(result.ResourcesExist) == 0 && len(result.ResourcesNotExist) == 0 &&
		len(result.LayerExists) == 0 && len(result.LayerNotExists) == 0 {
		return nil
	}
	return result
}

// InferConditions derives download-time conditions from the block types in a
// group. The rules are:
//   - RemovedBlock: resources_exist for From (skip if already removed)
//   - ImportBlock:  resources_not_exist for To (skip if already imported)
//   - MovedBlock:   resources_exist for From AND resources_not_exist for To
//
// All conditions use layer "." (the owning layer at download time).
// Returns nil if blocks is empty.
func InferConditions(blocks []Block) *MetadataCondition {
	if len(blocks) == 0 {
		return nil
	}

	existSet := make(map[string]bool)
	notExistSet := make(map[string]bool)

	for _, b := range blocks {
		switch blk := b.(type) {
		case *RemovedBlock:
			existSet[blk.From] = true
		case *ImportBlock:
			notExistSet[blk.To] = true
		case *MovedBlock:
			existSet[blk.From] = true
			notExistSet[blk.To] = true
		}
	}

	var result MetadataCondition

	if len(existSet) > 0 {
		result.ResourcesExist = []MetadataResourceCheck{{
			Layer:     ".",
			Addresses: sortedKeys(existSet),
		}}
	}
	if len(notExistSet) > 0 {
		result.ResourcesNotExist = []MetadataResourceCheck{{
			Layer:     ".",
			Addresses: sortedKeys(notExistSet),
		}}
	}

	if len(result.ResourcesExist) == 0 && len(result.ResourcesNotExist) == 0 {
		return nil
	}
	return &result
}

// MergeConditions combines two MetadataCondition values, grouping checks by
// layer and deduplicating addresses. Returns nil if both inputs are nil or
// the result is empty.
func MergeConditions(a, b *MetadataCondition) *MetadataCondition {
	if a == nil && b == nil {
		return nil
	}
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}

	existByLayer := make(map[string]map[string]bool)
	notExistByLayer := make(map[string]map[string]bool)

	collectChecks := func(checks []MetadataResourceCheck, target map[string]map[string]bool) {
		for _, check := range checks {
			if target[check.Layer] == nil {
				target[check.Layer] = make(map[string]bool)
			}
			for _, addr := range check.Addresses {
				target[check.Layer][addr] = true
			}
		}
	}

	collectChecks(a.ResourcesExist, existByLayer)
	collectChecks(b.ResourcesExist, existByLayer)
	collectChecks(a.ResourcesNotExist, notExistByLayer)
	collectChecks(b.ResourcesNotExist, notExistByLayer)

	// Merge layer existence conditions (deduplicate)
	layerExistSet := make(map[string]bool)
	layerNotExistSet := make(map[string]bool)
	for _, path := range a.LayerExists {
		layerExistSet[path] = true
	}
	for _, path := range b.LayerExists {
		layerExistSet[path] = true
	}
	for _, path := range a.LayerNotExists {
		layerNotExistSet[path] = true
	}
	for _, path := range b.LayerNotExists {
		layerNotExistSet[path] = true
	}

	result := &MetadataCondition{}

	for _, layer := range sortedKeys(existByLayer) {
		result.ResourcesExist = append(result.ResourcesExist, MetadataResourceCheck{
			Layer:     layer,
			Addresses: sortedKeys(existByLayer[layer]),
		})
	}
	for _, layer := range sortedKeys(notExistByLayer) {
		result.ResourcesNotExist = append(result.ResourcesNotExist, MetadataResourceCheck{
			Layer:     layer,
			Addresses: sortedKeys(notExistByLayer[layer]),
		})
	}
	if len(layerExistSet) > 0 {
		result.LayerExists = sortedKeys(layerExistSet)
	}
	if len(layerNotExistSet) > 0 {
		result.LayerNotExists = sortedKeys(layerNotExistSet)
	}

	if len(result.ResourcesExist) == 0 && len(result.ResourcesNotExist) == 0 &&
		len(result.LayerExists) == 0 && len(result.LayerNotExists) == 0 {
		return nil
	}
	return result
}

// sortedKeys returns the keys of a map[string]bool in sorted order.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// pathsEqual compares two filesystem paths after cleaning.
func pathsEqual(a, b string) bool {
	cleanA := filepath.Clean(filepath.FromSlash(a))
	cleanB := filepath.Clean(filepath.FromSlash(b))
	return cleanA == cleanB
}

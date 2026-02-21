package generator

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// MigrationMetadata holds structured information embedded in generated .tf files.
// It carries conditions and resource addresses needed by the download command
// to evaluate applicability and by plan/apply for targeting.
type MigrationMetadata struct {
	Conditions *MetadataCondition `json:"conditions,omitempty"`
	Resources  []string           `json:"resources"`
}

// MetadataCondition mirrors migration.Condition with JSON tags for embedding
// in generated .tf file metadata comments.
type MetadataCondition struct {
	ResourcesExist    []MetadataResourceCheck `json:"resources_exist,omitempty"`
	ResourcesNotExist []MetadataResourceCheck `json:"resources_not_exist,omitempty"`
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
		// Should not happen with well-formed metadata
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

	if len(result.ResourcesExist) == 0 && len(result.ResourcesNotExist) == 0 {
		return nil
	}
	return result
}

// pathsEqual compares two filesystem paths after cleaning.
func pathsEqual(a, b string) bool {
	return strings.TrimRight(a, "/") == strings.TrimRight(b, "/")
}

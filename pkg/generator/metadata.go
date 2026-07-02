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
	// Version is the metadata schema version. Absent/0 indicates a legacy
	// (v1) file written before versioning; CurrentMetadataVersion is written
	// for all newly generated files.
	Version int `json:"version,omitempty"`

	// SourceLayer identifies the layer (by its Terraform backend coordinates)
	// that a migration file belongs to. It is used to scope blob operations —
	// download, upload guard/cleanup, and prune — when multiple layers share a
	// single storage container. Absent for legacy files or when the backend
	// coordinates could not be discovered at generate time.
	SourceLayer *MetadataSourceLayer `json:"source_layer,omitempty"`

	Conditions *MetadataCondition `json:"conditions,omitempty"`
	Resources  []string           `json:"resources"`
}

// MetadataSourceLayer records the Azure Blob Storage backend coordinates of the
// layer that owns a migration file. In a shared-container setup these coordinates
// (specifically the state Key) distinguish one layer's migrations from another's.
type MetadataSourceLayer struct {
	StorageAccountName string `json:"storage_account_name,omitempty"`
	ContainerName      string `json:"container_name,omitempty"`
	Key                string `json:"key,omitempty"`
}

// CurrentMetadataVersion is the metadata schema version written to newly
// generated migration files. Version 2 introduced the source_layer field for
// shared-container scoping; version 1 (absent) files predate it.
const CurrentMetadataVersion = 2

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
	metadataBeginMarker = "# statebridge:metadata:begin"
	metadataEndMarker   = "# statebridge:metadata:end"
)

// RenderMetadataComment serializes metadata as a commented JSON block
// with statebridge:metadata:begin/end delimiters.
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

// Matches reports whether this source-layer descriptor refers to the layer with
// the given backend coordinates.
//
//   - match is true only when every field recorded in the descriptor equals the
//     corresponding argument. Empty descriptor fields act as wildcards (they do
//     not constrain the comparison).
//   - determinable is false when the descriptor records a Key but the caller
//     could not supply one (currentKey == ""). In that case ownership cannot be
//     decided and callers should fall back to condition-based behaviour. It is
//     always true for a nil descriptor (a legacy file trivially "matches").
//
// A nil receiver returns (true, true): legacy files have no source-layer scope
// and are treated as applicable everywhere.
func (s *MetadataSourceLayer) Matches(account, container, key string) (match bool, determinable bool) {
	if s == nil {
		return true, true
	}

	if s.Key != "" && key == "" {
		// The descriptor is keyed but the caller cannot determine its own key,
		// so ownership is undecidable.
		return false, false
	}

	if s.StorageAccountName != "" && s.StorageAccountName != account {
		return false, true
	}
	if s.ContainerName != "" && s.ContainerName != container {
		return false, true
	}
	if s.Key != "" && s.Key != key {
		return false, true
	}
	return true, true
}

// OwnedByOther reports whether this descriptor provably identifies a layer other
// than the one with the given backend coordinates. It is true only when
// ownership is determinable and does not match — i.e. the blob demonstrably
// belongs to a different layer and must not be acted upon.
//
// A nil receiver (legacy file) and the indeterminate case both return false, so
// callers keep their existing behaviour for those blobs.
func (s *MetadataSourceLayer) OwnedByOther(account, container, key string) bool {
	match, determinable := s.Matches(account, container, key)
	return determinable && !match
}

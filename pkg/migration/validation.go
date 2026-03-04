package migration

import (
	"fmt"
	"strings"
)

// ValidationError describes a specific validation failure in a migration file.
type ValidationError struct {
	// OperationIndex is the zero-based index of the operation that failed validation.
	// A value of -1 indicates a file-level validation error.
	OperationIndex int

	// Field is the name of the field that failed validation.
	Field string

	// Message describes what is wrong.
	Message string
}

// Error implements the error interface.
func (e ValidationError) Error() string {
	if e.OperationIndex < 0 {
		return fmt.Sprintf("validation error: %s: %s", e.Field, e.Message)
	}
	return fmt.Sprintf("validation error in operation[%d]: %s: %s", e.OperationIndex, e.Field, e.Message)
}

// Validate checks the structural correctness of a MigrationFile.
// It collects all validation errors rather than failing on the first one,
// enabling users to fix all problems in a single pass.
func Validate(mf *MigrationFile) []ValidationError {
	// Check status first — retired files skip all other validation.
	if mf.Status == StatusRetired {
		return nil
	}
	if mf.Status != StatusActive {
		return []ValidationError{{
			OperationIndex: -1,
			Field:          "status",
			Message:        fmt.Sprintf("unknown status %q (valid: retired)", mf.Status),
		}}
	}

	var errs []ValidationError

	if mf.Description == "" {
		errs = append(errs, ValidationError{
			OperationIndex: -1,
			Field:          "description",
			Message:        "migration file must have a description",
		})
	}

	if mf.Condition != nil {
		errs = append(errs, validateCondition(mf.Condition)...)
	}

	if len(mf.Operations) == 0 {
		errs = append(errs, ValidationError{
			OperationIndex: -1,
			Field:          "operations",
			Message:        "migration file must have at least one operation",
		})
		return errs
	}

	for i, op := range mf.Operations {
		errs = append(errs, validateOperation(i, &op)...)
	}

	return errs
}

// validateOperation validates a single operation based on its type.
func validateOperation(index int, op *Operation) []ValidationError {
	var errs []ValidationError

	// all_resources is only valid on move operations
	if op.AllResources && op.Type != OpMove {
		errs = append(errs, ValidationError{
			OperationIndex: index,
			Field:          "all_resources",
			Message:        "all_resources is only valid for move operations",
		})
	}

	switch op.Type {
	case OpMove:
		errs = append(errs, validateMove(index, op)...)
	case OpRename:
		errs = append(errs, validateRename(index, op)...)
	case OpRemove:
		errs = append(errs, validateRemove(index, op)...)
	case OpImport:
		errs = append(errs, validateImport(index, op)...)
	case "":
		errs = append(errs, ValidationError{
			OperationIndex: index,
			Field:          "type",
			Message:        "operation type is required",
		})
	default:
		errs = append(errs, ValidationError{
			OperationIndex: index,
			Field:          "type",
			Message:        fmt.Sprintf("unknown operation type %q (valid: move, rename, remove, import)", op.Type),
		})
	}

	return errs
}

// validateMove checks that a move operation has all required fields.
func validateMove(index int, op *Operation) []ValidationError {
	var errs []ValidationError

	if op.SourceLayer == "" {
		errs = append(errs, ValidationError{
			OperationIndex: index,
			Field:          "source_layer",
			Message:        "move operation requires a source_layer",
		})
	}
	if op.DestinationLayer == "" {
		errs = append(errs, ValidationError{
			OperationIndex: index,
			Field:          "destination_layer",
			Message:        "move operation requires a destination_layer",
		})
	}

	// address_prefix cannot coexist with source_prefix or destination_prefix
	if op.AddressPrefix != "" && (op.SourcePrefix != "" || op.DestinationPrefix != "") {
		errs = append(errs, ValidationError{
			OperationIndex: index,
			Field:          "address_prefix",
			Message:        "address_prefix cannot be combined with source_prefix or destination_prefix; use source_prefix/destination_prefix instead",
		})
	}

	hasAnyPrefix := op.AddressPrefix != "" || op.SourcePrefix != "" || op.DestinationPrefix != ""

	if op.AllResources {
		if hasAnyPrefix {
			errs = append(errs, ValidationError{
				OperationIndex: index,
				Field:          "address_prefix",
				Message:        "address_prefix/source_prefix/destination_prefix cannot be used with all_resources",
			})
		}
		for i, res := range op.Overrides {
			errs = append(errs, validateAllResourcesOverride(index, i, &res)...)
		}
		// Validate omit entries
		omitAddresses := make(map[string]bool)
		for i, entry := range op.Omit {
			fieldPrefix := fmt.Sprintf("omit[%d]", i)
			if entry.Address == "" {
				errs = append(errs, ValidationError{
					OperationIndex: index,
					Field:          fieldPrefix + ".address",
					Message:        "omit entry requires an address",
				})
			}
			omitAddresses[entry.Address] = true
		}
		// Check for overlap between omit and overrides
		for i, res := range op.Overrides {
			if omitAddresses[res.From] {
				errs = append(errs, ValidationError{
					OperationIndex: index,
					Field:          fmt.Sprintf("overrides[%d].from", i),
					Message:        fmt.Sprintf("address %q appears in both overrides and omit", res.From),
				})
			}
		}

		// Check for duplicate 'from' addresses in overrides.
		errs = append(errs, checkDuplicates(index, "overrides", op.Overrides, func(r ResourceMove) string { return r.From })...)
		// Check for duplicate addresses in omit.
		errs = append(errs, checkDuplicates(index, "omit", op.Omit, func(e OmitEntry) string { return e.Address })...)
	} else if len(op.Resources) == 0 {
		errs = append(errs, ValidationError{
			OperationIndex: index,
			Field:          "resources",
			Message:        "move operation requires at least one resource (or set all_resources: true)",
		})
	}

	if len(op.Omit) > 0 && !op.AllResources {
		errs = append(errs, ValidationError{
			OperationIndex: index,
			Field:          "omit",
			Message:        "omit is only valid when all_resources is true",
		})
	}

	if len(op.Overrides) > 0 && !op.AllResources {
		errs = append(errs, ValidationError{
			OperationIndex: index,
			Field:          "overrides",
			Message:        "overrides is only valid when all_resources is true",
		})
	}

	for i, res := range op.Resources {
		errs = append(errs, validateResourceMove(index, i, &res)...)
	}

	// Check for duplicate 'from' addresses in resources.
	errs = append(errs, checkDuplicates(index, "resources", op.Resources, func(r ResourceMove) string { return r.From })...)

	return errs
}

// validateResourceMove checks a single ResourceMove entry within a move operation.
func validateResourceMove(opIndex, resIndex int, res *ResourceMove) []ValidationError {
	var errs []ValidationError
	fieldPrefix := fmt.Sprintf("resources[%d]", resIndex)

	if res.From == "" {
		errs = append(errs, ValidationError{
			OperationIndex: opIndex,
			Field:          fieldPrefix + ".from",
			Message:        "resource 'from' address is required",
		})
	}

	// merge_duplicates requires keys to be present
	if res.MergeDuplicates && len(res.Keys) == 0 {
		errs = append(errs, ValidationError{
			OperationIndex: opIndex,
			Field:          fieldPrefix + ".merge_duplicates",
			Message:        "merge_duplicates is only valid when keys is present",
		})
	}

	// Validate module-level move constraints
	if res.From != "" && IsModuleAddress(res.From) {
		if len(res.Keys) > 0 {
			errs = append(errs, ValidationError{
				OperationIndex: opIndex,
				Field:          fieldPrefix + ".keys",
				Message:        "keys are not supported for module-level moves",
			})
		}
		if res.ImportID != "" {
			errs = append(errs, ValidationError{
				OperationIndex: opIndex,
				Field:          fieldPrefix + ".import_id",
				Message:        "import_id is not supported for module-level moves (auto-resolved from state)",
			})
		}
		if res.MergeDuplicates {
			errs = append(errs, ValidationError{
				OperationIndex: opIndex,
				Field:          fieldPrefix + ".merge_duplicates",
				Message:        "merge_duplicates is not supported for module-level moves",
			})
		}
		if res.To != "" && !IsModuleAddress(res.To) {
			errs = append(errs, ValidationError{
				OperationIndex: opIndex,
				Field:          fieldPrefix + ".to",
				Message:        "'to' for a module move must also be a module address",
			})
		}
	}

	// Validate keys map entries
	for pattern := range res.Keys {
		if pattern == "*" {
			continue // catch-all is valid
		}
		if strings.Contains(pattern, "*") {
			// * must only appear at the end
			if !strings.HasSuffix(pattern, "*") || strings.Count(pattern, "*") > 1 {
				errs = append(errs, ValidationError{
					OperationIndex: opIndex,
					Field:          fieldPrefix + ".keys",
					Message:        fmt.Sprintf("invalid key pattern %q: wildcard (*) is only allowed at the end of a pattern", pattern),
				})
			}
		}
	}

	return errs
}

// validateAllResourcesOverride checks a ResourceMove entry used as an override
// alongside all_resources: true. Overrides can specify a 'to' address to rename
// a resource during a bulk move and/or an import_id to override automatic import
// ID resolution for specific resources.
func validateAllResourcesOverride(opIndex, resIndex int, res *ResourceMove) []ValidationError {
	var errs []ValidationError
	fieldPrefix := fmt.Sprintf("overrides[%d]", resIndex)

	if len(res.Keys) > 0 {
		errs = append(errs, ValidationError{
			OperationIndex: opIndex,
			Field:          fieldPrefix + ".keys",
			Message:        "keys cannot be used with all_resources overrides",
		})
	}
	if res.MergeDuplicates {
		errs = append(errs, ValidationError{
			OperationIndex: opIndex,
			Field:          fieldPrefix + ".merge_duplicates",
			Message:        "merge_duplicates cannot be used with all_resources overrides",
		})
	}
	if res.To == "" && res.ImportID == "" {
		errs = append(errs, ValidationError{
			OperationIndex: opIndex,
			Field:          fieldPrefix,
			Message:        "override entry requires 'to' or 'import_id' (otherwise the entry has no effect)",
		})
	}
	if res.From != "" && IsModuleAddress(res.From) {
		errs = append(errs, ValidationError{
			OperationIndex: opIndex,
			Field:          fieldPrefix + ".from",
			Message:        "module addresses cannot be used as overrides with all_resources",
		})
	}

	return errs
}

// validateRename checks that a rename operation has all required fields.
func validateRename(index int, op *Operation) []ValidationError {
	var errs []ValidationError

	// source_prefix and destination_prefix are only valid for move operations
	if op.SourcePrefix != "" {
		errs = append(errs, ValidationError{
			OperationIndex: index,
			Field:          "source_prefix",
			Message:        "source_prefix is only valid for move operations",
		})
	}
	if op.DestinationPrefix != "" {
		errs = append(errs, ValidationError{
			OperationIndex: index,
			Field:          "destination_prefix",
			Message:        "destination_prefix is only valid for move operations",
		})
	}

	if op.Layer == "" {
		errs = append(errs, ValidationError{
			OperationIndex: index,
			Field:          "layer",
			Message:        "rename operation requires a layer path",
		})
	}
	if len(op.Renames) == 0 {
		errs = append(errs, ValidationError{
			OperationIndex: index,
			Field:          "renames",
			Message:        "rename operation requires at least one rename entry",
		})
	}
	for i, entry := range op.Renames {
		fieldPrefix := fmt.Sprintf("renames[%d]", i)
		if entry.From == "" {
			errs = append(errs, ValidationError{
				OperationIndex: index,
				Field:          fieldPrefix + ".from",
				Message:        "rename entry requires a 'from' address",
			})
		}
		if entry.To == "" {
			errs = append(errs, ValidationError{
				OperationIndex: index,
				Field:          fieldPrefix + ".to",
				Message:        "rename entry requires a 'to' address",
			})
		}
	}

	// Check for duplicate 'from' addresses in renames.
	errs = append(errs, checkDuplicates(index, "renames", op.Renames, func(e RenameEntry) string { return e.From })...)

	return errs
}

// validateRemove checks that a remove operation has all required fields.
func validateRemove(index int, op *Operation) []ValidationError {
	var errs []ValidationError

	// source_prefix and destination_prefix are only valid for move operations
	if op.SourcePrefix != "" {
		errs = append(errs, ValidationError{
			OperationIndex: index,
			Field:          "source_prefix",
			Message:        "source_prefix is only valid for move operations",
		})
	}
	if op.DestinationPrefix != "" {
		errs = append(errs, ValidationError{
			OperationIndex: index,
			Field:          "destination_prefix",
			Message:        "destination_prefix is only valid for move operations",
		})
	}

	if op.Layer == "" {
		errs = append(errs, ValidationError{
			OperationIndex: index,
			Field:          "layer",
			Message:        "remove operation requires a layer path",
		})
	}
	if len(op.Entries) == 0 {
		errs = append(errs, ValidationError{
			OperationIndex: index,
			Field:          "entries",
			Message:        "remove operation requires at least one entry",
		})
	}
	for i, entry := range op.Entries {
		if entry.Address == "" {
			errs = append(errs, ValidationError{
				OperationIndex: index,
				Field:          fmt.Sprintf("entries[%d].address", i),
				Message:        "remove entry requires an address",
			})
		}
	}

	// Check for duplicate addresses in entries.
	errs = append(errs, checkDuplicates(index, "entries", op.Entries, func(e RemoveEntry) string { return e.Address })...)

	return errs
}

// validateImport checks that an import operation has all required fields.
func validateImport(index int, op *Operation) []ValidationError {
	var errs []ValidationError

	// source_prefix and destination_prefix are only valid for move operations
	if op.SourcePrefix != "" {
		errs = append(errs, ValidationError{
			OperationIndex: index,
			Field:          "source_prefix",
			Message:        "source_prefix is only valid for move operations",
		})
	}
	if op.DestinationPrefix != "" {
		errs = append(errs, ValidationError{
			OperationIndex: index,
			Field:          "destination_prefix",
			Message:        "destination_prefix is only valid for move operations",
		})
	}

	if op.Layer == "" {
		errs = append(errs, ValidationError{
			OperationIndex: index,
			Field:          "layer",
			Message:        "import operation requires a layer path",
		})
	}
	if len(op.Imports) == 0 {
		errs = append(errs, ValidationError{
			OperationIndex: index,
			Field:          "imports",
			Message:        "import operation requires at least one import entry",
		})
	}
	for i, entry := range op.Imports {
		fieldPrefix := fmt.Sprintf("imports[%d]", i)
		if entry.Address == "" {
			errs = append(errs, ValidationError{
				OperationIndex: index,
				Field:          fieldPrefix + ".address",
				Message:        "import entry requires an address",
			})
		}
		if entry.ID == "" {
			errs = append(errs, ValidationError{
				OperationIndex: index,
				Field:          fieldPrefix + ".id",
				Message:        "import entry requires an id",
			})
		}

		// Validate source block if present.
		if entry.Source != nil {
			errs = append(errs, validateImportSource(index, fieldPrefix, &entry)...)
		} else {
			// Key is only valid when source is set.
			if entry.Key != "" {
				errs = append(errs, ValidationError{
					OperationIndex: index,
					Field:          fieldPrefix + ".key",
					Message:        "import entry key is only valid when source is set",
				})
			}
		}
	}

	// Check for duplicate addresses in imports (only for entries without source;
	// source-based entries expand dynamically and may share a base address).
	var staticEntries []ImportEntry
	for _, entry := range op.Imports {
		if entry.Source == nil {
			staticEntries = append(staticEntries, entry)
		}
	}
	errs = append(errs, checkDuplicates(index, "imports", staticEntries, func(e ImportEntry) string { return e.Address })...)

	return errs
}

// validateImportSource validates the source block and key field of an import entry.
func validateImportSource(index int, fieldPrefix string, entry *ImportEntry) []ValidationError {
	var errs []ValidationError
	src := entry.Source

	if src.Layer == "" {
		errs = append(errs, ValidationError{
			OperationIndex: index,
			Field:          fieldPrefix + ".source.layer",
			Message:        "import source requires a layer path",
		})
	}
	if src.Address == "" {
		errs = append(errs, ValidationError{
			OperationIndex: index,
			Field:          fieldPrefix + ".source.address",
			Message:        "import source requires a resource address",
		})
	}

	// When expand is set, key is required (must generate unique keys per list element).
	if src.Expand != "" && entry.Key == "" {
		errs = append(errs, ValidationError{
			OperationIndex: index,
			Field:          fieldPrefix + ".key",
			Message:        "import entry key is required when source.expand is set",
		})
	}

	return errs
}

// validateCondition checks the structural correctness of a Condition block.
func validateCondition(cond *Condition) []ValidationError {
	var errs []ValidationError

	for i := range cond.ResourcesExist {
		errs = append(errs, validateResourceCheck("condition.resources_exist", i, &cond.ResourcesExist[i])...)
	}

	for i := range cond.ResourcesNotExist {
		errs = append(errs, validateResourceCheck("condition.resources_not_exist", i, &cond.ResourcesNotExist[i])...)
	}

	// Check for contradictory conditions: same (layer, address) in both exist and not_exist.
	existAddrs := make(map[string]bool)
	for _, rc := range cond.ResourcesExist {
		for _, addr := range rc.Addresses {
			existAddrs[rc.Layer+"\x00"+addr] = true
		}
	}
	for _, rc := range cond.ResourcesNotExist {
		for _, addr := range rc.Addresses {
			if existAddrs[rc.Layer+"\x00"+addr] {
				errs = append(errs, ValidationError{
					OperationIndex: -1,
					Field:          "condition",
					Message:        fmt.Sprintf("contradictory condition: %q in layer %q appears in both resources_exist and resources_not_exist", addr, rc.Layer),
				})
			}
		}
	}

	// Check for contradictory layer conditions: same path in both layer_exists and layer_not_exists.
	layerExistSet := make(map[string]bool)
	for i, path := range cond.LayerExists {
		if path == "" {
			errs = append(errs, ValidationError{
				OperationIndex: -1,
				Field:          fmt.Sprintf("condition.layer_exists[%d]", i),
				Message:        "layer path must not be empty",
			})
		}
		layerExistSet[path] = true
	}

	for i, path := range cond.LayerNotExists {
		if path == "" {
			errs = append(errs, ValidationError{
				OperationIndex: -1,
				Field:          fmt.Sprintf("condition.layer_not_exists[%d]", i),
				Message:        "layer path must not be empty",
			})
		}
		if layerExistSet[path] {
			errs = append(errs, ValidationError{
				OperationIndex: -1,
				Field:          "condition",
				Message:        fmt.Sprintf("contradictory condition: layer %q appears in both layer_exists and layer_not_exists", path),
			})
		}
	}

	return errs
}

// validateResourceCheck checks a single ResourceCheck entry within a condition.
func validateResourceCheck(parentField string, index int, rc *ResourceCheck) []ValidationError {
	var errs []ValidationError
	fieldPrefix := fmt.Sprintf("%s[%d]", parentField, index)

	if rc.Layer == "" {
		errs = append(errs, ValidationError{
			OperationIndex: -1,
			Field:          fieldPrefix + ".layer",
			Message:        "resource check requires a layer path",
		})
	}

	if len(rc.Addresses) == 0 {
		errs = append(errs, ValidationError{
			OperationIndex: -1,
			Field:          fieldPrefix + ".addresses",
			Message:        "resource check requires at least one address",
		})
	}

	for j, addr := range rc.Addresses {
		if addr == "" {
			errs = append(errs, ValidationError{
				OperationIndex: -1,
				Field:          fmt.Sprintf("%s.addresses[%d]", fieldPrefix, j),
				Message:        "address must not be empty",
			})
		}
	}

	return errs
}

// checkDuplicates detects duplicate keys in a slice of entries, using keyFn to extract
// the key from each entry. Returns validation errors for any duplicates found.
func checkDuplicates[T any](opIndex int, fieldName string, entries []T, keyFn func(T) string) []ValidationError {
	var errs []ValidationError
	seen := make(map[string]int) // key -> first index
	for i, entry := range entries {
		key := keyFn(entry)
		if key == "" {
			continue // empty keys are caught by other validation
		}
		if firstIdx, ok := seen[key]; ok {
			errs = append(errs, ValidationError{
				OperationIndex: opIndex,
				Field:          fmt.Sprintf("%s[%d]", fieldName, i),
				Message:        fmt.Sprintf("duplicate address %q (first at %s[%d])", key, fieldName, firstIdx),
			})
		} else {
			seen[key] = i
		}
	}
	return errs
}

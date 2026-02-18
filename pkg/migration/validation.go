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
	if len(op.Resources) == 0 {
		errs = append(errs, ValidationError{
			OperationIndex: index,
			Field:          "resources",
			Message:        "move operation requires at least one resource",
		})
	}

	for i, res := range op.Resources {
		errs = append(errs, validateResourceMove(index, i, &res)...)
	}

	return errs
}

// validateResourceMove checks a single ResourceMove entry within a move operation.
func validateResourceMove(opIndex, resIndex int, res *ResourceMove) []ValidationError {
	var errs []ValidationError
	fieldPrefix := fmt.Sprintf("resources[%d]", resIndex)

	if res.Address == "" {
		errs = append(errs, ValidationError{
			OperationIndex: opIndex,
			Field:          fieldPrefix + ".address",
			Message:        "resource address is required",
		})
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

// validateRename checks that a rename operation has all required fields.
func validateRename(index int, op *Operation) []ValidationError {
	var errs []ValidationError

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

	return errs
}

// validateRemove checks that a remove operation has all required fields.
func validateRemove(index int, op *Operation) []ValidationError {
	var errs []ValidationError

	if op.Layer == "" {
		errs = append(errs, ValidationError{
			OperationIndex: index,
			Field:          "layer",
			Message:        "remove operation requires a layer path",
		})
	}
	if len(op.Addresses) == 0 {
		errs = append(errs, ValidationError{
			OperationIndex: index,
			Field:          "addresses",
			Message:        "remove operation requires at least one address",
		})
	}

	return errs
}

// validateImport checks that an import operation has all required fields.
func validateImport(index int, op *Operation) []ValidationError {
	var errs []ValidationError

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
		if entry.ImportID == "" {
			errs = append(errs, ValidationError{
				OperationIndex: index,
				Field:          fieldPrefix + ".import_id",
				Message:        "import entry requires an import_id",
			})
		}
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

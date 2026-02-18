package migration

import "fmt"

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

	if op.Source == nil {
		errs = append(errs, ValidationError{
			OperationIndex: index,
			Field:          "source",
			Message:        "move operation requires a source",
		})
	} else {
		if op.Source.Layer == "" {
			errs = append(errs, ValidationError{
				OperationIndex: index,
				Field:          "source.layer",
				Message:        "source layer path is required",
			})
		}
		if op.Source.Address == "" {
			errs = append(errs, ValidationError{
				OperationIndex: index,
				Field:          "source.address",
				Message:        "source address is required",
			})
		}
	}

	if op.Destination == nil {
		errs = append(errs, ValidationError{
			OperationIndex: index,
			Field:          "destination",
			Message:        "move operation requires a destination",
		})
	} else {
		if op.Destination.Layer == "" {
			errs = append(errs, ValidationError{
				OperationIndex: index,
				Field:          "destination.layer",
				Message:        "destination layer path is required",
			})
		}
		if op.Destination.Address == "" {
			errs = append(errs, ValidationError{
				OperationIndex: index,
				Field:          "destination.address",
				Message:        "destination address is required",
			})
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
	if op.From == "" {
		errs = append(errs, ValidationError{
			OperationIndex: index,
			Field:          "from",
			Message:        "rename operation requires a 'from' address",
		})
	}
	if op.To == "" {
		errs = append(errs, ValidationError{
			OperationIndex: index,
			Field:          "to",
			Message:        "rename operation requires a 'to' address",
		})
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
	if op.Address == "" {
		errs = append(errs, ValidationError{
			OperationIndex: index,
			Field:          "address",
			Message:        "remove operation requires an address",
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
	if op.Address == "" {
		errs = append(errs, ValidationError{
			OperationIndex: index,
			Field:          "address",
			Message:        "import operation requires an address",
		})
	}
	if op.ImportID == "" {
		errs = append(errs, ValidationError{
			OperationIndex: index,
			Field:          "import_id",
			Message:        "import operation requires an import_id",
		})
	}

	return errs
}

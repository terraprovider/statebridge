package migration

import (
	"testing"
)

func TestValidate_ValidMoveOperation(t *testing.T) {
	mf := &MigrationFile{
		Description: "Valid move",
		Operations: []Operation{
			{
				Type: OpMove,
				Source: &Endpoint{
					Layer:   "./src",
					Address: "aws_instance.web",
				},
				Destination: &Endpoint{
					Layer:   "./dst",
					Address: "aws_instance.web",
				},
			},
		},
	}

	errs := Validate(mf)
	if len(errs) != 0 {
		t.Errorf("expected no errors, got %v", errs)
	}
}

func TestValidate_ValidRenameOperation(t *testing.T) {
	mf := &MigrationFile{
		Description: "Valid rename",
		Operations: []Operation{
			{
				Type:  OpRename,
				Layer: "./layers/net",
				From:  "module.old",
				To:    "module.new",
			},
		},
	}

	errs := Validate(mf)
	if len(errs) != 0 {
		t.Errorf("expected no errors, got %v", errs)
	}
}

func TestValidate_ValidRemoveOperation(t *testing.T) {
	mf := &MigrationFile{
		Description: "Valid remove",
		Operations: []Operation{
			{
				Type:    OpRemove,
				Layer:   "./layers/legacy",
				Address: "aws_iam_role.deprecated",
			},
		},
	}

	errs := Validate(mf)
	if len(errs) != 0 {
		t.Errorf("expected no errors, got %v", errs)
	}
}

func TestValidate_ValidImportOperation(t *testing.T) {
	mf := &MigrationFile{
		Description: "Valid import",
		Operations: []Operation{
			{
				Type:     OpImport,
				Layer:    "./layers/db",
				Address:  "aws_db_instance.primary",
				ImportID: "my-db-id",
			},
		},
	}

	errs := Validate(mf)
	if len(errs) != 0 {
		t.Errorf("expected no errors, got %v", errs)
	}
}

func TestValidate_MissingDescription(t *testing.T) {
	mf := &MigrationFile{
		Operations: []Operation{
			{
				Type:    OpRemove,
				Layer:   "./l",
				Address: "aws_instance.x",
			},
		},
	}

	errs := Validate(mf)
	if !hasError(errs, "description") {
		t.Error("expected validation error for missing description")
	}
}

func TestValidate_EmptyOperations(t *testing.T) {
	mf := &MigrationFile{
		Description: "Empty",
		Operations:  []Operation{},
	}

	errs := Validate(mf)
	if !hasError(errs, "operations") {
		t.Error("expected validation error for empty operations")
	}
}

func TestValidate_UnknownOperationType(t *testing.T) {
	mf := &MigrationFile{
		Description: "Unknown type",
		Operations: []Operation{
			{Type: "invalid"},
		},
	}

	errs := Validate(mf)
	if !hasError(errs, "type") {
		t.Error("expected validation error for unknown type")
	}
}

func TestValidate_MissingOperationType(t *testing.T) {
	mf := &MigrationFile{
		Description: "Missing type",
		Operations: []Operation{
			{},
		},
	}

	errs := Validate(mf)
	if !hasError(errs, "type") {
		t.Error("expected validation error for missing type")
	}
}

func TestValidate_MoveMissingSource(t *testing.T) {
	mf := &MigrationFile{
		Description: "Bad move",
		Operations: []Operation{
			{
				Type: OpMove,
				Destination: &Endpoint{
					Layer:   "./dst",
					Address: "aws_instance.web",
				},
			},
		},
	}

	errs := Validate(mf)
	if !hasError(errs, "source") {
		t.Error("expected validation error for missing source")
	}
}

func TestValidate_MoveMissingDestination(t *testing.T) {
	mf := &MigrationFile{
		Description: "Bad move",
		Operations: []Operation{
			{
				Type: OpMove,
				Source: &Endpoint{
					Layer:   "./src",
					Address: "aws_instance.web",
				},
			},
		},
	}

	errs := Validate(mf)
	if !hasError(errs, "destination") {
		t.Error("expected validation error for missing destination")
	}
}

func TestValidate_MoveEmptySourceFields(t *testing.T) {
	mf := &MigrationFile{
		Description: "Bad move",
		Operations: []Operation{
			{
				Type:        OpMove,
				Source:      &Endpoint{},
				Destination: &Endpoint{Layer: "./dst", Address: "aws_instance.web"},
			},
		},
	}

	errs := Validate(mf)
	if !hasError(errs, "source.layer") {
		t.Error("expected validation error for empty source.layer")
	}
	if !hasError(errs, "source.address") {
		t.Error("expected validation error for empty source.address")
	}
}

func TestValidate_RenameMissingFields(t *testing.T) {
	mf := &MigrationFile{
		Description: "Bad rename",
		Operations: []Operation{
			{Type: OpRename},
		},
	}

	errs := Validate(mf)
	if !hasError(errs, "layer") {
		t.Error("expected validation error for missing layer")
	}
	if !hasError(errs, "from") {
		t.Error("expected validation error for missing from")
	}
	if !hasError(errs, "to") {
		t.Error("expected validation error for missing to")
	}
}

func TestValidate_RemoveMissingFields(t *testing.T) {
	mf := &MigrationFile{
		Description: "Bad remove",
		Operations: []Operation{
			{Type: OpRemove},
		},
	}

	errs := Validate(mf)
	if !hasError(errs, "layer") {
		t.Error("expected validation error for missing layer")
	}
	if !hasError(errs, "address") {
		t.Error("expected validation error for missing address")
	}
}

func TestValidate_ImportMissingFields(t *testing.T) {
	mf := &MigrationFile{
		Description: "Bad import",
		Operations: []Operation{
			{Type: OpImport},
		},
	}

	errs := Validate(mf)
	if !hasError(errs, "layer") {
		t.Error("expected validation error for missing layer")
	}
	if !hasError(errs, "address") {
		t.Error("expected validation error for missing address")
	}
	if !hasError(errs, "import_id") {
		t.Error("expected validation error for missing import_id")
	}
}

func TestValidate_MultipleErrors(t *testing.T) {
	mf := &MigrationFile{
		Operations: []Operation{
			{Type: OpMove},
			{Type: "invalid"},
			{Type: OpRename},
		},
	}

	errs := Validate(mf)
	// At least: missing description + move missing source/dest + invalid type + rename missing fields
	if len(errs) < 4 {
		t.Errorf("expected at least 4 errors, got %d: %v", len(errs), errs)
	}
}

func TestValidationError_Error(t *testing.T) {
	// File-level error
	e := ValidationError{OperationIndex: -1, Field: "description", Message: "required"}
	expected := "validation error: description: required"
	if e.Error() != expected {
		t.Errorf("expected %q, got %q", expected, e.Error())
	}

	// Operation-level error
	e = ValidationError{OperationIndex: 2, Field: "source", Message: "missing"}
	expected = "validation error in operation[2]: source: missing"
	if e.Error() != expected {
		t.Errorf("expected %q, got %q", expected, e.Error())
	}
}

// hasError checks if any validation error matches the given field name.
func hasError(errs []ValidationError, field string) bool {
	for _, e := range errs {
		if e.Field == field {
			return true
		}
	}
	return false
}

func TestValidate_KeyPrefixOnNonWildcard(t *testing.T) {
	mf := &MigrationFile{
		Description: "Bad key_prefix",
		Operations: []Operation{
			{
				Type: OpMove,
				Source: &Endpoint{
					Layer:     "./src",
					Address:   "aws_instance.web",
					KeyPrefix: "prod_",
				},
				Destination: &Endpoint{
					Layer:   "./dst",
					Address: "aws_instance.web",
				},
			},
		},
	}

	errs := Validate(mf)
	if !hasError(errs, "source.key_prefix") {
		t.Error("expected validation error for key_prefix on non-wildcard address")
	}
}

func TestValidate_KeyPrefixOnDestination(t *testing.T) {
	mf := &MigrationFile{
		Description: "Bad key_prefix on dest",
		Operations: []Operation{
			{
				Type: OpMove,
				Source: &Endpoint{
					Layer:   "./src",
					Address: "aws_resource.items[*]",
				},
				Destination: &Endpoint{
					Layer:     "./dst",
					Address:   "aws_resource.items",
					KeyPrefix: "prod_",
				},
			},
		},
	}

	errs := Validate(mf)
	if !hasError(errs, "destination.key_prefix") {
		t.Error("expected validation error for key_prefix on destination endpoint")
	}
}

func TestValidate_KeyPrefixValid(t *testing.T) {
	mf := &MigrationFile{
		Description: "Valid key_prefix",
		Operations: []Operation{
			{
				Type: OpMove,
				Source: &Endpoint{
					Layer:     "./src",
					Address:   "aws_resource.items[*]",
					KeyPrefix: "prod_",
				},
				Destination: &Endpoint{
					Layer:   "./dst",
					Address: `aws_resource.items["{{ .Key }}"]`,
				},
			},
		},
	}

	errs := Validate(mf)
	if len(errs) != 0 {
		t.Errorf("expected no errors, got %v", errs)
	}
}

func TestValidate_KeyPrefixConsistency_AllPrefixed(t *testing.T) {
	mf := &MigrationFile{
		Description: "Two prefixed moves",
		Operations: []Operation{
			{
				Type: OpMove,
				Source: &Endpoint{
					Layer:     "./src",
					Address:   "aws_resource.items[*]",
					KeyPrefix: "eng_",
				},
				Destination: &Endpoint{
					Layer:   "./dst1",
					Address: `aws_resource.items["{{ .Key }}"]`,
				},
			},
			{
				Type: OpMove,
				Source: &Endpoint{
					Layer:     "./src",
					Address:   "aws_resource.items[*]",
					KeyPrefix: "fin_",
				},
				Destination: &Endpoint{
					Layer:   "./dst2",
					Address: `aws_resource.items["{{ .Key }}"]`,
				},
			},
		},
	}

	errs := Validate(mf)
	if len(errs) != 0 {
		t.Errorf("expected no errors for two prefixed moves, got %v", errs)
	}
}

func TestValidate_KeyPrefixConsistency_MixedFiltered(t *testing.T) {
	mf := &MigrationFile{
		Description: "Mixed prefix moves",
		Operations: []Operation{
			{
				Type: OpMove,
				Source: &Endpoint{
					Layer:     "./src",
					Address:   "aws_resource.items[*]",
					KeyPrefix: "eng_",
				},
				Destination: &Endpoint{
					Layer:   "./dst1",
					Address: `aws_resource.items["{{ .Key }}"]`,
				},
			},
			{
				Type: OpMove,
				Source: &Endpoint{
					Layer:   "./src",
					Address: "aws_resource.items[*]",
					// Missing key_prefix — should error
				},
				Destination: &Endpoint{
					Layer:   "./dst2",
					Address: `aws_resource.items["{{ .Key }}"]`,
				},
			},
		},
	}

	errs := Validate(mf)
	if !hasError(errs, "source.key_prefix") {
		t.Error("expected validation error for mixed prefixed/non-prefixed wildcard moves")
	}
}

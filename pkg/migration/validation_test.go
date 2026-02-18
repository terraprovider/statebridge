package migration

import (
	"testing"
)

func TestValidate_ValidMoveOperation(t *testing.T) {
	mf := &MigrationFile{
		Description: "Valid move",
		Operations: []Operation{
			{
				Type:             OpMove,
				SourceLayer:      "./src",
				DestinationLayer: "./dst",
				Resources: []ResourceMove{
					{Address: "aws_instance.web"},
				},
			},
		},
	}

	errs := Validate(mf)
	if len(errs) != 0 {
		t.Errorf("expected no errors, got %v", errs)
	}
}

func TestValidate_ValidMoveWithKeys(t *testing.T) {
	mf := &MigrationFile{
		Description: "Valid move with keys",
		Operations: []Operation{
			{
				Type:             OpMove,
				SourceLayer:      "./src",
				DestinationLayer: "./dst",
				Resources: []ResourceMove{
					{
						Address: "aws_s3_bucket.data",
						Keys: map[string]string{
							"exact_key":  "new_key",
							"prefix_*":   `{{ .Key | trimPrefix "prefix_" }}`,
							"*":          "{{ .Key }}",
						},
					},
				},
			},
		},
	}

	errs := Validate(mf)
	if len(errs) != 0 {
		t.Errorf("expected no errors, got %v", errs)
	}
}

func TestValidate_ValidMoveWithAddressPrefix(t *testing.T) {
	mf := &MigrationFile{
		Description: "Valid move with address prefix",
		Operations: []Operation{
			{
				Type:             OpMove,
				SourceLayer:      "./src",
				DestinationLayer: "./dst",
				AddressPrefix:    "module.identity_governance",
				Resources: []ResourceMove{
					{Address: "azuread_access_package_catalog.all"},
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
				Renames: []RenameEntry{
					{From: "module.old", To: "module.new"},
				},
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
				Type:      OpRemove,
				Layer:     "./layers/legacy",
				Addresses: []string{"aws_iam_role.deprecated"},
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
				Type:  OpImport,
				Layer: "./layers/db",
				Imports: []ImportEntry{
					{Address: "aws_db_instance.primary", ImportID: "my-db-id"},
				},
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
				Type:      OpRemove,
				Layer:     "./l",
				Addresses: []string{"aws_instance.x"},
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

func TestValidate_MoveMissingSourceLayer(t *testing.T) {
	mf := &MigrationFile{
		Description: "Bad move",
		Operations: []Operation{
			{
				Type:             OpMove,
				DestinationLayer: "./dst",
				Resources: []ResourceMove{
					{Address: "aws_instance.web"},
				},
			},
		},
	}

	errs := Validate(mf)
	if !hasError(errs, "source_layer") {
		t.Error("expected validation error for missing source_layer")
	}
}

func TestValidate_MoveMissingDestinationLayer(t *testing.T) {
	mf := &MigrationFile{
		Description: "Bad move",
		Operations: []Operation{
			{
				Type:        OpMove,
				SourceLayer: "./src",
				Resources: []ResourceMove{
					{Address: "aws_instance.web"},
				},
			},
		},
	}

	errs := Validate(mf)
	if !hasError(errs, "destination_layer") {
		t.Error("expected validation error for missing destination_layer")
	}
}

func TestValidate_MoveMissingResources(t *testing.T) {
	mf := &MigrationFile{
		Description: "Bad move",
		Operations: []Operation{
			{
				Type:             OpMove,
				SourceLayer:      "./src",
				DestinationLayer: "./dst",
			},
		},
	}

	errs := Validate(mf)
	if !hasError(errs, "resources") {
		t.Error("expected validation error for missing resources")
	}
}

func TestValidate_MoveResourceMissingAddress(t *testing.T) {
	mf := &MigrationFile{
		Description: "Bad move resource",
		Operations: []Operation{
			{
				Type:             OpMove,
				SourceLayer:      "./src",
				DestinationLayer: "./dst",
				Resources: []ResourceMove{
					{Address: ""},
				},
			},
		},
	}

	errs := Validate(mf)
	if !hasError(errs, "resources[0].address") {
		t.Error("expected validation error for missing resource address")
	}
}

func TestValidate_InvalidKeyPattern_WildcardInMiddle(t *testing.T) {
	mf := &MigrationFile{
		Description: "Bad key pattern",
		Operations: []Operation{
			{
				Type:             OpMove,
				SourceLayer:      "./src",
				DestinationLayer: "./dst",
				Resources: []ResourceMove{
					{
						Address: "resource.all",
						Keys: map[string]string{
							"pre*fix": "value",
						},
					},
				},
			},
		},
	}

	errs := Validate(mf)
	if !hasError(errs, "resources[0].keys") {
		t.Error("expected validation error for wildcard in middle of key pattern")
	}
}

func TestValidate_InvalidKeyPattern_MultipleWildcards(t *testing.T) {
	mf := &MigrationFile{
		Description: "Bad key pattern",
		Operations: []Operation{
			{
				Type:             OpMove,
				SourceLayer:      "./src",
				DestinationLayer: "./dst",
				Resources: []ResourceMove{
					{
						Address: "resource.all",
						Keys: map[string]string{
							"*prefix*": "value",
						},
					},
				},
			},
		},
	}

	errs := Validate(mf)
	if !hasError(errs, "resources[0].keys") {
		t.Error("expected validation error for multiple wildcards in key pattern")
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
	if !hasError(errs, "renames") {
		t.Error("expected validation error for missing renames")
	}
}

func TestValidate_RenameEntryMissingFields(t *testing.T) {
	mf := &MigrationFile{
		Description: "Bad rename entry",
		Operations: []Operation{
			{
				Type:    OpRename,
				Layer:   "./layers/net",
				Renames: []RenameEntry{{From: "", To: ""}},
			},
		},
	}

	errs := Validate(mf)
	if !hasError(errs, "renames[0].from") {
		t.Error("expected validation error for missing from")
	}
	if !hasError(errs, "renames[0].to") {
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
	if !hasError(errs, "addresses") {
		t.Error("expected validation error for missing addresses")
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
	if !hasError(errs, "imports") {
		t.Error("expected validation error for missing imports")
	}
}

func TestValidate_ImportEntryMissingFields(t *testing.T) {
	mf := &MigrationFile{
		Description: "Bad import entry",
		Operations: []Operation{
			{
				Type:    OpImport,
				Layer:   "./layers/db",
				Imports: []ImportEntry{{Address: "", ImportID: ""}},
			},
		},
	}

	errs := Validate(mf)
	if !hasError(errs, "imports[0].address") {
		t.Error("expected validation error for missing import address")
	}
	if !hasError(errs, "imports[0].import_id") {
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
	// At least: missing description + move missing fields + invalid type + rename missing fields
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
	e = ValidationError{OperationIndex: 2, Field: "source_layer", Message: "missing"}
	expected = "validation error in operation[2]: source_layer: missing"
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

package migration

import (
	"testing"
)

func boolPtr(b bool) *bool {
	return &b
}

func TestValidate_ValidMoveOperation(t *testing.T) {
	mf := &MigrationFile{
		Description: "Valid move",
		Operations: []Operation{
			{
				Type:             OpMove,
				SourceLayer:      "./src",
				DestinationLayer: "./dst",
				Resources: []ResourceMove{
					{From: "aws_instance.web"},
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
						From: "aws_s3_bucket.data",
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
					{From: "azuread_access_package_catalog.all"},
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
				Type:    OpRemove,
				Layer:   "./layers/legacy",
				Entries: []RemoveEntry{{Address: "aws_iam_role.deprecated"}},
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
					{Address: "aws_db_instance.primary", ID: "my-db-id"},
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
				Type:    OpRemove,
				Layer:   "./l",
				Entries: []RemoveEntry{{Address: "aws_instance.x"}},
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
					{From: "aws_instance.web"},
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
					{From: "aws_instance.web"},
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
					{From: ""},
				},
			},
		},
	}

	errs := Validate(mf)
	if !hasError(errs, "resources[0].from") {
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
						From: "resource.all",
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
						From: "resource.all",
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
	if !hasError(errs, "entries") {
		t.Error("expected validation error for missing entries")
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
				Imports: []ImportEntry{{Address: "", ID: ""}},
			},
		},
	}

	errs := Validate(mf)
	if !hasError(errs, "imports[0].address") {
		t.Error("expected validation error for missing import address")
	}
	if !hasError(errs, "imports[0].id") {
		t.Error("expected validation error for missing id")
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

func TestValidate_ValidCondition(t *testing.T) {
	mf := &MigrationFile{
		Description: "Valid with condition",
		Condition: &Condition{
			ResourcesExist: []ResourceCheck{
				{Layer: "./layers/compute", Addresses: []string{"aws_instance.web"}},
			},
			ResourcesNotExist: []ResourceCheck{
				{Layer: "./layers/app", Addresses: []string{"aws_instance.web"}},
			},
		},
		Operations: []Operation{
			{
				Type:    OpRemove,
				Layer:   "./l",
				Entries: []RemoveEntry{{Address: "aws_instance.x"}},
			},
		},
	}

	errs := Validate(mf)
	if len(errs) != 0 {
		t.Errorf("expected no errors, got %v", errs)
	}
}

func TestValidate_ConditionMissingLayer(t *testing.T) {
	mf := &MigrationFile{
		Description: "Missing layer in condition",
		Condition: &Condition{
			ResourcesExist: []ResourceCheck{
				{Layer: "", Addresses: []string{"aws_instance.web"}},
			},
		},
		Operations: []Operation{
			{
				Type:    OpRemove,
				Layer:   "./l",
				Entries: []RemoveEntry{{Address: "aws_instance.x"}},
			},
		},
	}

	errs := Validate(mf)
	if !hasError(errs, "condition.resources_exist[0].layer") {
		t.Errorf("expected error for missing layer, got %v", errs)
	}
}

func TestValidate_ConditionEmptyAddresses(t *testing.T) {
	mf := &MigrationFile{
		Description: "Empty addresses in condition",
		Condition: &Condition{
			ResourcesNotExist: []ResourceCheck{
				{Layer: "./layers/app", Addresses: []string{}},
			},
		},
		Operations: []Operation{
			{
				Type:    OpRemove,
				Layer:   "./l",
				Entries: []RemoveEntry{{Address: "aws_instance.x"}},
			},
		},
	}

	errs := Validate(mf)
	if !hasError(errs, "condition.resources_not_exist[0].addresses") {
		t.Errorf("expected error for empty addresses, got %v", errs)
	}
}

func TestValidate_ConditionEmptyAddressString(t *testing.T) {
	mf := &MigrationFile{
		Description: "Empty address string in condition",
		Condition: &Condition{
			ResourcesExist: []ResourceCheck{
				{Layer: "./layers/compute", Addresses: []string{"aws_instance.web", ""}},
			},
		},
		Operations: []Operation{
			{
				Type:    OpRemove,
				Layer:   "./l",
				Entries: []RemoveEntry{{Address: "aws_instance.x"}},
			},
		},
	}

	errs := Validate(mf)
	if !hasError(errs, "condition.resources_exist[0].addresses[1]") {
		t.Errorf("expected error for empty address string, got %v", errs)
	}
}

func TestValidate_EmptyConditionStruct(t *testing.T) {
	mf := &MigrationFile{
		Description: "Empty condition struct",
		Condition:   &Condition{},
		Operations: []Operation{
			{
				Type:    OpRemove,
				Layer:   "./l",
				Entries: []RemoveEntry{{Address: "aws_instance.x"}},
			},
		},
	}

	errs := Validate(mf)
	if len(errs) != 0 {
		t.Errorf("expected no errors for empty condition struct, got %v", errs)
	}
}

func TestIsModuleAddress(t *testing.T) {
	tests := []struct {
		address  string
		expected bool
	}{
		{"module.foo", true},
		{"module.foo.module.bar", true},
		{"module.a.module.b.module.c", true},
		{"module.foo.aws_instance.web", false},
		{"aws_instance.web", false},
		{"module.foo.module.bar.aws_instance.web", false},
		{"", false},
		{"module", false},                  // odd number of segments
		{"module.foo.module", false},        // odd number of segments
		{"aws_instance.web.module.foo", false}, // doesn't start with module
	}
	for _, tt := range tests {
		result := IsModuleAddress(tt.address)
		if result != tt.expected {
			t.Errorf("IsModuleAddress(%q) = %v, want %v", tt.address, result, tt.expected)
		}
	}
}

func TestValidate_ModuleMoveValid(t *testing.T) {
	mf := &MigrationFile{
		Description: "Valid module move",
		Operations: []Operation{
			{
				Type:             OpMove,
				SourceLayer:      "./src",
				DestinationLayer: "./dst",
				Resources: []ResourceMove{
					{From: "module.foo"},
				},
			},
		},
	}

	errs := Validate(mf)
	if len(errs) != 0 {
		t.Errorf("expected no errors, got %v", errs)
	}
}

func TestValidate_ModuleMoveWithDestinationModule(t *testing.T) {
	mf := &MigrationFile{
		Description: "Valid module move with destination",
		Operations: []Operation{
			{
				Type:             OpMove,
				SourceLayer:      "./src",
				DestinationLayer: "./dst",
				Resources: []ResourceMove{
					{From: "module.foo", To: "module.bar"},
				},
			},
		},
	}

	errs := Validate(mf)
	if len(errs) != 0 {
		t.Errorf("expected no errors, got %v", errs)
	}
}

func TestValidate_ModuleMoveWithKeys(t *testing.T) {
	mf := &MigrationFile{
		Description: "Module move with keys (invalid)",
		Operations: []Operation{
			{
				Type:             OpMove,
				SourceLayer:      "./src",
				DestinationLayer: "./dst",
				Resources: []ResourceMove{
					{
						From: "module.foo",
						Keys: map[string]string{"*": "{{ .Key }}"},
					},
				},
			},
		},
	}

	errs := Validate(mf)
	if !hasError(errs, "resources[0].keys") {
		t.Errorf("expected validation error for keys on module move, got %v", errs)
	}
}

func TestValidate_ModuleMoveWithImportID(t *testing.T) {
	mf := &MigrationFile{
		Description: "Module move with import_id (invalid)",
		Operations: []Operation{
			{
				Type:             OpMove,
				SourceLayer:      "./src",
				DestinationLayer: "./dst",
				Resources: []ResourceMove{
					{
						From:     "module.foo",
						ImportID: "some-id",
					},
				},
			},
		},
	}

	errs := Validate(mf)
	if !hasError(errs, "resources[0].import_id") {
		t.Errorf("expected validation error for import_id on module move, got %v", errs)
	}
}

func TestValidate_ModuleMoveWithNonModuleDestination(t *testing.T) {
	mf := &MigrationFile{
		Description: "Module move with non-module destination (invalid)",
		Operations: []Operation{
			{
				Type:             OpMove,
				SourceLayer:      "./src",
				DestinationLayer: "./dst",
				Resources: []ResourceMove{
					{
						From: "module.foo",
						To:   "aws_instance.web",
					},
				},
			},
		},
	}

	errs := Validate(mf)
	if !hasError(errs, "resources[0].to") {
		t.Errorf("expected validation error for non-module destination, got %v", errs)
	}
}

func TestValidate_AllResourcesValid(t *testing.T) {
	mf := &MigrationFile{
		Description: "All resources move",
		Operations: []Operation{
			{
				Type:             OpMove,
				SourceLayer:      "./src",
				DestinationLayer: "./dst",
				AllResources:     true,
			},
		},
	}

	errs := Validate(mf)
	if len(errs) != 0 {
		t.Errorf("expected no errors, got %v", errs)
	}
}

func TestValidate_AllResourcesWithValidOverride(t *testing.T) {
	mf := &MigrationFile{
		Description: "All resources with rename",
		Operations: []Operation{
			{
				Type:             OpMove,
				SourceLayer:      "./src",
				DestinationLayer: "./dst",
				AllResources:     true,
				Overrides: []ResourceMove{
					{From: "aws_instance.web", To: "aws_instance.api"},
				},
			},
		},
	}

	errs := Validate(mf)
	if len(errs) != 0 {
		t.Errorf("expected no errors, got %v", errs)
	}
}

func TestValidate_AllResourcesWithAddressPrefix(t *testing.T) {
	mf := &MigrationFile{
		Description: "All resources with address prefix (invalid)",
		Operations: []Operation{
			{
				Type:             OpMove,
				SourceLayer:      "./src",
				DestinationLayer: "./dst",
				AllResources:     true,
				AddressPrefix:    "module.ig",
			},
		},
	}

	errs := Validate(mf)
	if !hasError(errs, "address_prefix") {
		t.Errorf("expected validation error for address_prefix with all_resources, got %v", errs)
	}
}

func TestValidate_AllResourcesOverrideMissingDestinationAndImportID(t *testing.T) {
	mf := &MigrationFile{
		Description: "All resources override without destination or import_id (invalid)",
		Operations: []Operation{
			{
				Type:             OpMove,
				SourceLayer:      "./src",
				DestinationLayer: "./dst",
				AllResources:     true,
				Overrides: []ResourceMove{
					{From: "aws_instance.web"},
				},
			},
		},
	}

	errs := Validate(mf)
	if !hasError(errs, "overrides[0]") {
		t.Errorf("expected validation error for empty override entry, got %v", errs)
	}
}

func TestValidate_AllResourcesOverrideWithKeys(t *testing.T) {
	mf := &MigrationFile{
		Description: "All resources override with keys (invalid)",
		Operations: []Operation{
			{
				Type:             OpMove,
				SourceLayer:      "./src",
				DestinationLayer: "./dst",
				AllResources:     true,
				Overrides: []ResourceMove{
					{
						From: "aws_instance.web",
						To:   "aws_instance.api",
						Keys: map[string]string{"*": "{{ .Key }}"},
					},
				},
			},
		},
	}

	errs := Validate(mf)
	if !hasError(errs, "overrides[0].keys") {
		t.Errorf("expected validation error for keys with all_resources, got %v", errs)
	}
}

func TestValidate_AllResourcesOverrideWithImportIDValid(t *testing.T) {
	mf := &MigrationFile{
		Description: "All resources override with import_id (valid)",
		Operations: []Operation{
			{
				Type:             OpMove,
				SourceLayer:      "./src",
				DestinationLayer: "./dst",
				AllResources:     true,
				Overrides: []ResourceMove{
					{
						From:     "aws_instance.web",
						To:       "aws_instance.api",
						ImportID: "some-id",
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

func TestValidate_AllResourcesOverrideWithImportIDOnly(t *testing.T) {
	mf := &MigrationFile{
		Description: "All resources override with import_id only (valid)",
		Operations: []Operation{
			{
				Type:             OpMove,
				SourceLayer:      "./src",
				DestinationLayer: "./dst",
				AllResources:     true,
				Overrides: []ResourceMove{
					{
						From:     "aws_instance.web",
						ImportID: "{{ .Attributes.project_id }}/{{ .Attributes.id }}",
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

func TestValidate_AllResourcesOverrideWithModuleAddress(t *testing.T) {
	mf := &MigrationFile{
		Description: "All resources override with module address (invalid)",
		Operations: []Operation{
			{
				Type:             OpMove,
				SourceLayer:      "./src",
				DestinationLayer: "./dst",
				AllResources:     true,
				Overrides: []ResourceMove{
					{
						From: "module.foo",
						To:   "module.bar",
					},
				},
			},
		},
	}

	errs := Validate(mf)
	if !hasError(errs, "overrides[0].from") {
		t.Errorf("expected validation error for module address with all_resources, got %v", errs)
	}
}

func TestValidate_AllResourcesOnRenameOperation(t *testing.T) {
	mf := &MigrationFile{
		Description: "All resources on rename (invalid)",
		Operations: []Operation{
			{
				Type:         OpRename,
				Layer:        "./src",
				AllResources: true,
				Renames: []RenameEntry{
					{From: "aws_instance.old", To: "aws_instance.new"},
				},
			},
		},
	}

	errs := Validate(mf)
	if !hasError(errs, "all_resources") {
		t.Errorf("expected validation error for all_resources on rename, got %v", errs)
	}
}

func TestValidate_OverridesWithoutAllResources(t *testing.T) {
	mf := &MigrationFile{
		Description: "Overrides without all_resources (invalid)",
		Operations: []Operation{
			{
				Type:             OpMove,
				SourceLayer:      "./src",
				DestinationLayer: "./dst",
				Resources: []ResourceMove{
					{From: "aws_instance.web"},
				},
				Overrides: []ResourceMove{
					{From: "aws_instance.api", To: "aws_instance.backend"},
				},
			},
		},
	}

	errs := Validate(mf)
	if !hasError(errs, "overrides") {
		t.Errorf("expected validation error for overrides without all_resources, got %v", errs)
	}
}

func TestValidate_OmitValid(t *testing.T) {
	mf := &MigrationFile{
		Description: "All resources with omit",
		Operations: []Operation{
			{
				Type:             OpMove,
				SourceLayer:      "./src",
				DestinationLayer: "./dst",
				AllResources:     true,
				Omit: []OmitEntry{
					{Address: "aws_instance.ephemeral"},
				},
			},
		},
	}

	errs := Validate(mf)
	if len(errs) != 0 {
		t.Errorf("expected no errors, got %v", errs)
	}
}

func TestValidate_OmitWithoutAllResources(t *testing.T) {
	mf := &MigrationFile{
		Description: "Omit without all_resources",
		Operations: []Operation{
			{
				Type:             OpMove,
				SourceLayer:      "./src",
				DestinationLayer: "./dst",
				Resources:        []ResourceMove{{From: "aws_instance.web"}},
				Omit:             []OmitEntry{{Address: "aws_instance.ephemeral"}},
			},
		},
	}

	errs := Validate(mf)
	if !hasError(errs, "omit") {
		t.Errorf("expected validation error for omit without all_resources, got %v", errs)
	}
}

func TestValidate_OmitMissingAddress(t *testing.T) {
	mf := &MigrationFile{
		Description: "Omit missing address",
		Operations: []Operation{
			{
				Type:             OpMove,
				SourceLayer:      "./src",
				DestinationLayer: "./dst",
				AllResources:     true,
				Omit:             []OmitEntry{{Address: ""}},
			},
		},
	}

	errs := Validate(mf)
	if !hasError(errs, "omit[0].address") {
		t.Errorf("expected validation error for missing omit address, got %v", errs)
	}
}

func TestValidate_OmitOverlapWithOverrides(t *testing.T) {
	mf := &MigrationFile{
		Description: "Omit overlapping with overrides",
		Operations: []Operation{
			{
				Type:             OpMove,
				SourceLayer:      "./src",
				DestinationLayer: "./dst",
				AllResources:     true,
				Overrides: []ResourceMove{
					{From: "aws_instance.web", To: "aws_instance.api"},
				},
				Omit: []OmitEntry{
					{Address: "aws_instance.web"},
				},
			},
		},
	}

	errs := Validate(mf)
	if !hasError(errs, "overrides[0].from") {
		t.Errorf("expected validation error for overlapping address, got %v", errs)
	}
}

func TestValidate_StatusRetired(t *testing.T) {
	mf := &MigrationFile{
		Status: StatusRetired,
		// No description, no operations — retired skips all validation.
	}

	errs := Validate(mf)
	if len(errs) != 0 {
		t.Errorf("expected no errors for retired status, got %v", errs)
	}
}

func TestValidate_UnknownStatus(t *testing.T) {
	mf := &MigrationFile{
		Status:      "draft",
		Description: "Bad status",
		Operations: []Operation{
			{
				Type:    OpRemove,
				Layer:   "./l",
				Entries: []RemoveEntry{{Address: "aws_instance.x"}},
			},
		},
	}

	errs := Validate(mf)
	if !hasError(errs, "status") {
		t.Errorf("expected validation error for unknown status, got %v", errs)
	}
}

func TestValidate_LayerExistsEmpty(t *testing.T) {
	mf := &MigrationFile{
		Description: "Layer exists empty path",
		Condition: &Condition{
			LayerExists: []string{"./valid/path", ""},
		},
		Operations: []Operation{
			{
				Type:    OpRemove,
				Layer:   "./l",
				Entries: []RemoveEntry{{Address: "aws_instance.x"}},
			},
		},
	}

	errs := Validate(mf)
	if !hasError(errs, "condition.layer_exists[1]") {
		t.Errorf("expected validation error for empty layer_exists path, got %v", errs)
	}
}

func TestValidate_LayerNotExistsEmpty(t *testing.T) {
	mf := &MigrationFile{
		Description: "Layer not exists empty path",
		Condition: &Condition{
			LayerNotExists: []string{""},
		},
		Operations: []Operation{
			{
				Type:    OpRemove,
				Layer:   "./l",
				Entries: []RemoveEntry{{Address: "aws_instance.x"}},
			},
		},
	}

	errs := Validate(mf)
	if !hasError(errs, "condition.layer_not_exists[0]") {
		t.Errorf("expected validation error for empty layer_not_exists path, got %v", errs)
	}
}

func TestValidate_LayerConditionsValid(t *testing.T) {
	mf := &MigrationFile{
		Description: "Valid layer conditions",
		Condition: &Condition{
			LayerExists:    []string{"./layers/source"},
			LayerNotExists: []string{"./layers/deprecated"},
		},
		Operations: []Operation{
			{
				Type:    OpRemove,
				Layer:   "./l",
				Entries: []RemoveEntry{{Address: "aws_instance.x"}},
			},
		},
	}

	errs := Validate(mf)
	if len(errs) != 0 {
		t.Errorf("expected no errors for valid layer conditions, got %v", errs)
	}
}

func TestValidate_OmitWithDestroy(t *testing.T) {
	mf := &MigrationFile{
		Description: "Omit with destroy",
		Operations: []Operation{
			{
				Type:             OpMove,
				SourceLayer:      "./src",
				DestinationLayer: "./dst",
				AllResources:     true,
				Omit: []OmitEntry{
					{Address: "aws_instance.ephemeral", Destroy: boolPtr(true)},
				},
			},
		},
	}

	errs := Validate(mf)
	if len(errs) != 0 {
		t.Errorf("expected no errors, got %v", errs)
	}
}

// --- Import Source validation tests ---

func TestValidate_ValidImportWithSource(t *testing.T) {
	mf := &MigrationFile{
		Description: "Valid import with source",
		Operations: []Operation{
			{
				Type:  OpImport,
				Layer: "./layers/app",
				Imports: []ImportEntry{
					{
						Address: "azuread_api_access.all",
						ID:      `{{ .Attributes.id }}`,
						Source: &ImportSource{
							Layer:   "./layers/app",
							Address: "azuread_application.all",
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

func TestValidate_ValidImportWithSourceAndExpand(t *testing.T) {
	mf := &MigrationFile{
		Description: "Valid import with source and expand",
		Operations: []Operation{
			{
				Type:  OpImport,
				Layer: "./layers/app",
				Imports: []ImportEntry{
					{
						Address: "azuread_api_access.all",
						ID:      `{{ .Attributes.id }}/apiAccess/{{ .Item.resource_app_id }}`,
						Key:     `{{ .Key }}_{{ .Item.resource_app_id }}`,
						Source: &ImportSource{
							Layer:   "./layers/app",
							Address: "azuread_application.all",
							Expand:  "required_resource_access",
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

func TestValidate_ImportSourceMissingLayer(t *testing.T) {
	mf := &MigrationFile{
		Description: "Import source missing layer",
		Operations: []Operation{
			{
				Type:  OpImport,
				Layer: "./layers/app",
				Imports: []ImportEntry{
					{
						Address: "azuread_api_access.all",
						ID:      `{{ .Attributes.id }}`,
						Source: &ImportSource{
							Address: "azuread_application.all",
						},
					},
				},
			},
		},
	}

	errs := Validate(mf)
	if !hasError(errs, "imports[0].source.layer") {
		t.Error("expected validation error for missing source layer")
	}
}

func TestValidate_ImportSourceMissingAddress(t *testing.T) {
	mf := &MigrationFile{
		Description: "Import source missing address",
		Operations: []Operation{
			{
				Type:  OpImport,
				Layer: "./layers/app",
				Imports: []ImportEntry{
					{
						Address: "azuread_api_access.all",
						ID:      `{{ .Attributes.id }}`,
						Source: &ImportSource{
							Layer: "./layers/app",
						},
					},
				},
			},
		},
	}

	errs := Validate(mf)
	if !hasError(errs, "imports[0].source.address") {
		t.Error("expected validation error for missing source address")
	}
}

func TestValidate_ImportExpandWithoutKey(t *testing.T) {
	mf := &MigrationFile{
		Description: "Import expand without key",
		Operations: []Operation{
			{
				Type:  OpImport,
				Layer: "./layers/app",
				Imports: []ImportEntry{
					{
						Address: "azuread_api_access.all",
						ID:      `{{ .Attributes.id }}`,
						Source: &ImportSource{
							Layer:   "./layers/app",
							Address: "azuread_application.all",
							Expand:  "required_resource_access",
						},
					},
				},
			},
		},
	}

	errs := Validate(mf)
	if !hasError(errs, "imports[0].key") {
		t.Error("expected validation error for missing key when expand is set")
	}
}

func TestValidate_ImportKeyWithoutSource(t *testing.T) {
	mf := &MigrationFile{
		Description: "Import key without source",
		Operations: []Operation{
			{
				Type:  OpImport,
				Layer: "./layers/app",
				Imports: []ImportEntry{
					{
						Address: "azuread_api_access.all",
						ID:      "some-id",
						Key:     `{{ .Key }}`,
					},
				},
			},
		},
	}

	errs := Validate(mf)
	if !hasError(errs, "imports[0].key") {
		t.Error("expected validation error for key without source")
	}
}

func TestValidate_ImportMixedStaticAndSource(t *testing.T) {
	mf := &MigrationFile{
		Description: "Mixed static and source imports",
		Operations: []Operation{
			{
				Type:  OpImport,
				Layer: "./layers/app",
				Imports: []ImportEntry{
					{
						Address: "aws_instance.web",
						ID:      "i-12345",
					},
					{
						Address: "azuread_api_access.all",
						ID:      `{{ .Attributes.id }}`,
						Source: &ImportSource{
							Layer:   "./layers/app",
							Address: "azuread_application.all",
						},
					},
				},
			},
		},
	}

	errs := Validate(mf)
	if len(errs) != 0 {
		t.Errorf("expected no errors for mixed imports, got %v", errs)
	}
}

func TestValidate_MergeDuplicatesValid(t *testing.T) {
	mf := &MigrationFile{
		Description: "Valid merge_duplicates",
		Operations: []Operation{
			{
				Type:             OpMove,
				SourceLayer:      "./layers/src",
				DestinationLayer: "./layers/dst",
				Resources: []ResourceMove{
					{
						From:            "aws_resource.policy_active",
						To:              "aws_resource.policy",
						MergeDuplicates: true,
						Keys:            map[string]string{"key_a": "shared"},
					},
				},
			},
		},
	}

	errs := Validate(mf)
	if len(errs) != 0 {
		t.Errorf("expected no validation errors, got %v", errs)
	}
}

func TestValidate_MergeDuplicatesWithoutKeys(t *testing.T) {
	mf := &MigrationFile{
		Description: "merge_duplicates without keys",
		Operations: []Operation{
			{
				Type:             OpMove,
				SourceLayer:      "./layers/src",
				DestinationLayer: "./layers/dst",
				Resources: []ResourceMove{
					{
						From:            "aws_resource.policy_active",
						MergeDuplicates: true,
					},
				},
			},
		},
	}

	errs := Validate(mf)
	if !hasError(errs, "resources[0].merge_duplicates") {
		t.Errorf("expected validation error for merge_duplicates without keys, got %v", errs)
	}
}

func TestValidate_MergeDuplicatesOnModuleMove(t *testing.T) {
	mf := &MigrationFile{
		Description: "merge_duplicates on module move",
		Operations: []Operation{
			{
				Type:             OpMove,
				SourceLayer:      "./layers/src",
				DestinationLayer: "./layers/dst",
				Resources: []ResourceMove{
					{
						From:            "module.foo",
						MergeDuplicates: true,
					},
				},
			},
		},
	}

	errs := Validate(mf)
	if !hasError(errs, "resources[0].merge_duplicates") {
		t.Errorf("expected validation error for merge_duplicates on module move, got %v", errs)
	}
}

func TestValidate_MergeDuplicatesOnAllResourcesOverride(t *testing.T) {
	mf := &MigrationFile{
		Description: "merge_duplicates on all_resources override",
		Operations: []Operation{
			{
				Type:             OpMove,
				SourceLayer:      "./layers/src",
				DestinationLayer: "./layers/dst",
				AllResources:     true,
				Overrides: []ResourceMove{
					{
						From:            "aws_resource.x",
						To:              "aws_resource.y",
						MergeDuplicates: true,
					},
				},
			},
		},
	}

	errs := Validate(mf)
	if !hasError(errs, "overrides[0].merge_duplicates") {
		t.Errorf("expected validation error for merge_duplicates on override, got %v", errs)
	}
}

func TestValidate_ValidMoveWithSourceDestPrefix(t *testing.T) {
	mf := &MigrationFile{
		Description: "Valid move with source/destination prefix",
		Operations: []Operation{
			{
				Type:              OpMove,
				SourceLayer:       "./src",
				DestinationLayer:  "./dst",
				SourcePrefix:      "module.old",
				DestinationPrefix: "module.new",
				Resources: []ResourceMove{
					{From: "aws_instance.web"},
				},
			},
		},
	}

	errs := Validate(mf)
	if len(errs) != 0 {
		t.Errorf("expected no errors, got %v", errs)
	}
}

func TestValidate_ValidMoveWithSourcePrefixOnly(t *testing.T) {
	mf := &MigrationFile{
		Description: "Valid move with only source prefix",
		Operations: []Operation{
			{
				Type:             OpMove,
				SourceLayer:      "./src",
				DestinationLayer: "./dst",
				SourcePrefix:     "module.old",
				Resources: []ResourceMove{
					{From: "aws_instance.web"},
				},
			},
		},
	}

	errs := Validate(mf)
	if len(errs) != 0 {
		t.Errorf("expected no errors, got %v", errs)
	}
}

func TestValidate_AddressPrefixConflictsWithSourcePrefix(t *testing.T) {
	mf := &MigrationFile{
		Description: "Conflicting address_prefix and source_prefix",
		Operations: []Operation{
			{
				Type:             OpMove,
				SourceLayer:      "./src",
				DestinationLayer: "./dst",
				AddressPrefix:    "module.shared",
				SourcePrefix:     "module.old",
				Resources: []ResourceMove{
					{From: "aws_instance.web"},
				},
			},
		},
	}

	errs := Validate(mf)
	if !hasError(errs, "address_prefix") {
		t.Errorf("expected validation error for conflicting address_prefix, got %v", errs)
	}
}

func TestValidate_AddressPrefixConflictsWithDestPrefix(t *testing.T) {
	mf := &MigrationFile{
		Description: "Conflicting address_prefix and destination_prefix",
		Operations: []Operation{
			{
				Type:              OpMove,
				SourceLayer:       "./src",
				DestinationLayer:  "./dst",
				AddressPrefix:     "module.shared",
				DestinationPrefix: "module.new",
				Resources: []ResourceMove{
					{From: "aws_instance.web"},
				},
			},
		},
	}

	errs := Validate(mf)
	if !hasError(errs, "address_prefix") {
		t.Errorf("expected validation error for conflicting address_prefix, got %v", errs)
	}
}

func TestValidate_AllResourcesWithSourceDestPrefix(t *testing.T) {
	mf := &MigrationFile{
		Description: "All resources with source/dest prefix (invalid)",
		Operations: []Operation{
			{
				Type:              OpMove,
				SourceLayer:       "./src",
				DestinationLayer:  "./dst",
				AllResources:      true,
				SourcePrefix:      "module.old",
				DestinationPrefix: "module.new",
			},
		},
	}

	errs := Validate(mf)
	if !hasError(errs, "address_prefix") {
		t.Errorf("expected validation error for prefix with all_resources, got %v", errs)
	}
}

func TestValidate_SourcePrefixOnRenameInvalid(t *testing.T) {
	mf := &MigrationFile{
		Description: "Source prefix on rename (invalid)",
		Operations: []Operation{
			{
				Type:         OpRename,
				Layer:        "./layer",
				SourcePrefix: "module.foo",
				Renames: []RenameEntry{
					{From: "aws_instance.old", To: "aws_instance.new"},
				},
			},
		},
	}

	errs := Validate(mf)
	if !hasError(errs, "source_prefix") {
		t.Errorf("expected validation error for source_prefix on rename, got %v", errs)
	}
}

func TestValidate_DestinationPrefixOnRemoveInvalid(t *testing.T) {
	mf := &MigrationFile{
		Description: "Destination prefix on remove (invalid)",
		Operations: []Operation{
			{
				Type:              OpRemove,
				Layer:             "./layer",
				DestinationPrefix: "module.foo",
				Entries: []RemoveEntry{
					{Address: "aws_instance.web"},
				},
			},
		},
	}

	errs := Validate(mf)
	if !hasError(errs, "destination_prefix") {
		t.Errorf("expected validation error for destination_prefix on remove, got %v", errs)
	}
}

func TestValidate_SourcePrefixOnImportInvalid(t *testing.T) {
	mf := &MigrationFile{
		Description: "Source prefix on import (invalid)",
		Operations: []Operation{
			{
				Type:         OpImport,
				Layer:        "./layer",
				SourcePrefix: "module.foo",
				Imports: []ImportEntry{
					{Address: "aws_instance.web", ID: "i-123"},
				},
			},
		},
	}

	errs := Validate(mf)
	if !hasError(errs, "source_prefix") {
		t.Errorf("expected validation error for source_prefix on import, got %v", errs)
	}
}

func TestValidate_UseMovedBlocksOnNonMoveInvalid(t *testing.T) {
	trueVal := true
	mf := &MigrationFile{
		Description: "use_moved_blocks on rename (invalid)",
		Operations: []Operation{
			{
				Type:           OpRename,
				Layer:          "./layer",
				UseMovedBlocks: &trueVal,
				Renames: []RenameEntry{
					{From: "aws_instance.old", To: "aws_instance.new"},
				},
			},
		},
	}

	errs := Validate(mf)
	if !hasError(errs, "use_moved_blocks") {
		t.Errorf("expected validation error for use_moved_blocks on rename, got %v", errs)
	}
}

func TestValidate_UseMovedBlocksFalseOnModuleMoveInvalid(t *testing.T) {
	falseVal := false
	mf := &MigrationFile{
		Description: "use_moved_blocks false on module move (invalid)",
		Operations: []Operation{
			{
				Type:             OpMove,
				SourceLayer:      "./src",
				DestinationLayer: "./src",
				Resources: []ResourceMove{
					{From: "module.foo", UseMovedBlocks: &falseVal},
				},
			},
		},
	}

	errs := Validate(mf)
	if !hasError(errs, "resources[0].use_moved_blocks") {
		t.Errorf("expected validation error for use_moved_blocks false on module move, got %v", errs)
	}
}

func TestValidate_UseMovedBlocksOnMoveValid(t *testing.T) {
	falseVal := false
	mf := &MigrationFile{
		Description: "use_moved_blocks false on move (valid)",
		Operations: []Operation{
			{
				Type:             OpMove,
				SourceLayer:      "./src",
				DestinationLayer: "./src",
				UseMovedBlocks:   &falseVal,
				Resources: []ResourceMove{
					{From: "aws_instance.web"},
				},
			},
		},
	}

	errs := Validate(mf)
	if len(errs) != 0 {
		t.Errorf("expected no errors, got %v", errs)
	}
}

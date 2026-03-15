package template

import (
	"testing"
)

func TestEvaluate_SimpleAttribute(t *testing.T) {
	ctx := &TemplateContext{
		Attributes: map[string]interface{}{
			"bucket": "my-bucket-name",
		},
	}

	result, err := Evaluate("{{ .Attributes.bucket }}", ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "my-bucket-name" {
		t.Errorf("expected %q, got %q", "my-bucket-name", result)
	}
}

func TestEvaluate_ResourceAddress(t *testing.T) {
	ctx := &TemplateContext{
		Address: `aws_s3_bucket.data["key-1"]`,
		Type:    "aws_s3_bucket",
		Name:    "data",
		Key:     "key-1",
	}

	result, err := Evaluate(`aws_s3_bucket.data["{{ .Key }}"]`, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != `aws_s3_bucket.data["key-1"]` {
		t.Errorf("expected %q, got %q", `aws_s3_bucket.data["key-1"]`, result)
	}
}

func TestEvaluate_ReplaceFunction(t *testing.T) {
	ctx := &TemplateContext{
		Key: "us-east-1-prod",
	}

	result, err := Evaluate(`{{ .Key | replace "-" "_" }}`, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "us_east_1_prod" {
		t.Errorf("expected %q, got %q", "us_east_1_prod", result)
	}
}

func TestEvaluate_TrimPrefixFunction(t *testing.T) {
	ctx := &TemplateContext{
		Key: "prefix-value",
	}

	result, err := Evaluate(`{{ .Key | trimPrefix "prefix-" }}`, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "value" {
		t.Errorf("expected %q, got %q", "value", result)
	}
}

func TestEvaluate_LowerFunction(t *testing.T) {
	ctx := &TemplateContext{
		Key: "MyKey",
	}

	result, err := Evaluate(`{{ .Key | lower }}`, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "mykey" {
		t.Errorf("expected %q, got %q", "mykey", result)
	}
}

func TestEvaluate_SplitJoinFunctions(t *testing.T) {
	ctx := &TemplateContext{
		Key: "a-b-c",
	}

	result, err := Evaluate(`{{ .Key | split "-" | join "_" }}`, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "a_b_c" {
		t.Errorf("expected %q, got %q", "a_b_c", result)
	}
}

func TestEvaluate_AtFunction(t *testing.T) {
	ctx := &TemplateContext{
		Attributes: map[string]interface{}{
			"catalog_resource_association_id": "subscription/abc-123/resourceGroup/my-rg",
		},
	}

	result, err := Evaluate(`{{ .Attributes.catalog_resource_association_id | split "/" | at 1 }}`, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "abc-123" {
		t.Errorf("expected %q, got %q", "abc-123", result)
	}
}

func TestEvaluate_AtFunction_InAddress(t *testing.T) {
	ctx := &TemplateContext{
		Attributes: map[string]interface{}{
			"catalog_resource_association_id": "subscription/abc-123/resourceGroup/my-rg",
		},
	}

	result, err := Evaluate(
		`customer_approval_customer_approval_{{ .Attributes.catalog_resource_association_id | split "/" | at 1 }}`,
		ctx,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := "customer_approval_customer_approval_abc-123"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestEvaluate_AtFunction_OutOfRange(t *testing.T) {
	ctx := &TemplateContext{
		Key: "a-b",
	}

	_, err := Evaluate(`{{ .Key | split "-" | at 5 }}`, ctx)
	if err == nil {
		t.Fatal("expected error for out-of-range index")
	}
}

func TestEvaluate_DefaultFunction(t *testing.T) {
	ctx := &TemplateContext{
		Key: "",
	}

	result, err := Evaluate(`{{ .Key | default "fallback" }}`, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "fallback" {
		t.Errorf("expected %q, got %q", "fallback", result)
	}

	ctx.Key = "actual"
	result, err = Evaluate(`{{ .Key | default "fallback" }}`, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "actual" {
		t.Errorf("expected %q, got %q", "actual", result)
	}
}

func TestEvaluate_QuoteFunction(t *testing.T) {
	ctx := &TemplateContext{
		Key: "my-key",
	}

	result, err := Evaluate(`{{ .Key | quote }}`, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != `"my-key"` {
		t.Errorf("expected %q, got %q", `"my-key"`, result)
	}
}

func TestEvaluate_AttrFunction_NestedLookup(t *testing.T) {
	ctx := &TemplateContext{
		Attributes: map[string]interface{}{
			"tags": map[string]interface{}{
				"Name": "my-resource",
			},
		},
	}

	result, err := Evaluate(`{{ attr .Attributes "tags" "Name" }}`, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "my-resource" {
		t.Errorf("expected %q, got %q", "my-resource", result)
	}
}

func TestEvaluate_AttrFunction_MissingKey(t *testing.T) {
	ctx := &TemplateContext{
		Attributes: map[string]interface{}{
			"tags": map[string]interface{}{},
		},
	}

	_, err := Evaluate(`{{ attr .Attributes "tags" "Missing" }}`, ctx)
	if err == nil {
		t.Fatal("expected error for missing nested key")
	}
}

func TestEvaluate_CompositeKeyTransformation(t *testing.T) {
	ctx := &TemplateContext{
		Key: "composite-abc-us-east-1",
		Attributes: map[string]interface{}{
			"bucket": "my-bucket-abc",
			"id":     "composite-abc-us-east-1",
			"tags": map[string]interface{}{
				"Name": "my-bucket-abc",
			},
		},
	}

	// Transform composite key to use bucket name
	result, err := Evaluate(`aws_s3_bucket.data["{{ .Attributes.bucket }}"]`, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != `aws_s3_bucket.data["my-bucket-abc"]` {
		t.Errorf("expected %q, got %q", `aws_s3_bucket.data["my-bucket-abc"]`, result)
	}
}

func TestEvaluate_ImportIDFromAttributes(t *testing.T) {
	ctx := &TemplateContext{
		Attributes: map[string]interface{}{
			"id":  "i-0abc123",
			"arn": "arn:aws:ec2:us-east-1:123456789:instance/i-0abc123",
		},
	}

	result, err := Evaluate("{{ .Attributes.id }}", ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "i-0abc123" {
		t.Errorf("expected %q, got %q", "i-0abc123", result)
	}
}

func TestEvaluate_InvalidTemplate(t *testing.T) {
	ctx := &TemplateContext{}
	_, err := Evaluate("{{ .Invalid }", ctx)
	if err == nil {
		t.Fatal("expected error for invalid template syntax")
	}
}

func TestEvaluate_MissingAttribute(t *testing.T) {
	ctx := &TemplateContext{
		Attributes: map[string]interface{}{},
	}
	_, err := Evaluate("{{ .Attributes.missing }}", ctx)
	if err == nil {
		t.Fatal("expected error for missing attribute")
	}
}

func TestEvaluate_PrintfFunction(t *testing.T) {
	ctx := &TemplateContext{
		Type: "aws_instance",
		Name: "web",
	}

	result, err := Evaluate(`{{ printf "%s.%s" .Type .Name }}`, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "aws_instance.web" {
		t.Errorf("expected %q, got %q", "aws_instance.web", result)
	}
}

func TestIsTemplate(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"aws_instance.web", false},
		{`aws_s3_bucket.data["key"]`, false},
		{"{{ .Key }}", true},
		{`aws_s3_bucket.data["{{ .Attributes.bucket }}"]`, true},
		{"{{ .Attributes.id }}", true},
		{"no templates here", false},
		{"{{ incomplete", false},
		{"incomplete }}", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := IsTemplate(tt.input)
			if result != tt.expected {
				t.Errorf("IsTemplate(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestEvaluate_HasPrefixFunction(t *testing.T) {
	ctx := &TemplateContext{
		Key: "prod-us-east-1",
	}

	result, err := Evaluate(`{{ if .Key | hasPrefix "prod" }}production{{ else }}other{{ end }}`, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "production" {
		t.Errorf("expected %q, got %q", "production", result)
	}
}

func TestEvaluate_ContainsFunction(t *testing.T) {
	ctx := &TemplateContext{
		Key: "my-special-key",
	}

	result, err := Evaluate(`{{ if .Key | contains "special" }}yes{{ else }}no{{ end }}`, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "yes" {
		t.Errorf("expected %q, got %q", "yes", result)
	}
}

func TestEvaluate_SanitizeKeyFunction(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		template string
		expected string
	}{
		{
			name:     "simple lowercase",
			input:    "Hello_World",
			template: `{{ .Key | sanitizeKey }}`,
			expected: "hello_world",
		},
		{
			name:     "replace special chars",
			input:    "my-key@with#special!chars",
			template: `{{ .Key | sanitizeKey }}`,
			expected: "my_key_with_special_chars",
		},
		{
			name:     "collapse multiple non-alnum",
			input:    "foo---bar___baz",
			template: `{{ .Key | sanitizeKey }}`,
			expected: "foo_bar_baz",
		},
		{
			name:     "strip leading and trailing",
			input:    "---hello---",
			template: `{{ .Key | sanitizeKey }}`,
			expected: "hello",
		},
		{
			name:     "already clean",
			input:    "clean_key_123",
			template: `{{ .Key | sanitizeKey }}`,
			expected: "clean_key_123",
		},
		{
			name:     "mixed case with spaces and dots",
			input:    "Access Package.Admin Role",
			template: `{{ .Key | sanitizeKey }}`,
			expected: "access_package_admin_role",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &TemplateContext{Key: tt.input}
			result, err := Evaluate(tt.template, ctx)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestEvaluate_FormatKeyFunction(t *testing.T) {
	ctx := &TemplateContext{
		Attributes: map[string]interface{}{
			"access_package_key": "Engineering Access",
			"role":               "Admin-Role",
		},
	}

	result, err := Evaluate(
		`{{ formatKey "%s_%s" .Attributes.access_package_key .Attributes.role }}`,
		ctx,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// lower(replace(format("%s_%s", "Engineering Access", "Admin-Role"), "/[^a-zA-Z0-9]+/", "_"))
	// = lower(replace("Engineering Access_Admin-Role", ...))
	// = lower("Engineering_Access_Admin_Role")
	// = "engineering_access_admin_role"
	expected := "engineering_access_admin_role"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestEvaluate_FormatKeyFunction_ComplexKeys(t *testing.T) {
	tests := []struct {
		name     string
		attrs    map[string]interface{}
		template string
		expected string
	}{
		{
			name: "ids with hyphens",
			attrs: map[string]interface{}{
				"package_id": "pkg-abc-123",
				"role_id":    "role-def-456",
			},
			template: `{{ formatKey "%s_%s" .Attributes.package_id .Attributes.role_id }}`,
			expected: "pkg_abc_123_role_def_456",
		},
		{
			name: "unicode and special chars",
			attrs: map[string]interface{}{
				"name":  "Über-Admin",
				"scope": "org/unit",
			},
			template: `{{ formatKey "%s_%s" .Attributes.name .Attributes.scope }}`,
			expected: "ber_admin_org_unit",
		},
		{
			name: "single value",
			attrs: map[string]interface{}{
				"name": "My Resource Name",
			},
			template: `{{ formatKey "%s" .Attributes.name }}`,
			expected: "my_resource_name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &TemplateContext{Attributes: tt.attrs}
			result, err := Evaluate(tt.template, ctx)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestEvaluate_SanitizeKeyWithPrintf(t *testing.T) {
	// Verify that piping printf through sanitizeKey gives the same result as formatKey
	ctx := &TemplateContext{
		Attributes: map[string]interface{}{
			"access_package_key": "Engineering Access",
			"role":               "Admin-Role",
		},
	}

	piped, err := Evaluate(
		`{{ printf "%s_%s" .Attributes.access_package_key .Attributes.role | sanitizeKey }}`,
		ctx,
	)
	if err != nil {
		t.Fatalf("piped: unexpected error: %v", err)
	}

	direct, err := Evaluate(
		`{{ formatKey "%s_%s" .Attributes.access_package_key .Attributes.role }}`,
		ctx,
	)
	if err != nil {
		t.Fatalf("direct: unexpected error: %v", err)
	}

	if piped != direct {
		t.Errorf("piped (%q) != direct (%q)", piped, direct)
	}
}

func TestEvaluate_RegexReplaceFunction(t *testing.T) {
	ctx := &TemplateContext{Key: "hello-world_123!foo"}

	result, err := Evaluate(`{{ .Key | regexReplace "[^a-z0-9]+" "_" }}`, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "hello_world_123_foo" {
		t.Errorf("expected %q, got %q", "hello_world_123_foo", result)
	}
}

func TestEvaluate_RegexReplaceFunction_InvalidPattern(t *testing.T) {
	ctx := &TemplateContext{Key: "test"}
	_, err := Evaluate(`{{ .Key | regexReplace "[invalid" "x" }}`, ctx)
	if err == nil {
		t.Fatal("expected error for invalid regex pattern")
	}
}

func TestEvaluate_FormatKeyInAddress(t *testing.T) {
	// End-to-end: use formatKey inside a destination address template
	ctx := &TemplateContext{
		Attributes: map[string]interface{}{
			"access_package_key": "Finance Team",
			"role":               "Reader",
		},
	}

	result, err := Evaluate(
		`aws_resource.items["{{ formatKey "%s_%s" .Attributes.access_package_key .Attributes.role }}"]`,
		ctx,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := `aws_resource.items["finance_team_reader"]`
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

// --- Item/ItemIndex expansion tests ---

func TestEvaluate_ItemFieldAccess(t *testing.T) {
	ctx := &TemplateContext{
		Key: "myapp",
		Attributes: map[string]interface{}{
			"id": "app-123",
		},
		Item: map[string]interface{}{
			"resource_app_id": "00000003-0000-0000-c000-000000000000",
			"resource_access": []interface{}{"Scope.Read", "Scope.Write"},
		},
		ItemIndex: 0,
	}

	result, err := Evaluate(`{{ .Item.resource_app_id }}`, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "00000003-0000-0000-c000-000000000000" {
		t.Errorf("expected graph API app ID, got %q", result)
	}
}

func TestEvaluate_ItemIndexAccess(t *testing.T) {
	ctx := &TemplateContext{
		Key:       "myapp",
		Item:      map[string]interface{}{"id": "abc"},
		ItemIndex: 2,
	}

	result, err := Evaluate(`{{ .Key }}_item{{ .ItemIndex }}`, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "myapp_item2" {
		t.Errorf("expected %q, got %q", "myapp_item2", result)
	}
}

func TestEvaluate_ItemCombinedWithAttributes(t *testing.T) {
	ctx := &TemplateContext{
		Attributes: map[string]interface{}{
			"id": "app-456",
		},
		Item: map[string]interface{}{
			"resource_app_id": "graph-api-id",
		},
		ItemIndex: 1,
	}

	result, err := Evaluate(`{{ .Attributes.id }}/apiAccess/{{ .Item.resource_app_id }}`, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := "app-456/apiAccess/graph-api-id"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestEvaluate_ItemNilDoesNotBreak(t *testing.T) {
	// When Item is nil (no expansion), templates that don't reference .Item still work.
	ctx := &TemplateContext{
		Key: "mykey",
	}

	result, err := Evaluate(`{{ .Key }}`, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "mykey" {
		t.Errorf("expected %q, got %q", "mykey", result)
	}
}

func TestEvaluate_ItemZeroIndexDefault(t *testing.T) {
	// When ItemIndex is not set (0), it's still accessible.
	ctx := &TemplateContext{
		Item:      map[string]interface{}{"name": "test"},
		ItemIndex: 0,
	}

	result, err := Evaluate(`{{ .ItemIndex }}`, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "0" {
		t.Errorf("expected %q, got %q", "0", result)
	}
}

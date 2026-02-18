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

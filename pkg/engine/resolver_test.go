package engine

import (
	"context"
	"testing"

	tfjson "github.com/hashicorp/terraform-json"

	"github.com/redtenant/tfmigrate/internal/testutil"
	"github.com/redtenant/tfmigrate/pkg/state"
)

func TestResolveImportID_Explicit(t *testing.T) {
	resolver := NewResolver(nil)

	resource := &state.ResourceInfo{
		Address: "aws_instance.web",
	}

	id, err := resolver.ResolveImportID(resource, "i-explicit-id")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "i-explicit-id" {
		t.Errorf("expected %q, got %q", "i-explicit-id", id)
	}
}

func TestResolveImportID_FromState(t *testing.T) {
	resolver := NewResolver(nil)

	resource := &state.ResourceInfo{
		Address: "aws_instance.web",
		Attributes: map[string]interface{}{
			"id": "i-from-state",
		},
	}

	id, err := resolver.ResolveImportID(resource, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "i-from-state" {
		t.Errorf("expected %q, got %q", "i-from-state", id)
	}
}

func TestResolveImportID_Template(t *testing.T) {
	resolver := NewResolver(nil)

	resource := &state.ResourceInfo{
		Address: "aws_instance.web",
		Type:    "aws_instance",
		Name:    "web",
		Attributes: map[string]interface{}{
			"id":  "i-123",
			"arn": "arn:aws:ec2:us-east-1:123:instance/i-123",
		},
	}

	id, err := resolver.ResolveImportID(resource, "{{ .Attributes.arn }}")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "arn:aws:ec2:us-east-1:123:instance/i-123" {
		t.Errorf("expected ARN, got %q", id)
	}
}

func TestResolveImportID_NoIDAttribute(t *testing.T) {
	resolver := NewResolver(nil)

	resource := &state.ResourceInfo{
		Address:    "aws_instance.web",
		Attributes: map[string]interface{}{},
	}

	_, err := resolver.ResolveImportID(resource, "")
	if err == nil {
		t.Fatal("expected error when no id attribute exists")
	}
}

func TestResolveImportID_NilAttributes(t *testing.T) {
	resolver := NewResolver(nil)

	resource := &state.ResourceInfo{
		Address: "aws_instance.web",
	}

	_, err := resolver.ResolveImportID(resource, "")
	if err == nil {
		t.Fatal("expected error when attributes are nil")
	}
}

func TestLookupResources(t *testing.T) {
	mock := testutil.NewMockStateReader(map[string]*tfjson.State{
		"./layers/data": testutil.BuildState(
			testutil.NewResource(
				`aws_s3_bucket.data["alpha"]`, "aws_s3_bucket", "data", "alpha",
				map[string]interface{}{"id": "id-alpha"},
			),
			testutil.NewResource(
				`aws_s3_bucket.data["beta"]`, "aws_s3_bucket", "data", "beta",
				map[string]interface{}{"id": "id-beta"},
			),
			testutil.NewResource(
				`aws_s3_bucket.data["gamma"]`, "aws_s3_bucket", "data", "gamma",
				map[string]interface{}{"id": "id-gamma"},
			),
		),
	})

	resolver := NewResolver(mock)
	ctx := context.Background()

	resources, err := resolver.LookupResources(ctx, "./layers/data", "aws_s3_bucket.data")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(resources) != 3 {
		t.Fatalf("expected 3 resources, got %d", len(resources))
	}

	keySet := make(map[string]bool)
	for _, r := range resources {
		keySet[r.Key] = true
	}
	for _, expected := range []string{"alpha", "beta", "gamma"} {
		if !keySet[expected] {
			t.Errorf("expected key %q not found in resources", expected)
		}
	}
}

func TestLookupResource_Single(t *testing.T) {
	mock := testutil.NewMockStateReader(map[string]*tfjson.State{
		"./src": testutil.BuildState(
			testutil.NewResource("aws_instance.web", "aws_instance", "web", nil, map[string]interface{}{
				"id": "i-123",
			}),
		),
	})

	resolver := NewResolver(mock)
	ctx := context.Background()

	resource, err := resolver.LookupResource(ctx, "./src", "aws_instance.web")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resource.Address != "aws_instance.web" {
		t.Errorf("expected address %q, got %q", "aws_instance.web", resource.Address)
	}
}

func TestLookupResources_NoMatches(t *testing.T) {
	mock := testutil.NewMockStateReader(map[string]*tfjson.State{
		"./layers/data": testutil.BuildState(
			testutil.NewResource("aws_instance.web", "aws_instance", "web", nil, nil),
		),
	})

	resolver := NewResolver(mock)
	ctx := context.Background()

	_, err := resolver.LookupResources(ctx, "./layers/data", "aws_s3_bucket.data")
	if err == nil {
		t.Fatal("expected error when no resources match")
	}
}

func TestEvaluateTemplate_LiteralString(t *testing.T) {
	resolver := NewResolver(nil)

	resource := &state.ResourceInfo{
		Address: "aws_instance.web",
		Key:     "my_key",
	}

	result, err := resolver.EvaluateTemplate("plain_value", resource)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "plain_value" {
		t.Errorf("expected %q, got %q", "plain_value", result)
	}
}

func TestEvaluateTemplate_WithTemplate(t *testing.T) {
	resolver := NewResolver(nil)

	resource := &state.ResourceInfo{
		Address: "aws_instance.web",
		Type:    "aws_instance",
		Name:    "web",
		Key:     "eng_admin",
		Attributes: map[string]interface{}{
			"id": "i-123",
		},
	}

	result, err := resolver.EvaluateTemplate(`{{ .Key | trimPrefix "eng_" }}`, resource)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "admin" {
		t.Errorf("expected %q, got %q", "admin", result)
	}
}

func TestEvaluateTemplate_WithAttributes(t *testing.T) {
	resolver := NewResolver(nil)

	resource := &state.ResourceInfo{
		Address: "aws_instance.web",
		Key:     "my_key",
		Attributes: map[string]interface{}{
			"id":     "resource-id",
			"bucket": "my-bucket",
		},
	}

	result, err := resolver.EvaluateTemplate("{{ .Attributes.bucket }}", resource)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "my-bucket" {
		t.Errorf("expected %q, got %q", "my-bucket", result)
	}
}

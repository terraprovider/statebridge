package engine

import (
	"context"
	"testing"

	tfjson "github.com/hashicorp/terraform-json"

	"github.com/redtenant/tfmigrate/internal/testutil"
	"github.com/redtenant/tfmigrate/pkg/state"
)

func TestResolveImportID_Explicit(t *testing.T) {
	resolver := NewResolver(nil) // no state reader needed

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

func TestExpandWildcard(t *testing.T) {
	mock := testutil.NewMockStateReader(map[string]*tfjson.State{
		"./layers/data": testutil.BuildState(
			testutil.NewResource(
				`aws_s3_bucket.data["key-a"]`, "aws_s3_bucket", "data", "key-a",
				map[string]interface{}{
					"id":     "bucket-a-id",
					"bucket": "bucket-a",
				},
			),
			testutil.NewResource(
				`aws_s3_bucket.data["key-b"]`, "aws_s3_bucket", "data", "key-b",
				map[string]interface{}{
					"id":     "bucket-b-id",
					"bucket": "bucket-b",
				},
			),
		),
	})

	resolver := NewResolver(mock)
	ctx := context.Background()

	instances, err := resolver.ExpandWildcard(
		ctx,
		"./layers/data",
		"aws_s3_bucket.data[*]",
		`aws_s3_bucket.data["{{ .Attributes.bucket }}"]`,
		"{{ .Attributes.id }}",
	)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(instances) != 2 {
		t.Fatalf("expected 2 instances, got %d", len(instances))
	}

	// Verify first instance
	if instances[0].DestAddress != `aws_s3_bucket.data["bucket-a"]` {
		t.Errorf("instance[0] dest address = %q, want %q", instances[0].DestAddress, `aws_s3_bucket.data["bucket-a"]`)
	}
	if instances[0].ImportID != "bucket-a-id" {
		t.Errorf("instance[0] import ID = %q, want %q", instances[0].ImportID, "bucket-a-id")
	}

	// Verify second instance
	if instances[1].DestAddress != `aws_s3_bucket.data["bucket-b"]` {
		t.Errorf("instance[1] dest address = %q, want %q", instances[1].DestAddress, `aws_s3_bucket.data["bucket-b"]`)
	}
	if instances[1].ImportID != "bucket-b-id" {
		t.Errorf("instance[1] import ID = %q, want %q", instances[1].ImportID, "bucket-b-id")
	}
}

func TestExpandWildcard_NoMatches(t *testing.T) {
	mock := testutil.NewMockStateReader(map[string]*tfjson.State{
		"./layers/data": testutil.BuildState(
			testutil.NewResource("aws_instance.web", "aws_instance", "web", nil, nil),
		),
	})

	resolver := NewResolver(mock)
	ctx := context.Background()

	_, err := resolver.ExpandWildcard(
		ctx,
		"./layers/data",
		"aws_s3_bucket.data[*]",
		`aws_s3_bucket.data["{{ .Key }}"]`,
		"",
	)

	if err == nil {
		t.Fatal("expected error when no resources match wildcard")
	}
}

func TestResolveSingleMove_ExplicitImportID(t *testing.T) {
	mock := testutil.NewMockStateReader(map[string]*tfjson.State{
		"./src": testutil.BuildState(
			testutil.NewResource("aws_instance.web", "aws_instance", "web", nil, map[string]interface{}{
				"id": "i-123",
			}),
		),
	})

	resolver := NewResolver(mock)
	ctx := context.Background()

	inst, err := resolver.ResolveSingleMove(ctx, "./src", "aws_instance.web", "aws_instance.web", "i-explicit")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if inst.ImportID != "i-explicit" {
		t.Errorf("expected import ID %q, got %q", "i-explicit", inst.ImportID)
	}
}

func TestResolveSingleMove_AutoImportID(t *testing.T) {
	mock := testutil.NewMockStateReader(map[string]*tfjson.State{
		"./src": testutil.BuildState(
			testutil.NewResource("aws_instance.web", "aws_instance", "web", nil, map[string]interface{}{
				"id": "i-auto-resolved",
			}),
		),
	})

	resolver := NewResolver(mock)
	ctx := context.Background()

	inst, err := resolver.ResolveSingleMove(ctx, "./src", "aws_instance.web", "aws_instance.web", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if inst.ImportID != "i-auto-resolved" {
		t.Errorf("expected import ID %q, got %q", "i-auto-resolved", inst.ImportID)
	}
}

func TestResolveSingleMove_ResourceNotInState_WithExplicitID(t *testing.T) {
	mock := testutil.NewMockStateReader(map[string]*tfjson.State{
		"./src": testutil.BuildState(), // empty state
	})

	resolver := NewResolver(mock)
	ctx := context.Background()

	inst, err := resolver.ResolveSingleMove(ctx, "./src", "aws_instance.web", "aws_instance.web", "i-explicit")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if inst.ImportID != "i-explicit" {
		t.Errorf("expected import ID %q, got %q", "i-explicit", inst.ImportID)
	}
}

func TestResolveSingleMove_ResourceNotInState_NoID(t *testing.T) {
	mock := testutil.NewMockStateReader(map[string]*tfjson.State{
		"./src": testutil.BuildState(), // empty state
	})

	resolver := NewResolver(mock)
	ctx := context.Background()

	_, err := resolver.ResolveSingleMove(ctx, "./src", "aws_instance.web", "aws_instance.web", "")
	if err == nil {
		t.Fatal("expected error when resource not in state and no explicit ID")
	}
}

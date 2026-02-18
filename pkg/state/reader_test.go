package state

import (
	"context"
	"testing"

	tfjson "github.com/hashicorp/terraform-json"

	"github.com/redtenant/tfmigrate/internal/testutil"
)

func TestFlattenState_NilState(t *testing.T) {
	result := FlattenState(nil)
	if result != nil {
		t.Errorf("expected nil, got %v", result)
	}
}

func TestFlattenState_EmptyState(t *testing.T) {
	s := &tfjson.State{
		Values: &tfjson.StateValues{
			RootModule: &tfjson.StateModule{},
		},
	}
	result := FlattenState(s)
	if len(result) != 0 {
		t.Errorf("expected 0 resources, got %d", len(result))
	}
}

func TestFlattenState_RootResources(t *testing.T) {
	s := testutil.BuildState(
		testutil.NewResource("aws_instance.web", "aws_instance", "web", nil, map[string]interface{}{
			"id": "i-123",
		}),
		testutil.NewResource("aws_s3_bucket.data", "aws_s3_bucket", "data", nil, map[string]interface{}{
			"id":     "my-bucket",
			"bucket": "my-bucket",
		}),
	)

	result := FlattenState(s)
	if len(result) != 2 {
		t.Fatalf("expected 2 resources, got %d", len(result))
	}

	if result[0].Address != "aws_instance.web" {
		t.Errorf("expected address %q, got %q", "aws_instance.web", result[0].Address)
	}
	if result[0].Type != "aws_instance" {
		t.Errorf("expected type %q, got %q", "aws_instance", result[0].Type)
	}
	if result[0].Attributes["id"] != "i-123" {
		t.Errorf("expected id %q, got %q", "i-123", result[0].Attributes["id"])
	}
}

func TestFlattenState_NestedModules(t *testing.T) {
	s := testutil.BuildStateWithModules(
		[]*tfjson.StateResource{
			testutil.NewResource("aws_instance.root", "aws_instance", "root", nil, nil),
		},
		[]*tfjson.StateModule{
			{
				Address: "module.vpc",
				Resources: []*tfjson.StateResource{
					testutil.NewResource("module.vpc.aws_vpc.main", "aws_vpc", "main", nil, nil),
				},
				ChildModules: []*tfjson.StateModule{
					{
						Address: "module.vpc.module.subnets",
						Resources: []*tfjson.StateResource{
							testutil.NewResource("module.vpc.module.subnets.aws_subnet.public", "aws_subnet", "public", nil, nil),
						},
					},
				},
			},
		},
	)

	result := FlattenState(s)
	if len(result) != 3 {
		t.Fatalf("expected 3 resources, got %d", len(result))
	}

	addresses := make(map[string]bool)
	for _, r := range result {
		addresses[r.Address] = true
	}

	expected := []string{
		"aws_instance.root",
		"module.vpc.aws_vpc.main",
		"module.vpc.module.subnets.aws_subnet.public",
	}
	for _, addr := range expected {
		if !addresses[addr] {
			t.Errorf("expected address %q not found", addr)
		}
	}
}

func TestFlattenState_ForEachResources(t *testing.T) {
	s := testutil.BuildState(
		testutil.NewResource(`aws_s3_bucket.data["key-a"]`, "aws_s3_bucket", "data", "key-a", map[string]interface{}{
			"id":     "key-a-bucket",
			"bucket": "bucket-a",
		}),
		testutil.NewResource(`aws_s3_bucket.data["key-b"]`, "aws_s3_bucket", "data", "key-b", map[string]interface{}{
			"id":     "key-b-bucket",
			"bucket": "bucket-b",
		}),
	)

	result := FlattenState(s)
	if len(result) != 2 {
		t.Fatalf("expected 2 resources, got %d", len(result))
	}

	if result[0].Key != "key-a" {
		t.Errorf("expected key %q, got %q", "key-a", result[0].Key)
	}
	if result[1].Key != "key-b" {
		t.Errorf("expected key %q, got %q", "key-b", result[1].Key)
	}
}

func TestFlattenState_CountResources(t *testing.T) {
	s := testutil.BuildState(
		testutil.NewResource("aws_instance.web[0]", "aws_instance", "web", float64(0), nil),
		testutil.NewResource("aws_instance.web[1]", "aws_instance", "web", float64(1), nil),
	)

	result := FlattenState(s)
	if len(result) != 2 {
		t.Fatalf("expected 2 resources, got %d", len(result))
	}

	if result[0].Key != "0" {
		t.Errorf("expected key %q, got %q", "0", result[0].Key)
	}
	if result[1].Key != "1" {
		t.Errorf("expected key %q, got %q", "1", result[1].Key)
	}
}

func TestLookupResource_Found(t *testing.T) {
	s := testutil.BuildState(
		testutil.NewResource("aws_instance.web", "aws_instance", "web", nil, map[string]interface{}{"id": "i-123"}),
		testutil.NewResource("aws_s3_bucket.data", "aws_s3_bucket", "data", nil, nil),
	)

	r, err := LookupResource(s, "aws_instance.web")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.Address != "aws_instance.web" {
		t.Errorf("expected address %q, got %q", "aws_instance.web", r.Address)
	}
}

func TestLookupResource_NotFound(t *testing.T) {
	s := testutil.BuildState(
		testutil.NewResource("aws_instance.web", "aws_instance", "web", nil, nil),
	)

	_, err := LookupResource(s, "aws_instance.missing")
	if err == nil {
		t.Fatal("expected error for missing resource")
	}
}

func TestLookupResourcesByPrefix_Found(t *testing.T) {
	s := testutil.BuildState(
		testutil.NewResource(`aws_s3_bucket.data["key-a"]`, "aws_s3_bucket", "data", "key-a", nil),
		testutil.NewResource(`aws_s3_bucket.data["key-b"]`, "aws_s3_bucket", "data", "key-b", nil),
		testutil.NewResource("aws_instance.web", "aws_instance", "web", nil, nil),
	)

	matches, err := LookupResourcesByPrefix(s, "aws_s3_bucket.data")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(matches) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(matches))
	}
}

func TestLookupResourcesByPrefix_IncludesExactMatch(t *testing.T) {
	s := testutil.BuildState(
		testutil.NewResource("aws_instance.web", "aws_instance", "web", nil, nil),
	)

	matches, err := LookupResourcesByPrefix(s, "aws_instance.web")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matches))
	}
}

func TestLookupResourcesByPrefix_NotFound(t *testing.T) {
	s := testutil.BuildState(
		testutil.NewResource("aws_instance.web", "aws_instance", "web", nil, nil),
	)

	_, err := LookupResourcesByPrefix(s, "aws_s3_bucket.missing")
	if err == nil {
		t.Fatal("expected error for no matches")
	}
}

func TestCachedStateReader_CachesResults(t *testing.T) {
	mock := testutil.NewMockStateReader(map[string]*tfjson.State{
		"./layer-a": testutil.BuildState(
			testutil.NewResource("aws_instance.web", "aws_instance", "web", nil, nil),
		),
	})

	cached := NewCachedStateReader(mock)
	ctx := context.Background()

	// First read
	s1, err := cached.ReadState(ctx, "./layer-a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Second read (should be cached)
	s2, err := cached.ReadState(ctx, "./layer-a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if s1 != s2 {
		t.Error("expected same state pointer from cache")
	}

	if mock.ReadCount["./layer-a"] != 1 {
		t.Errorf("expected 1 read, got %d", mock.ReadCount["./layer-a"])
	}
}

func TestCachedStateReader_DifferentLayers(t *testing.T) {
	mock := testutil.NewMockStateReader(map[string]*tfjson.State{
		"./layer-a": testutil.BuildState(),
		"./layer-b": testutil.BuildState(),
	})

	cached := NewCachedStateReader(mock)
	ctx := context.Background()

	_, err := cached.ReadState(ctx, "./layer-a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = cached.ReadState(ctx, "./layer-b")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mock.ReadCount["./layer-a"] != 1 {
		t.Errorf("expected 1 read for layer-a, got %d", mock.ReadCount["./layer-a"])
	}
	if mock.ReadCount["./layer-b"] != 1 {
		t.Errorf("expected 1 read for layer-b, got %d", mock.ReadCount["./layer-b"])
	}
}

func TestFormatIndex(t *testing.T) {
	tests := []struct {
		name     string
		index    interface{}
		expected string
	}{
		{"nil", nil, ""},
		{"string", "my-key", "my-key"},
		{"float64", float64(42), "42"},
		{"float64_zero", float64(0), "0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatIndex(tt.index)
			if result != tt.expected {
				t.Errorf("formatIndex(%v) = %q, want %q", tt.index, result, tt.expected)
			}
		})
	}
}

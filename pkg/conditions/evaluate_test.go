package conditions

import (
	"context"
	"testing"

	tfjson "github.com/hashicorp/terraform-json"

	"github.com/redtenant/tfmigrate/pkg/generator"
)

func buildState(resources ...*tfjson.StateResource) *tfjson.State {
	return &tfjson.State{
		FormatVersion: "1.0",
		Values: &tfjson.StateValues{
			RootModule: &tfjson.StateModule{
				Resources: resources,
			},
		},
	}
}

func newResource(address string) *tfjson.StateResource {
	return &tfjson.StateResource{
		Address:      address,
		Mode:         tfjson.ManagedResourceMode,
		Type:         "aws_instance",
		Name:         "web",
		ProviderName: "registry.opentofu.org/hashicorp/aws",
	}
}

func TestEvaluateMetadataConditions_NilMeta(t *testing.T) {
	result, err := EvaluateMetadataConditions(context.Background(), nil, nil, "/layer")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result {
		t.Error("expected true for nil metadata")
	}
}

func TestEvaluateMetadataConditions_NilConditions(t *testing.T) {
	meta := &generator.MigrationMetadata{Conditions: nil}
	result, err := EvaluateMetadataConditions(context.Background(), meta, nil, "/layer")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result {
		t.Error("expected true for nil conditions")
	}
}

func TestEvaluateMetadataConditions_ResourcesExistMet(t *testing.T) {
	meta := &generator.MigrationMetadata{
		Conditions: &generator.MetadataCondition{
			ResourcesExist: []generator.MetadataResourceCheck{
				{Layer: ".", Addresses: []string{"aws_instance.web"}},
			},
		},
	}

	readState := func(_ context.Context, _ string) (*tfjson.State, error) {
		return buildState(newResource("aws_instance.web")), nil
	}

	result, err := EvaluateMetadataConditions(context.Background(), meta, readState, "/layer")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result {
		t.Error("expected true when resource exists")
	}
}

func TestEvaluateMetadataConditions_ResourcesExistNotMet(t *testing.T) {
	meta := &generator.MigrationMetadata{
		Conditions: &generator.MetadataCondition{
			ResourcesExist: []generator.MetadataResourceCheck{
				{Layer: ".", Addresses: []string{"aws_instance.missing"}},
			},
		},
	}

	readState := func(_ context.Context, _ string) (*tfjson.State, error) {
		return buildState(newResource("aws_instance.web")), nil
	}

	result, err := EvaluateMetadataConditions(context.Background(), meta, readState, "/layer")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result {
		t.Error("expected false when resource does not exist")
	}
}

func TestEvaluateMetadataConditions_ResourcesNotExistMet(t *testing.T) {
	meta := &generator.MigrationMetadata{
		Conditions: &generator.MetadataCondition{
			ResourcesNotExist: []generator.MetadataResourceCheck{
				{Layer: ".", Addresses: []string{"aws_instance.missing"}},
			},
		},
	}

	readState := func(_ context.Context, _ string) (*tfjson.State, error) {
		return buildState(newResource("aws_instance.web")), nil
	}

	result, err := EvaluateMetadataConditions(context.Background(), meta, readState, "/layer")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result {
		t.Error("expected true when resource does not exist")
	}
}

func TestEvaluateMetadataConditions_ResourcesNotExistNotMet(t *testing.T) {
	meta := &generator.MigrationMetadata{
		Conditions: &generator.MetadataCondition{
			ResourcesNotExist: []generator.MetadataResourceCheck{
				{Layer: ".", Addresses: []string{"aws_instance.web"}},
			},
		},
	}

	readState := func(_ context.Context, _ string) (*tfjson.State, error) {
		return buildState(newResource("aws_instance.web")), nil
	}

	result, err := EvaluateMetadataConditions(context.Background(), meta, readState, "/layer")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result {
		t.Error("expected false when resource exists but should not")
	}
}

func TestEvaluateMetadataConditions_CrossLayerTreatedAsMet(t *testing.T) {
	meta := &generator.MigrationMetadata{
		Conditions: &generator.MetadataCondition{
			ResourcesExist: []generator.MetadataResourceCheck{
				{Layer: "../other", Addresses: []string{"aws_instance.web"}},
			},
		},
	}

	// readState should not be called for cross-layer conditions
	readState := func(_ context.Context, _ string) (*tfjson.State, error) {
		t.Error("readState should not be called for cross-layer conditions")
		return nil, nil
	}

	result, err := EvaluateMetadataConditions(context.Background(), meta, readState, "/layer")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result {
		t.Error("expected true for cross-layer condition (treated as met)")
	}
}

func TestEvaluateMetadataConditions_StateReadError(t *testing.T) {
	meta := &generator.MigrationMetadata{
		Conditions: &generator.MetadataCondition{
			ResourcesExist: []generator.MetadataResourceCheck{
				{Layer: ".", Addresses: []string{"aws_instance.web"}},
			},
		},
	}

	readState := func(_ context.Context, _ string) (*tfjson.State, error) {
		return nil, &stateReadError{msg: "connection refused"}
	}

	_, err := EvaluateMetadataConditions(context.Background(), meta, readState, "/layer")
	if err == nil {
		t.Fatal("expected error on state read failure")
	}
}

type stateReadError struct{ msg string }

func (e *stateReadError) Error() string { return e.msg }

func TestEvaluateMetadataConditions_BothConditionTypes(t *testing.T) {
	meta := &generator.MigrationMetadata{
		Conditions: &generator.MetadataCondition{
			ResourcesExist: []generator.MetadataResourceCheck{
				{Layer: ".", Addresses: []string{"aws_instance.web"}},
			},
			ResourcesNotExist: []generator.MetadataResourceCheck{
				{Layer: ".", Addresses: []string{"aws_instance.missing"}},
			},
		},
	}

	readState := func(_ context.Context, _ string) (*tfjson.State, error) {
		return buildState(newResource("aws_instance.web")), nil
	}

	result, err := EvaluateMetadataConditions(context.Background(), meta, readState, "/layer")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result {
		t.Error("expected true when both condition types pass")
	}
}

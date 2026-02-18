package state

import (
	"testing"

	tfjson "github.com/hashicorp/terraform-json"
)

func buildTestState(resources ...*tfjson.StateResource) *tfjson.State {
	return &tfjson.State{
		FormatVersion: "1.0",
		Values: &tfjson.StateValues{
			RootModule: &tfjson.StateModule{
				Resources: resources,
			},
		},
	}
}

func newTestResource(address string, index interface{}) *tfjson.StateResource {
	return &tfjson.StateResource{
		Address:         address,
		Mode:            tfjson.ManagedResourceMode,
		Type:            "aws_instance",
		Name:            "web",
		Index:           index,
		ProviderName:    "registry.opentofu.org/hashicorp/aws",
		AttributeValues: map[string]interface{}{"id": "test-id"},
	}
}

func TestResourceExists_ExactMatch(t *testing.T) {
	s := buildTestState(newTestResource("aws_instance.web", nil))

	if !ResourceExists(s, "aws_instance.web") {
		t.Error("expected aws_instance.web to exist")
	}
}

func TestResourceExists_NotFound(t *testing.T) {
	s := buildTestState(newTestResource("aws_instance.web", nil))

	if ResourceExists(s, "aws_instance.api") {
		t.Error("expected aws_instance.api to not exist")
	}
}

func TestResourceExists_BaseAddressMatchesForEach(t *testing.T) {
	s := buildTestState(
		newTestResource(`aws_instance.web["key-a"]`, "key-a"),
		newTestResource(`aws_instance.web["key-b"]`, "key-b"),
	)

	if !ResourceExists(s, "aws_instance.web") {
		t.Error("expected base address aws_instance.web to match for_each instances")
	}
}

func TestResourceExists_FullAddressWithKey(t *testing.T) {
	s := buildTestState(
		newTestResource(`aws_instance.web["key-a"]`, "key-a"),
		newTestResource(`aws_instance.web["key-b"]`, "key-b"),
	)

	if !ResourceExists(s, `aws_instance.web["key-a"]`) {
		t.Error("expected full address with key to match")
	}
	if ResourceExists(s, `aws_instance.web["key-c"]`) {
		t.Error("expected missing key to not match")
	}
}

func TestResourceExists_NilState(t *testing.T) {
	if ResourceExists(nil, "aws_instance.web") {
		t.Error("expected nil state to return false")
	}
}

func TestResourceExists_NilValues(t *testing.T) {
	s := &tfjson.State{FormatVersion: "1.0"}
	if ResourceExists(s, "aws_instance.web") {
		t.Error("expected nil values to return false")
	}
}

func TestResourceExists_NilRootModule(t *testing.T) {
	s := &tfjson.State{
		FormatVersion: "1.0",
		Values:        &tfjson.StateValues{},
	}
	if ResourceExists(s, "aws_instance.web") {
		t.Error("expected nil root module to return false")
	}
}

func TestResourceExists_EmptyState(t *testing.T) {
	s := buildTestState() // no resources
	if ResourceExists(s, "aws_instance.web") {
		t.Error("expected empty state to return false")
	}
}

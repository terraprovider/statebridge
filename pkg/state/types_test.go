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

func newTestDataResource(address string) *tfjson.StateResource {
	return &tfjson.StateResource{
		Address:         address,
		Mode:            tfjson.DataResourceMode,
		Type:            "aws_ami",
		Name:            "latest",
		ProviderName:    "registry.opentofu.org/hashicorp/aws",
		AttributeValues: map[string]interface{}{"id": "ami-123"},
	}
}

func newTestModuleResource(address string) *tfjson.StateResource {
	return &tfjson.StateResource{
		Address:         address,
		Mode:            tfjson.ManagedResourceMode,
		Type:            "aws_instance",
		Name:            "web",
		ProviderName:    "registry.opentofu.org/hashicorp/aws",
		AttributeValues: map[string]interface{}{"id": "i-123"},
	}
}

func buildModuleState(rootResources []*tfjson.StateResource, childModules ...*tfjson.StateModule) *tfjson.State {
	return &tfjson.State{
		FormatVersion: "1.0",
		Values: &tfjson.StateValues{
			RootModule: &tfjson.StateModule{
				Resources:    rootResources,
				ChildModules: childModules,
			},
		},
	}
}

func TestResourceExists_ModulePrefix(t *testing.T) {
	s := buildModuleState(nil, &tfjson.StateModule{
		Address: "module.foo",
		Resources: []*tfjson.StateResource{
			newTestModuleResource("module.foo.aws_instance.web"),
		},
	})

	if !ResourceExists(s, "module.foo") {
		t.Error("expected module.foo to exist via prefix check")
	}
	if ResourceExists(s, "module.bar") {
		t.Error("expected module.bar to not exist")
	}
}

func TestResourceExists_ModulePrefixExactResourceStillWorks(t *testing.T) {
	s := buildModuleState(nil, &tfjson.StateModule{
		Address: "module.foo",
		Resources: []*tfjson.StateResource{
			newTestModuleResource("module.foo.aws_instance.web"),
		},
	})

	if !ResourceExists(s, "module.foo.aws_instance.web") {
		t.Error("expected module.foo.aws_instance.web to exist via exact match")
	}
}

func TestHasResourcesWithPrefix(t *testing.T) {
	s := buildModuleState(nil,
		&tfjson.StateModule{
			Address: "module.foo",
			Resources: []*tfjson.StateResource{
				newTestModuleResource("module.foo.aws_instance.web"),
			},
		},
		&tfjson.StateModule{
			Address: "module.bar",
			Resources: []*tfjson.StateResource{
				newTestModuleResource("module.bar.aws_s3_bucket.data"),
			},
		},
	)

	idx := NewStateIndex(s)

	if !idx.HasResourcesWithPrefix("module.foo") {
		t.Error("expected HasResourcesWithPrefix(module.foo) to be true")
	}
	if !idx.HasResourcesWithPrefix("module.bar") {
		t.Error("expected HasResourcesWithPrefix(module.bar) to be true")
	}
	if idx.HasResourcesWithPrefix("module.baz") {
		t.Error("expected HasResourcesWithPrefix(module.baz) to be false")
	}
}

func TestHasResourcesWithPrefix_NilIndex(t *testing.T) {
	var idx *StateIndex
	if idx.HasResourcesWithPrefix("module.foo") {
		t.Error("expected nil index to return false")
	}
}

func TestManagedBaseAddresses(t *testing.T) {
	s := buildModuleState(
		[]*tfjson.StateResource{
			newTestResource("aws_instance.root", nil),
			newTestDataResource("data.aws_ami.latest"),
		},
		&tfjson.StateModule{
			Address: "module.foo",
			Resources: []*tfjson.StateResource{
				newTestModuleResource("module.foo.aws_instance.web"),
				{
					Address:         "module.foo.data.aws_caller_identity.current",
					Mode:            tfjson.DataResourceMode,
					Type:            "aws_caller_identity",
					Name:            "current",
					ProviderName:    "registry.opentofu.org/hashicorp/aws",
					AttributeValues: map[string]interface{}{},
				},
			},
		},
	)

	idx := NewStateIndex(s)
	managed := idx.ManagedBaseAddresses()

	expected := []string{"aws_instance.root", "module.foo.aws_instance.web"}
	if len(managed) != len(expected) {
		t.Fatalf("expected %d managed addresses, got %d: %v", len(expected), len(managed), managed)
	}
	for i, addr := range expected {
		if managed[i] != addr {
			t.Errorf("expected managed[%d] = %q, got %q", i, addr, managed[i])
		}
	}
}

func TestManagedBaseAddresses_NilIndex(t *testing.T) {
	var idx *StateIndex
	if addrs := idx.ManagedBaseAddresses(); addrs != nil {
		t.Errorf("expected nil result for nil index, got %v", addrs)
	}
}

func TestManagedResourcesUnderModule(t *testing.T) {
	s := buildModuleState(
		[]*tfjson.StateResource{
			newTestResource("aws_instance.root", nil),
		},
		&tfjson.StateModule{
			Address: "module.foo",
			Resources: []*tfjson.StateResource{
				newTestModuleResource("module.foo.aws_instance.web"),
				{
					Address:         "module.foo.aws_s3_bucket.data",
					Mode:            tfjson.ManagedResourceMode,
					Type:            "aws_s3_bucket",
					Name:            "data",
					ProviderName:    "registry.opentofu.org/hashicorp/aws",
					AttributeValues: map[string]interface{}{"id": "bucket-123"},
				},
				{
					Address:         "module.foo.data.aws_ami.latest",
					Mode:            tfjson.DataResourceMode,
					Type:            "aws_ami",
					Name:            "latest",
					ProviderName:    "registry.opentofu.org/hashicorp/aws",
					AttributeValues: map[string]interface{}{"id": "ami-123"},
				},
			},
		},
	)

	idx := NewStateIndex(s)
	resources := idx.ManagedResourcesUnderModule("module.foo")

	if len(resources) != 2 {
		t.Fatalf("expected 2 managed resources under module.foo, got %d", len(resources))
	}

	addrs := map[string]bool{}
	for _, r := range resources {
		addrs[r.Address] = true
	}
	if !addrs["module.foo.aws_instance.web"] {
		t.Error("expected module.foo.aws_instance.web in results")
	}
	if !addrs["module.foo.aws_s3_bucket.data"] {
		t.Error("expected module.foo.aws_s3_bucket.data in results")
	}
}

func TestManagedResourcesUnderModule_NestedModules(t *testing.T) {
	s := buildModuleState(nil,
		&tfjson.StateModule{
			Address: "module.foo",
			Resources: []*tfjson.StateResource{
				newTestModuleResource("module.foo.aws_instance.web"),
			},
			ChildModules: []*tfjson.StateModule{
				{
					Address: "module.foo.module.bar",
					Resources: []*tfjson.StateResource{
						{
							Address:         "module.foo.module.bar.aws_s3_bucket.logs",
							Mode:            tfjson.ManagedResourceMode,
							Type:            "aws_s3_bucket",
							Name:            "logs",
							ProviderName:    "registry.opentofu.org/hashicorp/aws",
							AttributeValues: map[string]interface{}{"id": "bucket-456"},
						},
					},
				},
			},
		},
	)

	idx := NewStateIndex(s)

	// module.foo should include nested module.foo.module.bar resources
	resources := idx.ManagedResourcesUnderModule("module.foo")
	if len(resources) != 2 {
		t.Fatalf("expected 2 managed resources under module.foo, got %d", len(resources))
	}

	// module.foo.module.bar should only include its own resources
	resources = idx.ManagedResourcesUnderModule("module.foo.module.bar")
	if len(resources) != 1 {
		t.Fatalf("expected 1 managed resource under module.foo.module.bar, got %d", len(resources))
	}
	if resources[0].Address != "module.foo.module.bar.aws_s3_bucket.logs" {
		t.Errorf("expected module.foo.module.bar.aws_s3_bucket.logs, got %q", resources[0].Address)
	}
}

func TestManagedResourcesUnderModule_NoMatches(t *testing.T) {
	s := buildModuleState(
		[]*tfjson.StateResource{
			newTestResource("aws_instance.root", nil),
		},
	)

	idx := NewStateIndex(s)
	resources := idx.ManagedResourcesUnderModule("module.nonexistent")

	if len(resources) != 0 {
		t.Errorf("expected no resources for nonexistent module, got %d", len(resources))
	}
}

func TestManagedResourcesUnderModule_NilIndex(t *testing.T) {
	var idx *StateIndex
	resources := idx.ManagedResourcesUnderModule("module.foo")
	if resources != nil {
		t.Errorf("expected nil for nil index, got %v", resources)
	}
}

func TestManagedResourcesUnderModule_ForEachResources(t *testing.T) {
	s := buildModuleState(nil,
		&tfjson.StateModule{
			Address: "module.foo",
			Resources: []*tfjson.StateResource{
				{
					Address:         `module.foo.aws_s3_bucket.data["key-a"]`,
					Mode:            tfjson.ManagedResourceMode,
					Type:            "aws_s3_bucket",
					Name:            "data",
					Index:           "key-a",
					ProviderName:    "registry.opentofu.org/hashicorp/aws",
					AttributeValues: map[string]interface{}{"id": "bucket-a"},
				},
				{
					Address:         `module.foo.aws_s3_bucket.data["key-b"]`,
					Mode:            tfjson.ManagedResourceMode,
					Type:            "aws_s3_bucket",
					Name:            "data",
					Index:           "key-b",
					ProviderName:    "registry.opentofu.org/hashicorp/aws",
					AttributeValues: map[string]interface{}{"id": "bucket-b"},
				},
			},
		},
	)

	idx := NewStateIndex(s)
	resources := idx.ManagedResourcesUnderModule("module.foo")

	if len(resources) != 2 {
		t.Fatalf("expected 2 for_each instances under module.foo, got %d", len(resources))
	}
}

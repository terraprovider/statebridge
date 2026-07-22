package state

import (
	"encoding/json"
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

func TestAllManagedResources(t *testing.T) {
	s := buildModuleState(
		[]*tfjson.StateResource{
			newTestResource("aws_instance.web", nil),
			newTestDataResource("data.aws_ami.latest"),
		},
		&tfjson.StateModule{
			Address: "module.foo",
			Resources: []*tfjson.StateResource{
				newTestModuleResource("module.foo.aws_s3_bucket.data"),
			},
		},
	)

	idx := NewStateIndex(s)
	resources := idx.AllManagedResources()

	if len(resources) != 2 {
		t.Fatalf("expected 2 managed resources (excluding data source), got %d", len(resources))
	}

	addrs := map[string]bool{}
	for _, r := range resources {
		addrs[r.Address] = true
	}
	if !addrs["aws_instance.web"] {
		t.Error("expected aws_instance.web")
	}
	if !addrs["module.foo.aws_s3_bucket.data"] {
		t.Error("expected module.foo.aws_s3_bucket.data")
	}
}

func TestAllManagedResources_NilIndex(t *testing.T) {
	var idx *StateIndex
	resources := idx.AllManagedResources()
	if resources != nil {
		t.Errorf("expected nil for nil index, got %v", resources)
	}
}

func TestAllManagedResources_EmptyState(t *testing.T) {
	s := buildModuleState(nil)
	idx := NewStateIndex(s)
	resources := idx.AllManagedResources()
	if len(resources) != 0 {
		t.Errorf("expected no resources for empty state, got %d", len(resources))
	}
}

func TestAllManagedResources_ForEach(t *testing.T) {
	s := buildModuleState(
		[]*tfjson.StateResource{
			{
				Address:         `aws_s3_bucket.data["key-a"]`,
				Mode:            tfjson.ManagedResourceMode,
				Type:            "aws_s3_bucket",
				Name:            "data",
				Index:           "key-a",
				ProviderName:    "registry.opentofu.org/hashicorp/aws",
				AttributeValues: map[string]interface{}{"id": "bucket-a"},
			},
			{
				Address:         `aws_s3_bucket.data["key-b"]`,
				Mode:            tfjson.ManagedResourceMode,
				Type:            "aws_s3_bucket",
				Name:            "data",
				Index:           "key-b",
				ProviderName:    "registry.opentofu.org/hashicorp/aws",
				AttributeValues: map[string]interface{}{"id": "bucket-b"},
			},
		},
	)

	idx := NewStateIndex(s)
	resources := idx.AllManagedResources()
	if len(resources) != 2 {
		t.Fatalf("expected 2 for_each instances, got %d", len(resources))
	}
}

func TestBaseAddress(t *testing.T) {
	tests := []struct {
		address string
		want    string
	}{
		{"aws_instance.web", "aws_instance.web"},
		{`aws_s3_bucket.data["key"]`, "aws_s3_bucket.data"},
		{"aws_instance.web[0]", "aws_instance.web"},
		{"module.foo.aws_instance.web", "module.foo.aws_instance.web"},
		{`module.foo.aws_s3_bucket.data["key"]`, "module.foo.aws_s3_bucket.data"},
		// Indexed module instances: the module index must be preserved and only
		// the trailing resource key stripped.
		{"module.configuration_policies[0].azuread_group.all", "module.configuration_policies[0].azuread_group.all"},
		{`module.configuration_policies[0].azuread_group.all["cfg_x"]`, "module.configuration_policies[0].azuread_group.all"},
		{`module.foo[0].module.bar[1].type.name["k"]`, "module.foo[0].module.bar[1].type.name"},
		{"module.foo[0].type.name[2]", "module.foo[0].type.name"},
	}
	for _, tt := range tests {
		t.Run(tt.address, func(t *testing.T) {
			if got := BaseAddress(tt.address); got != tt.want {
				t.Errorf("BaseAddress(%q) = %q, want %q", tt.address, got, tt.want)
			}
		})
	}
}

func TestFormatInstanceKey(t *testing.T) {
	tests := []struct {
		name  string
		index interface{}
		want  string
	}{
		{"nil index", nil, ""},
		{"for_each string key", "my-key", `["my-key"]`},
		{"for_each numeric-looking string key", "0", `["0"]`},
		// Real state read via terraform-exec Show decodes numbers as
		// json.Number; count indices arrive this way and must render as bare
		// integers, never quoted strings.
		{"count index json.Number", json.Number("0"), "[0]"},
		{"count index json.Number nonzero", json.Number("12"), "[12]"},
		// float64/int and other integer kinds are handled for callers that
		// build state directly rather than decoding it from JSON.
		{"count index float64", float64(0), "[0]"},
		{"count index float64 nonzero", float64(12), "[12]"},
		{"count index int", 3, "[3]"},
		{"count index int64", int64(7), "[7]"},
		{"count index uint", uint(9), "[9]"},
		{"count index uint64", uint64(42), "[42]"},
		{"count index float32", float32(5), "[5]"},
		{"for_each key with quote", `a"b`, `["a\"b"]`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FormatInstanceKey(tt.index); got != tt.want {
				t.Errorf("FormatInstanceKey(%#v) = %q, want %q", tt.index, got, tt.want)
			}
		})
	}
}

func TestConfigAddress(t *testing.T) {
	tests := []struct {
		address string
		want    string
	}{
		{"aws_instance.web", "aws_instance.web"},
		{"aws_instance.web[2]", "aws_instance.web"},
		{`aws_s3_bucket.data["key"]`, "aws_s3_bucket.data"},
		{"module.foo.aws_instance.web", "module.foo.aws_instance.web"},
		// Module-instance indices are stripped along with resource keys.
		{"module.cp[0]", "module.cp"},
		{"module.cp[0].random_id.items", "module.cp.random_id.items"},
		{`module.cp[0].random_id.items["a"]`, "module.cp.random_id.items"},
		{`module.a[0].module.b[1].type.name["k"]`, "module.a.module.b.type.name"},
	}
	for _, tt := range tests {
		t.Run(tt.address, func(t *testing.T) {
			if got := ConfigAddress(tt.address); got != tt.want {
				t.Errorf("ConfigAddress(%q) = %q, want %q", tt.address, got, tt.want)
			}
		})
	}
}

func TestModuleInstanceBases(t *testing.T) {
	newInst := func(addr, key string) *tfjson.StateResource {
		return &tfjson.StateResource{
			Address:         addr,
			Mode:            tfjson.ManagedResourceMode,
			Type:            "random_id",
			Name:            "items",
			Index:           key,
			ProviderName:    "registry.opentofu.org/hashicorp/random",
			AttributeValues: map[string]interface{}{"id": "x"},
		}
	}

	t.Run("multiple module instances", func(t *testing.T) {
		s := buildModuleState(nil,
			&tfjson.StateModule{
				Address: "module.cp[0]",
				Resources: []*tfjson.StateResource{
					newInst(`module.cp[0].random_id.items["a"]`, "a"),
					newInst(`module.cp[0].random_id.items["b"]`, "b"),
				},
			},
			&tfjson.StateModule{
				Address: "module.cp[1]",
				Resources: []*tfjson.StateResource{
					newInst(`module.cp[1].random_id.items["a"]`, "a"),
				},
			},
		)
		idx := NewStateIndex(s)
		got := idx.ModuleInstanceBases("module.cp.random_id.items")
		want := []string{"module.cp[0].random_id.items", "module.cp[1].random_id.items"}
		if len(got) != len(want) {
			t.Fatalf("expected %d bases, got %d: %v", len(want), len(got), got)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("base[%d] = %q, want %q", i, got[i], want[i])
			}
		}
	})

	t.Run("single module instance", func(t *testing.T) {
		s := buildModuleState(nil,
			&tfjson.StateModule{
				Address: "module.cp[0]",
				Resources: []*tfjson.StateResource{
					newInst(`module.cp[0].random_id.items["a"]`, "a"),
					newInst(`module.cp[0].random_id.items["b"]`, "b"),
				},
			},
		)
		idx := NewStateIndex(s)
		got := idx.ModuleInstanceBases("module.cp.random_id.items")
		if len(got) != 1 || got[0] != "module.cp[0].random_id.items" {
			t.Errorf("expected single base module.cp[0].random_id.items, got %v", got)
		}
	})

	t.Run("nil index", func(t *testing.T) {
		var idx *StateIndex
		if got := idx.ModuleInstanceBases("module.cp.random_id.items"); got != nil {
			t.Errorf("expected nil for nil index, got %v", got)
		}
	})

	t.Run("no match", func(t *testing.T) {
		s := buildModuleState(nil,
			&tfjson.StateModule{
				Address: "module.cp[0]",
				Resources: []*tfjson.StateResource{
					newInst(`module.cp[0].random_id.items["a"]`, "a"),
				},
			},
		)
		idx := NewStateIndex(s)
		if got := idx.ModuleInstanceBases("module.other.random_id.items"); got != nil {
			t.Errorf("expected nil for unmatched config address, got %v", got)
		}
	})
}

func TestResourceExists_IndexedModuleForEach(t *testing.T) {
	s := buildModuleState(nil,
		&tfjson.StateModule{
			Address: "module.configuration_policies[0]",
			Resources: []*tfjson.StateResource{
				{
					Address:         `module.configuration_policies[0].azuread_group.all["cfg_a"]`,
					Mode:            tfjson.ManagedResourceMode,
					Type:            "azuread_group",
					Name:            "all",
					Index:           "cfg_a",
					ProviderName:    "registry.opentofu.org/hashicorp/azuread",
					AttributeValues: map[string]interface{}{"id": "grp-a"},
				},
				{
					Address:         `module.configuration_policies[0].azuread_group.all["cfg_b"]`,
					Mode:            tfjson.ManagedResourceMode,
					Type:            "azuread_group",
					Name:            "all",
					Index:           "cfg_b",
					ProviderName:    "registry.opentofu.org/hashicorp/azuread",
					AttributeValues: map[string]interface{}{"id": "grp-b"},
				},
			},
		},
	)

	// Base address (with module index, no resource key) matches any instance.
	if !ResourceExists(s, "module.configuration_policies[0].azuread_group.all") {
		t.Error("expected base address under indexed module to match for_each instances")
	}
	// Exact instance addresses match.
	if !ResourceExists(s, `module.configuration_policies[0].azuread_group.all["cfg_a"]`) {
		t.Error("expected exact instance cfg_a to exist")
	}
	// A missing key must not match, even though other keys exist.
	if ResourceExists(s, `module.configuration_policies[0].azuread_group.all["cfg_missing"]`) {
		t.Error("expected missing key under indexed module to not match")
	}
	// A different module index must not match.
	if ResourceExists(s, "module.configuration_policies[1].azuread_group.all") {
		t.Error("expected a different module index to not match")
	}
	// The indexed module prefix matches (any resource beneath it).
	if !ResourceExists(s, "module.configuration_policies[0]") {
		t.Error("expected indexed module prefix to match resources beneath it")
	}
	// The config address (module index stripped) — the form emitted for removed
	// blocks — must match the indexed instances in state.
	if !ResourceExists(s, "module.configuration_policies.azuread_group.all") {
		t.Error("expected config address (module index stripped) to match indexed instances")
	}
	// A config address for a resource that does not exist must not match.
	if ResourceExists(s, "module.configuration_policies.azuread_group.missing") {
		t.Error("expected config address for a missing resource to not match")
	}
}

func TestLookupResourcesByPrefix_IndexedModule(t *testing.T) {
	s := buildModuleState(nil,
		&tfjson.StateModule{
			Address: "module.configuration_policies[0]",
			Resources: []*tfjson.StateResource{
				{
					Address:         `module.configuration_policies[0].azuread_group.all["cfg_a"]`,
					Mode:            tfjson.ManagedResourceMode,
					Type:            "azuread_group",
					Name:            "all",
					Index:           "cfg_a",
					ProviderName:    "registry.opentofu.org/hashicorp/azuread",
					AttributeValues: map[string]interface{}{"id": "grp-a"},
				},
				{
					Address:         `module.configuration_policies[0].azuread_group.all["cfg_b"]`,
					Mode:            tfjson.ManagedResourceMode,
					Type:            "azuread_group",
					Name:            "all",
					Index:           "cfg_b",
					ProviderName:    "registry.opentofu.org/hashicorp/azuread",
					AttributeValues: map[string]interface{}{"id": "grp-b"},
				},
				// A different resource under the same indexed module must not be
				// lumped in with the azuread_group.all base group.
				{
					Address:         "module.configuration_policies[0].azuread_application.app",
					Mode:            tfjson.ManagedResourceMode,
					Type:            "azuread_application",
					Name:            "app",
					ProviderName:    "registry.opentofu.org/hashicorp/azuread",
					AttributeValues: map[string]interface{}{"id": "app-1"},
				},
			},
		},
	)

	idx := NewStateIndex(s)
	got, err := idx.LookupResourcesByPrefix("module.configuration_policies[0].azuread_group.all")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 instances of azuread_group.all, got %d", len(got))
	}
	for _, r := range got {
		if r.Type != "azuread_group" {
			t.Errorf("unexpected resource in group: %q", r.Address)
		}
	}
}

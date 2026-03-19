package engine

import (
	"context"
	"testing"

	tfjson "github.com/hashicorp/terraform-json"

	"github.com/terraprovider/statebridge/internal/testutil"
	"github.com/terraprovider/statebridge/pkg/generator"
)

// --- Helper: allModulePrefixes ---

func TestAllModulePrefixes(t *testing.T) {
	tests := []struct {
		address  string
		expected []string
	}{
		{"aws_instance.web", nil},
		{"module.foo.aws_instance.web", []string{"module.foo"}},
		{"module.foo.module.bar.aws_instance.web", []string{"module.foo.module.bar", "module.foo"}},
		{"module.a.module.b.module.c.aws_instance.web", []string{"module.a.module.b.module.c", "module.a.module.b", "module.a"}},
	}
	for _, tt := range tests {
		result := allModulePrefixes(tt.address)
		if len(result) != len(tt.expected) {
			t.Errorf("allModulePrefixes(%q) = %v, want %v", tt.address, result, tt.expected)
			continue
		}
		for i := range result {
			if result[i] != tt.expected[i] {
				t.Errorf("allModulePrefixes(%q)[%d] = %q, want %q", tt.address, i, result[i], tt.expected[i])
			}
		}
	}
}

// --- Helper: extractModulePath ---

func TestExtractModulePath(t *testing.T) {
	tests := []struct {
		address  string
		expected string
	}{
		{"aws_instance.web", ""},
		{"module.foo.aws_instance.web", "module.foo"},
		{"module.foo.module.bar.aws_instance.web", "module.foo.module.bar"},
		{"module.a.module.b.module.c.aws_instance.web", "module.a.module.b.module.c"},
	}
	for _, tt := range tests {
		result := extractModulePath(tt.address)
		if result != tt.expected {
			t.Errorf("extractModulePath(%q) = %q, want %q", tt.address, result, tt.expected)
		}
	}
}

// --- consolidateModuleRemovals tests ---

func makeRemovedBlock(from, layer, source string) *generator.RemovedBlock {
	return &generator.RemovedBlock{
		From:    from,
		Destroy: false,
		Layer:   layer,
		Source:  source,
	}
}

func makeImportBlock(to, layer, source string) *generator.ImportBlock {
	return &generator.ImportBlock{
		To:       to,
		ID:       "id-" + to,
		Layer:    layer,
		Source:   source,
		Provider: "registry.opentofu.org/hashicorp/aws",
	}
}

func TestConsolidateModuleRemovals_AllResourcesInModule(t *testing.T) {
	// All resources under module.foo are being removed → consolidate
	srcLayer := "/layers/compute"

	mock := testutil.NewMockStateReader(map[string]*tfjson.State{
		srcLayer: testutil.BuildState(
			testutil.NewResource("module.foo.aws_instance.web", "aws_instance", "web", "",
				map[string]interface{}{"id": "i-123"}),
			testutil.NewResource("module.foo.aws_s3_bucket.data", "aws_s3_bucket", "data", "",
				map[string]interface{}{"id": "bucket-123"}),
		),
	})

	engine := New(Config{StateReader: mock})

	blocks := []generator.Block{
		makeRemovedBlock("module.foo.aws_instance.web", srcLayer, "001.yaml"),
		makeRemovedBlock("module.foo.aws_s3_bucket.data", srcLayer, "001.yaml"),
		makeImportBlock("module.bar.aws_instance.web", "/layers/app", "001.yaml"),
	}

	result, err := engine.consolidateModuleRemovals(context.Background(), blocks)
	if err != nil {
		t.Fatalf("consolidateModuleRemovals returned unexpected error: %v", err)
	}

	var removedBlocks []*generator.RemovedBlock
	var otherBlocks []generator.Block
	for _, b := range result {
		if rb, ok := b.(*generator.RemovedBlock); ok {
			removedBlocks = append(removedBlocks, rb)
		} else {
			otherBlocks = append(otherBlocks, b)
		}
	}

	if len(removedBlocks) != 1 {
		t.Fatalf("expected 1 consolidated removed block, got %d: %v", len(removedBlocks), removedBlocks)
	}
	if removedBlocks[0].From != "module.foo" {
		t.Errorf("expected removed block from module.foo, got %q", removedBlocks[0].From)
	}
	if len(otherBlocks) != 1 {
		t.Errorf("expected 1 non-removed block (import), got %d", len(otherBlocks))
	}
}

func TestConsolidateModuleRemovals_PartialModule(t *testing.T) {
	// Only some resources under module.foo are removed → no consolidation
	srcLayer := "/layers/compute"

	mock := testutil.NewMockStateReader(map[string]*tfjson.State{
		srcLayer: testutil.BuildState(
			testutil.NewResource("module.foo.aws_instance.web", "aws_instance", "web", "",
				map[string]interface{}{"id": "i-123"}),
			testutil.NewResource("module.foo.aws_s3_bucket.data", "aws_s3_bucket", "data", "",
				map[string]interface{}{"id": "bucket-123"}),
		),
	})

	engine := New(Config{StateReader: mock})

	blocks := []generator.Block{
		makeRemovedBlock("module.foo.aws_instance.web", srcLayer, "001.yaml"),
		// aws_s3_bucket.data NOT removed
	}

	result, err := engine.consolidateModuleRemovals(context.Background(), blocks)
	if err != nil {
		t.Fatalf("consolidateModuleRemovals returned unexpected error: %v", err)
	}

	if len(result) != 1 {
		t.Fatalf("expected 1 block (no consolidation), got %d", len(result))
	}
	rb := result[0].(*generator.RemovedBlock)
	if rb.From != "module.foo.aws_instance.web" {
		t.Errorf("expected individual removed block, got %q", rb.From)
	}
}

func TestConsolidateModuleRemovals_NestedModulePartial(t *testing.T) {
	// All of module.foo.module.bar removed, but not all of module.foo
	// → consolidate module.foo.module.bar only
	srcLayer := "/layers/compute"

	mock := testutil.NewMockStateReader(map[string]*tfjson.State{
		srcLayer: testutil.BuildState(
			testutil.NewResource("module.foo.aws_instance.web", "aws_instance", "web", "",
				map[string]interface{}{"id": "i-123"}),
			testutil.NewResource("module.foo.module.bar.aws_instance.api", "aws_instance", "api", "",
				map[string]interface{}{"id": "i-456"}),
			testutil.NewResource("module.foo.module.bar.aws_s3_bucket.logs", "aws_s3_bucket", "logs", "",
				map[string]interface{}{"id": "bucket-456"}),
		),
	})

	engine := New(Config{StateReader: mock})

	blocks := []generator.Block{
		// Only module.foo.module.bar resources removed, not module.foo.aws_instance.web
		makeRemovedBlock("module.foo.module.bar.aws_instance.api", srcLayer, "001.yaml"),
		makeRemovedBlock("module.foo.module.bar.aws_s3_bucket.logs", srcLayer, "001.yaml"),
	}

	result, err := engine.consolidateModuleRemovals(context.Background(), blocks)
	if err != nil {
		t.Fatalf("consolidateModuleRemovals returned unexpected error: %v", err)
	}

	var removedBlocks []*generator.RemovedBlock
	for _, b := range result {
		if rb, ok := b.(*generator.RemovedBlock); ok {
			removedBlocks = append(removedBlocks, rb)
		}
	}

	if len(removedBlocks) != 1 {
		t.Fatalf("expected 1 consolidated removed block, got %d", len(removedBlocks))
	}
	if removedBlocks[0].From != "module.foo.module.bar" {
		t.Errorf("expected removed block from module.foo.module.bar, got %q", removedBlocks[0].From)
	}
}

func TestConsolidateModuleRemovals_FullNestedConsolidation(t *testing.T) {
	// All resources under module.foo (including nested module.foo.module.bar) removed
	// → single removed { from = module.foo }
	srcLayer := "/layers/compute"

	mock := testutil.NewMockStateReader(map[string]*tfjson.State{
		srcLayer: testutil.BuildState(
			testutil.NewResource("module.foo.aws_instance.web", "aws_instance", "web", "",
				map[string]interface{}{"id": "i-123"}),
			testutil.NewResource("module.foo.module.bar.aws_instance.api", "aws_instance", "api", "",
				map[string]interface{}{"id": "i-456"}),
		),
	})

	engine := New(Config{StateReader: mock})

	blocks := []generator.Block{
		makeRemovedBlock("module.foo.aws_instance.web", srcLayer, "001.yaml"),
		makeRemovedBlock("module.foo.module.bar.aws_instance.api", srcLayer, "001.yaml"),
	}

	result, err := engine.consolidateModuleRemovals(context.Background(), blocks)
	if err != nil {
		t.Fatalf("consolidateModuleRemovals returned unexpected error: %v", err)
	}

	var removedBlocks []*generator.RemovedBlock
	for _, b := range result {
		if rb, ok := b.(*generator.RemovedBlock); ok {
			removedBlocks = append(removedBlocks, rb)
		}
	}

	if len(removedBlocks) != 1 {
		t.Fatalf("expected 1 consolidated removed block, got %d", len(removedBlocks))
	}
	if removedBlocks[0].From != "module.foo" {
		t.Errorf("expected removed block from module.foo, got %q", removedBlocks[0].From)
	}
}

func TestConsolidateModuleRemovals_MixModuleAndRoot(t *testing.T) {
	// Module resources consolidated, root resource kept individual
	srcLayer := "/layers/compute"

	mock := testutil.NewMockStateReader(map[string]*tfjson.State{
		srcLayer: testutil.BuildState(
			testutil.NewResource("aws_instance.standalone", "aws_instance", "standalone", "",
				map[string]interface{}{"id": "i-000"}),
			testutil.NewResource("module.foo.aws_instance.web", "aws_instance", "web", "",
				map[string]interface{}{"id": "i-123"}),
		),
	})

	engine := New(Config{StateReader: mock})

	blocks := []generator.Block{
		makeRemovedBlock("aws_instance.standalone", srcLayer, "001.yaml"),
		makeRemovedBlock("module.foo.aws_instance.web", srcLayer, "001.yaml"),
	}

	result, err := engine.consolidateModuleRemovals(context.Background(), blocks)
	if err != nil {
		t.Fatalf("consolidateModuleRemovals returned unexpected error: %v", err)
	}

	var removedBlocks []*generator.RemovedBlock
	for _, b := range result {
		if rb, ok := b.(*generator.RemovedBlock); ok {
			removedBlocks = append(removedBlocks, rb)
		}
	}

	if len(removedBlocks) != 2 {
		t.Fatalf("expected 2 removed blocks (root + consolidated module), got %d", len(removedBlocks))
	}

	fromAddrs := map[string]bool{}
	for _, rb := range removedBlocks {
		fromAddrs[rb.From] = true
	}
	if !fromAddrs["aws_instance.standalone"] {
		t.Error("expected root resource removed block to remain")
	}
	if !fromAddrs["module.foo"] {
		t.Error("expected module.foo consolidated removed block")
	}
}

func TestConsolidateModuleRemovals_NoModules(t *testing.T) {
	// Only root resources → nothing to consolidate
	srcLayer := "/layers/compute"

	mock := testutil.NewMockStateReader(map[string]*tfjson.State{
		srcLayer: testutil.BuildState(
			testutil.NewResource("aws_instance.web", "aws_instance", "web", "",
				map[string]interface{}{"id": "i-123"}),
		),
	})

	engine := New(Config{StateReader: mock})

	blocks := []generator.Block{
		makeRemovedBlock("aws_instance.web", srcLayer, "001.yaml"),
	}

	result, err := engine.consolidateModuleRemovals(context.Background(), blocks)
	if err != nil {
		t.Fatalf("consolidateModuleRemovals returned unexpected error: %v", err)
	}

	if len(result) != 1 {
		t.Fatalf("expected 1 block unchanged, got %d", len(result))
	}
}

func TestConsolidateModuleRemovals_EmptyBlocks(t *testing.T) {
	engine := New(Config{StateReader: testutil.NewMockStateReader(nil)})

	result, err := engine.consolidateModuleRemovals(context.Background(), nil)
	if err != nil {
		t.Fatalf("consolidateModuleRemovals returned unexpected error: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty result, got %d blocks", len(result))
	}

	result, err = engine.consolidateModuleRemovals(context.Background(), []generator.Block{})
	if err != nil {
		t.Fatalf("consolidateModuleRemovals returned unexpected error: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty result, got %d blocks", len(result))
	}
}

func TestConsolidateModuleRemovals_MultipleIndependentModules(t *testing.T) {
	// Two independent modules, both fully removed
	srcLayer := "/layers/compute"

	mock := testutil.NewMockStateReader(map[string]*tfjson.State{
		srcLayer: testutil.BuildState(
			testutil.NewResource("module.foo.aws_instance.web", "aws_instance", "web", "",
				map[string]interface{}{"id": "i-123"}),
			testutil.NewResource("module.bar.aws_s3_bucket.data", "aws_s3_bucket", "data", "",
				map[string]interface{}{"id": "bucket-123"}),
		),
	})

	engine := New(Config{StateReader: mock})

	blocks := []generator.Block{
		makeRemovedBlock("module.foo.aws_instance.web", srcLayer, "001.yaml"),
		makeRemovedBlock("module.bar.aws_s3_bucket.data", srcLayer, "002.yaml"),
	}

	result, err := engine.consolidateModuleRemovals(context.Background(), blocks)
	if err != nil {
		t.Fatalf("consolidateModuleRemovals returned unexpected error: %v", err)
	}

	var removedBlocks []*generator.RemovedBlock
	for _, b := range result {
		if rb, ok := b.(*generator.RemovedBlock); ok {
			removedBlocks = append(removedBlocks, rb)
		}
	}

	if len(removedBlocks) != 2 {
		t.Fatalf("expected 2 consolidated removed blocks, got %d", len(removedBlocks))
	}

	fromAddrs := map[string]bool{}
	for _, rb := range removedBlocks {
		fromAddrs[rb.From] = true
	}
	if !fromAddrs["module.foo"] {
		t.Error("expected module.foo consolidated")
	}
	if !fromAddrs["module.bar"] {
		t.Error("expected module.bar consolidated")
	}
}

func TestConsolidateModuleRemovals_DataSourcesIgnored(t *testing.T) {
	// Module has a data source — data sources should not prevent consolidation
	srcLayer := "/layers/compute"

	// Build state with both managed and data resources in the module
	s := &tfjson.State{
		FormatVersion: "1.0",
		Values: &tfjson.StateValues{
			RootModule: &tfjson.StateModule{
				ChildModules: []*tfjson.StateModule{
					{
						Address: "module.foo",
						Resources: []*tfjson.StateResource{
							{
								Address:         "module.foo.aws_instance.web",
								Mode:            tfjson.ManagedResourceMode,
								Type:            "aws_instance",
								Name:            "web",
								ProviderName:    "registry.opentofu.org/hashicorp/aws",
								AttributeValues: map[string]interface{}{"id": "i-123"},
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
				},
			},
		},
	}

	mock := testutil.NewMockStateReader(map[string]*tfjson.State{srcLayer: s})
	engine := New(Config{StateReader: mock})

	blocks := []generator.Block{
		// Only the managed resource is removed (data source filtered earlier)
		makeRemovedBlock("module.foo.aws_instance.web", srcLayer, "001.yaml"),
	}

	result, err := engine.consolidateModuleRemovals(context.Background(), blocks)
	if err != nil {
		t.Fatalf("consolidateModuleRemovals returned unexpected error: %v", err)
	}

	var removedBlocks []*generator.RemovedBlock
	for _, b := range result {
		if rb, ok := b.(*generator.RemovedBlock); ok {
			removedBlocks = append(removedBlocks, rb)
		}
	}

	if len(removedBlocks) != 1 {
		t.Fatalf("expected 1 consolidated removed block, got %d", len(removedBlocks))
	}
	if removedBlocks[0].From != "module.foo" {
		t.Errorf("expected module.foo consolidated (data source should not prevent consolidation), got %q", removedBlocks[0].From)
	}
}

func TestConsolidateModuleRemovals_ThreeLevelNesting(t *testing.T) {
	// Three levels: module.a.module.b.module.c — all removed → consolidate to module.a
	srcLayer := "/layers/compute"

	mock := testutil.NewMockStateReader(map[string]*tfjson.State{
		srcLayer: testutil.BuildState(
			testutil.NewResource("module.a.aws_instance.root", "aws_instance", "root", "",
				map[string]interface{}{"id": "i-root"}),
			testutil.NewResource("module.a.module.b.aws_instance.mid", "aws_instance", "mid", "",
				map[string]interface{}{"id": "i-mid"}),
			testutil.NewResource("module.a.module.b.module.c.aws_s3_bucket.deep", "aws_s3_bucket", "deep", "",
				map[string]interface{}{"id": "bucket-deep"}),
		),
	})

	engine := New(Config{StateReader: mock})

	blocks := []generator.Block{
		makeRemovedBlock("module.a.aws_instance.root", srcLayer, "001.yaml"),
		makeRemovedBlock("module.a.module.b.aws_instance.mid", srcLayer, "001.yaml"),
		makeRemovedBlock("module.a.module.b.module.c.aws_s3_bucket.deep", srcLayer, "001.yaml"),
	}

	result, err := engine.consolidateModuleRemovals(context.Background(), blocks)
	if err != nil {
		t.Fatalf("consolidateModuleRemovals returned unexpected error: %v", err)
	}

	var removedBlocks []*generator.RemovedBlock
	for _, b := range result {
		if rb, ok := b.(*generator.RemovedBlock); ok {
			removedBlocks = append(removedBlocks, rb)
		}
	}

	if len(removedBlocks) != 1 {
		t.Fatalf("expected 1 consolidated removed block, got %d", len(removedBlocks))
	}
	if removedBlocks[0].From != "module.a" {
		t.Errorf("expected consolidated to module.a, got %q", removedBlocks[0].From)
	}
}

func TestConsolidateModuleRemovals_ThreeLevelPartial(t *testing.T) {
	// Three levels but only module.a.module.b.module.c fully removed;
	// module.a.module.b has other resources → consolidate only module.c
	srcLayer := "/layers/compute"

	mock := testutil.NewMockStateReader(map[string]*tfjson.State{
		srcLayer: testutil.BuildState(
			testutil.NewResource("module.a.module.b.aws_instance.stays", "aws_instance", "stays", "",
				map[string]interface{}{"id": "i-stays"}),
			testutil.NewResource("module.a.module.b.module.c.aws_s3_bucket.deep1", "aws_s3_bucket", "deep1", "",
				map[string]interface{}{"id": "bucket-deep1"}),
			testutil.NewResource("module.a.module.b.module.c.aws_s3_bucket.deep2", "aws_s3_bucket", "deep2", "",
				map[string]interface{}{"id": "bucket-deep2"}),
		),
	})

	engine := New(Config{StateReader: mock})

	// Only remove module.c resources, not module.b's own resource
	blocks := []generator.Block{
		makeRemovedBlock("module.a.module.b.module.c.aws_s3_bucket.deep1", srcLayer, "001.yaml"),
		makeRemovedBlock("module.a.module.b.module.c.aws_s3_bucket.deep2", srcLayer, "001.yaml"),
	}

	result, err := engine.consolidateModuleRemovals(context.Background(), blocks)
	if err != nil {
		t.Fatalf("consolidateModuleRemovals returned unexpected error: %v", err)
	}

	var removedBlocks []*generator.RemovedBlock
	for _, b := range result {
		if rb, ok := b.(*generator.RemovedBlock); ok {
			removedBlocks = append(removedBlocks, rb)
		}
	}

	if len(removedBlocks) != 1 {
		t.Fatalf("expected 1 consolidated removed block, got %d", len(removedBlocks))
	}
	if removedBlocks[0].From != "module.a.module.b.module.c" {
		t.Errorf("expected consolidated to module.a.module.b.module.c, got %q", removedBlocks[0].From)
	}
}

func TestConsolidateModuleRemovals_MixedSiblingConsolidation(t *testing.T) {
	// Two sibling modules under module.a: module.b (fully removed) and module.c (partially removed).
	// module.b should consolidate; module.c should not; module.a should not consolidate.
	srcLayer := "/layers/compute"

	mock := testutil.NewMockStateReader(map[string]*tfjson.State{
		srcLayer: testutil.BuildState(
			testutil.NewResource("module.a.module.b.aws_instance.web", "aws_instance", "web", "",
				map[string]interface{}{"id": "i-b-web"}),
			testutil.NewResource("module.a.module.c.aws_instance.api", "aws_instance", "api", "",
				map[string]interface{}{"id": "i-c-api"}),
			testutil.NewResource("module.a.module.c.aws_s3_bucket.logs", "aws_s3_bucket", "logs", "",
				map[string]interface{}{"id": "bucket-c-logs"}),
		),
	})

	engine := New(Config{StateReader: mock})

	blocks := []generator.Block{
		// All of module.b removed
		makeRemovedBlock("module.a.module.b.aws_instance.web", srcLayer, "001.yaml"),
		// Only part of module.c removed
		makeRemovedBlock("module.a.module.c.aws_instance.api", srcLayer, "001.yaml"),
	}

	result, err := engine.consolidateModuleRemovals(context.Background(), blocks)
	if err != nil {
		t.Fatalf("consolidateModuleRemovals returned unexpected error: %v", err)
	}

	var removedBlocks []*generator.RemovedBlock
	for _, b := range result {
		if rb, ok := b.(*generator.RemovedBlock); ok {
			removedBlocks = append(removedBlocks, rb)
		}
	}

	if len(removedBlocks) != 2 {
		t.Fatalf("expected 2 removed blocks, got %d", len(removedBlocks))
	}

	fromAddrs := map[string]bool{}
	for _, rb := range removedBlocks {
		fromAddrs[rb.From] = true
	}

	// module.b should be consolidated
	if !fromAddrs["module.a.module.b"] {
		t.Error("expected module.a.module.b consolidated")
	}
	// module.c should remain individual (not consolidated)
	if !fromAddrs["module.a.module.c.aws_instance.api"] {
		t.Error("expected module.a.module.c.aws_instance.api to remain individual")
	}
	// module.a should NOT be consolidated (module.c is partial)
	if fromAddrs["module.a"] {
		t.Error("module.a should not be consolidated (module.c is partial)")
	}
}

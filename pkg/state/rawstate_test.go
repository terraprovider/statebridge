package state

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-exec/tfexec"
)

func TestParseRawState(t *testing.T) {
	const raw = `{
  "version": 4,
  "resources": [
    {"mode": "managed", "type": "azurerm_resource_group", "name": "rg",
     "provider": "provider[\"registry.opentofu.org/hashicorp/azurerm\"]",
     "instances": [{"schema_version": 0, "attributes": {"id": "/subscriptions/x/resourceGroups/rg"}}]},
    {"mode": "managed", "type": "random_id", "name": "c",
     "provider": "provider[\"registry.opentofu.org/hashicorp/random\"].alias",
     "instances": [
       {"index_key": 0, "attributes": {"id": "c0"}},
       {"index_key": 1, "attributes": {"id": "c1"}},
       {"index_key": 1, "deposed": "abcd1234", "attributes": {"id": "old"}}
     ]},
    {"module": "module.m[\"a.b\"]", "mode": "managed", "type": "random_id", "name": "e",
     "provider": "provider[\"registry.opentofu.org/hashicorp/random\"]",
     "instances": [{"index_key": "k.1", "attributes": {"id": "e1", "n": 5}}]},
    {"mode": "data", "type": "azurerm_client_config", "name": "cur",
     "provider": "provider[\"registry.opentofu.org/hashicorp/azurerm\"]",
     "instances": [{"attributes": {"id": "cc"}}]}
  ]
}`

	s, err := parseRawState([]byte(raw))
	if err != nil {
		t.Fatalf("parseRawState: %v", err)
	}
	idx := NewStateIndex(s)

	tests := []struct {
		address  string
		mode     string
		key      string
		provider string
		id       string
	}{
		{"azurerm_resource_group.rg", "managed", "", "registry.opentofu.org/hashicorp/azurerm", "/subscriptions/x/resourceGroups/rg"},
		{"random_id.c[0]", "managed", "0", "registry.opentofu.org/hashicorp/random", "c0"},
		{"random_id.c[1]", "managed", "1", "registry.opentofu.org/hashicorp/random", "c1"},
		{`module.m["a.b"].random_id.e["k.1"]`, "managed", "k.1", "registry.opentofu.org/hashicorp/random", "e1"},
		{"data.azurerm_client_config.cur", "data", "", "registry.opentofu.org/hashicorp/azurerm", "cc"},
	}
	for _, tt := range tests {
		t.Run(tt.address, func(t *testing.T) {
			r, err := idx.LookupResource(tt.address)
			if err != nil {
				t.Fatalf("LookupResource: %v", err)
			}
			if r.Mode != tt.mode {
				t.Errorf("Mode = %q, want %q", r.Mode, tt.mode)
			}
			if r.Key != tt.key {
				t.Errorf("Key = %q, want %q", r.Key, tt.key)
			}
			if r.Provider != tt.provider {
				t.Errorf("Provider = %q, want %q", r.Provider, tt.provider)
			}
			if r.Attributes["id"] != tt.id {
				t.Errorf("id = %v, want %q", r.Attributes["id"], tt.id)
			}
		})
	}

	t.Run("deposed objects are skipped", func(t *testing.T) {
		if got := len(idx.AllManagedResources()); got != 4 {
			t.Errorf("expected 4 managed instances, got %d", got)
		}
	})

	t.Run("numbers decode as json.Number", func(t *testing.T) {
		r, _ := idx.LookupResource("random_id.c[0]")
		if _, ok := r.Index.(json.Number); !ok {
			t.Errorf("Index type = %T, want json.Number", r.Index)
		}
		r, _ = idx.LookupResource(`module.m["a.b"].random_id.e["k.1"]`)
		if _, ok := r.Attributes["n"].(json.Number); !ok {
			t.Errorf("attribute n type = %T, want json.Number", r.Attributes["n"])
		}
	})

	t.Run("module exists", func(t *testing.T) {
		if !idx.ResourceExists(`module.m["a.b"]`) {
			t.Error(`expected module.m["a.b"] to exist`)
		}
	})
}

func TestParseRawState_Empty(t *testing.T) {
	tests := []struct {
		name string
		in   string
	}{
		{"empty", ""},
		{"whitespace", "  \n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := parseRawState([]byte(tt.in))
			if err != nil {
				t.Fatalf("parseRawState: %v", err)
			}
			if got := FlattenState(s); len(got) != 0 {
				t.Errorf("expected no resources, got %d", len(got))
			}
		})
	}
}

func TestParseRawState_Errors(t *testing.T) {
	tests := []struct {
		name string
		in   string
	}{
		{"invalid json", "{"},
		{"version 3", `{"version": 3, "modules": []}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := parseRawState([]byte(tt.in)); err == nil {
				t.Error("expected error")
			}
		})
	}
}

// TestReadState_FallsBackToRawStateOnSchemaVersionMismatch reproduces the
// failure seen after a provider upgrade: the state still records the old
// resource schema version, so `tofu show -json` refuses to render it
// ("schema version N for X in state does not match version M from the
// provider"). ReadState must fall back to `tofu state pull` and return the
// same addresses as before.
func TestReadState_FallsBackToRawStateOnSchemaVersionMismatch(t *testing.T) {
	tofuPath, dir, want := setupSchemaMismatchLayer(t)

	got := stateAddresses(t, NewTofuStateReader(tofuPath, nil), dir)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("fallback addresses = %v, want %v", got, want)
	}
}

// TestReadState_InitFailureIsNotMaskedByRawStateFallback asserts that when
// `tofu show -json` fails and the auto-init retry itself fails, ReadState
// returns the init error instead of falling back to `tofu state pull`, which
// would still read the state of the previously initialized backend.
func TestReadState_InitFailureIsNotMaskedByRawStateFallback(t *testing.T) {
	tofuPath, dir, _ := setupSchemaMismatchLayer(t)

	// The local backend accepts no "foo" argument, so `tofu init` fails.
	r := NewTofuStateReader(tofuPath, []string{"-backend-config=foo=bar"})
	_, err := r.ReadState(context.Background(), dir)
	if err == nil {
		t.Fatal("expected ReadState to fail with the tofu init error, got nil")
	}
	if !strings.Contains(err.Error(), "tofu init failed") {
		t.Errorf("expected tofu init error, got: %v", err)
	}
}

// TestReadState_ReportsStatePullError asserts that when both `tofu show -json`
// and the `tofu state pull` fallback fail, the error names both failures.
func TestReadState_ReportsStatePullError(t *testing.T) {
	tofuPath, dir, _ := setupSchemaMismatchLayer(t)

	// An uninstalled module makes both show -json and state pull fail
	// ("Module not installed").
	if err := os.WriteFile(filepath.Join(dir, "extra.tf"), []byte(`module "extra" { source = "./mod" }`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := NewTofuStateReader(tofuPath, nil).ReadState(context.Background(), dir)
	if err == nil {
		t.Fatal("expected ReadState to fail, got nil")
	}
	for _, want := range []string{"reading state for", "tofu state pull failed"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("expected error to contain %q, got: %v", want, err)
		}
	}
}

// setupSchemaMismatchLayer applies a small configuration to a local backend,
// records its state addresses, then rewrites every schema_version in the
// state file so that `tofu show -json` fails with a schema version mismatch.
// It returns the tofu path, the layer directory and the recorded addresses.
func setupSchemaMismatchLayer(t *testing.T) (string, string, []string) {
	t.Helper()
	tofuPath, err := exec.LookPath("tofu")
	if err != nil {
		t.Skip("tofu binary not found in PATH; skipping")
	}

	dir := t.TempDir()
	// terraform_data comes from the built-in provider, so no network access
	// is needed for init or apply.
	files := map[string]string{
		"main.tf": `
terraform {
  backend "local" {}
}
resource "terraform_data" "c" {
  count = 2
  input = count.index
}
resource "terraform_data" "e" {
  for_each = toset(["a.b"])
  input    = each.key
}
module "m" {
  source   = "./mod"
  for_each = toset(["x"])
}
`,
		"mod/main.tf": `resource "terraform_data" "inner" {}` + "\n",
	}
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	ctx := context.Background()
	tf, err := tfexec.NewTerraform(dir, tofuPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := tf.Init(ctx); err != nil {
		t.Fatalf("tofu init: %v", err)
	}
	if err := tf.Apply(ctx); err != nil {
		t.Fatalf("tofu apply: %v", err)
	}

	want := stateAddresses(t, NewTofuStateReader(tofuPath, nil), dir)

	// Simulate state written by a different provider schema version.
	statePath := filepath.Join(dir, "terraform.tfstate")
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	data = []byte(strings.ReplaceAll(string(data), `"schema_version": 0`, `"schema_version": 1`))
	if err := os.WriteFile(statePath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := tf.Show(ctx); err == nil || !strings.Contains(err.Error(), "does not match version") {
		t.Fatalf("expected tofu show to fail with a schema version mismatch, got: %v", err)
	}

	return tofuPath, dir, want
}

func stateAddresses(t *testing.T, r *TofuStateReader, dir string) []string {
	t.Helper()
	s, err := r.ReadState(context.Background(), dir)
	if err != nil {
		t.Fatalf("ReadState: %v", err)
	}
	var addrs []string
	for _, res := range FlattenState(s) {
		addrs = append(addrs, res.Address)
	}
	sort.Strings(addrs)
	return addrs
}

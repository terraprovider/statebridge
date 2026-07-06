package upload

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseHCLBackend(t *testing.T) {
	tests := []struct {
		name    string
		files   map[string]string
		wantSA  string
		wantCN  string
		wantRG  string
		wantKey string
		wantErr bool
	}{
		{
			name: "standard azurerm backend",
			files: map[string]string{
				"backend.tf": `
terraform {
  backend "azurerm" {
    storage_account_name = "mystorageacct"
    container_name       = "tfstate"
    key                  = "terraform.tfstate"
    resource_group_name  = "myrg"
  }
}
`,
			},
			wantSA:  "mystorageacct",
			wantCN:  "tfstate",
			wantRG:  "myrg",
			wantKey: "terraform.tfstate",
		},
		{
			name: "backend in separate file among other tf files",
			files: map[string]string{
				"main.tf": `
resource "azurerm_resource_group" "example" {
  name     = "example"
  location = "West Europe"
}
`,
				"backend.tf": `
terraform {
  backend "azurerm" {
    storage_account_name = "acct2"
    container_name       = "state2"
  }
}
`,
			},
			wantSA: "acct2",
			wantCN: "state2",
		},
		{
			name: "no backend block returns zero value",
			files: map[string]string{
				"main.tf": `
resource "azurerm_resource_group" "example" {
  name     = "example"
  location = "West Europe"
}
`,
			},
		},
		{
			name: "non-azurerm backend ignored",
			files: map[string]string{
				"backend.tf": `
terraform {
  backend "s3" {
    bucket = "my-bucket"
    key    = "terraform.tfstate"
  }
}
`,
			},
		},
		{
			name: "unparseable tf file skipped",
			files: map[string]string{
				"broken.tf": `this is not valid HCL at all!!!`,
				"backend.tf": `
terraform {
  backend "azurerm" {
    storage_account_name = "goodacct"
    container_name       = "goodcontainer"
  }
}
`,
			},
			wantSA: "goodacct",
			wantCN: "goodcontainer",
		},
		{
			name:  "empty directory",
			files: map[string]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			for name, content := range tt.files {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
			}

			cfg, err := ParseHCLBackend(dir)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if cfg.StorageAccountName != tt.wantSA {
				t.Errorf("StorageAccountName = %q, want %q", cfg.StorageAccountName, tt.wantSA)
			}
			if cfg.ContainerName != tt.wantCN {
				t.Errorf("ContainerName = %q, want %q", cfg.ContainerName, tt.wantCN)
			}
			if cfg.ResourceGroupName != tt.wantRG {
				t.Errorf("ResourceGroupName = %q, want %q", cfg.ResourceGroupName, tt.wantRG)
			}
			if cfg.Key != tt.wantKey {
				t.Errorf("Key = %q, want %q", cfg.Key, tt.wantKey)
			}
		})
	}
}

func TestParseInitArgs(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want map[string]string
	}{
		{
			name: "standard backend-config args",
			args: []string{
				"-backend-config=storage_account_name=myacct",
				"-backend-config=container_name=mycontainer",
				"-reconfigure",
			},
			want: map[string]string{
				"storage_account_name": "myacct",
				"container_name":       "mycontainer",
			},
		},
		{
			name: "unrecognized keys ignored",
			args: []string{
				"-backend-config=storage_account_name=acct",
				"-backend-config=key=terraform.tfstate",
				"-backend-config=access_key=secret123",
			},
			want: map[string]string{
				"storage_account_name": "acct",
				"key":                  "terraform.tfstate",
			},
		},
		{
			name: "raw key=value without prefix",
			args: []string{
				"storage_account_name=rawacct",
				"container_name=rawcontainer",
			},
			want: map[string]string{
				"storage_account_name": "rawacct",
				"container_name":       "rawcontainer",
			},
		},
		{
			name: "non-kv args skipped",
			args: []string{"-reconfigure", "-upgrade"},
			want: map[string]string{},
		},
		{
			name: "empty args",
			args: nil,
			want: map[string]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseInitArgs(".", tt.args)
			if len(got) != len(tt.want) {
				t.Fatalf("got %d entries, want %d: %v", len(got), len(tt.want), got)
			}
			for k, v := range tt.want {
				if got[k] != v {
					t.Errorf("key %q = %q, want %q", k, got[k], v)
				}
			}
		})
	}
}

func TestParseInitArgsWithHCLFile(t *testing.T) {
	dir := t.TempDir()

	// Create an HCL backend config file
	hclFile := filepath.Join(dir, "backend.hcl")
	err := os.WriteFile(hclFile, []byte(`
storage_account_name = "fileacct"
container_name       = "filecontainer"
resource_group_name  = "filerg"
key                  = "terraform.tfstate"
`), 0o644)
	if err != nil {
		t.Fatal(err)
	}

	args := []string{
		"-backend-config=" + hclFile,
		"-reconfigure",
	}

	got := ParseInitArgs(dir, args)

	if got["storage_account_name"] != "fileacct" {
		t.Errorf("storage_account_name = %q, want %q", got["storage_account_name"], "fileacct")
	}
	if got["container_name"] != "filecontainer" {
		t.Errorf("container_name = %q, want %q", got["container_name"], "filecontainer")
	}
	if got["resource_group_name"] != "filerg" {
		t.Errorf("resource_group_name = %q, want %q", got["resource_group_name"], "filerg")
	}
	// "key" is a recognized backend field and should be extracted.
	if got["key"] != "terraform.tfstate" {
		t.Errorf("key = %q, want %q", got["key"], "terraform.tfstate")
	}
}

func TestParseInitArgsWithPlainTextFile(t *testing.T) {
	dir := t.TempDir()

	// Create a plain text backend config file
	plainFile := filepath.Join(dir, "backend.conf")
	err := os.WriteFile(plainFile, []byte(`# Backend configuration
storage_account_name=plaintextacct
container_name=plaintextcontainer
resource_group_name=plaintextrg
`), 0o644)
	if err != nil {
		t.Fatal(err)
	}

	args := []string{
		"-backend-config=" + plainFile,
	}

	got := ParseInitArgs(dir, args)

	if got["storage_account_name"] != "plaintextacct" {
		t.Errorf("storage_account_name = %q, want %q", got["storage_account_name"], "plaintextacct")
	}
	if got["container_name"] != "plaintextcontainer" {
		t.Errorf("container_name = %q, want %q", got["container_name"], "plaintextcontainer")
	}
	if got["resource_group_name"] != "plaintextrg" {
		t.Errorf("resource_group_name = %q, want %q", got["resource_group_name"], "plaintextrg")
	}
}

func TestParseInitArgsFileMixedWithInline(t *testing.T) {
	dir := t.TempDir()

	// File provides base values
	hclFile := filepath.Join(dir, "base.hcl")
	err := os.WriteFile(hclFile, []byte(`
storage_account_name = "baseacct"
container_name       = "basecontainer"
`), 0o644)
	if err != nil {
		t.Fatal(err)
	}

	// Inline arg overrides storage_account_name
	args := []string{
		"-backend-config=" + hclFile,
		"-backend-config=storage_account_name=overrideacct",
	}

	got := ParseInitArgs(dir, args)

	// Inline should override file value (processed after)
	if got["storage_account_name"] != "overrideacct" {
		t.Errorf("storage_account_name = %q, want %q (inline should override file)", got["storage_account_name"], "overrideacct")
	}
	if got["container_name"] != "basecontainer" {
		t.Errorf("container_name = %q, want %q", got["container_name"], "basecontainer")
	}
}

func TestParseInitArgsNonexistentFile(t *testing.T) {
	// File that doesn't exist should produce a warning but not crash
	args := []string{
		"-backend-config=/nonexistent/path/backend.hcl",
		"-backend-config=storage_account_name=fallback",
	}

	got := ParseInitArgs(".", args)

	// The inline arg should still work
	if got["storage_account_name"] != "fallback" {
		t.Errorf("storage_account_name = %q, want %q", got["storage_account_name"], "fallback")
	}
}

func TestParseInitArgsPlainTextWithQuotes(t *testing.T) {
	dir := t.TempDir()

	// Plain text with quoted values (some configs have this)
	plainFile := filepath.Join(dir, "backend.conf")
	err := os.WriteFile(plainFile, []byte(`storage_account_name="quotedacct"
container_name='quotedcontainer'
`), 0o644)
	if err != nil {
		t.Fatal(err)
	}

	args := []string{"-backend-config=" + plainFile}
	got := ParseInitArgs(dir, args)

	if got["storage_account_name"] != "quotedacct" {
		t.Errorf("storage_account_name = %q, want %q", got["storage_account_name"], "quotedacct")
	}
	if got["container_name"] != "quotedcontainer" {
		t.Errorf("container_name = %q, want %q", got["container_name"], "quotedcontainer")
	}
}

func TestParseInitArgsRelativeFileResolution(t *testing.T) {
	dir := t.TempDir()
	hclFile := filepath.Join(dir, "backend.hcl")
	content := `storage_account_name = "relacct"
container_name = "relcontainer"
`
	if err := os.WriteFile(hclFile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	// Pass relative filename only — ParseInitArgs should resolve against layerPath (dir)
	args := []string{"-backend-config=backend.hcl"}
	got := ParseInitArgs(dir, args)

	if got["storage_account_name"] != "relacct" {
		t.Errorf("storage_account_name = %q, want %q", got["storage_account_name"], "relacct")
	}
	if got["container_name"] != "relcontainer" {
		t.Errorf("container_name = %q, want %q", got["container_name"], "relcontainer")
	}
}

func TestMergeBackendConfig(t *testing.T) {
	base := &BackendConfig{
		StorageAccountName: "inline_acct",
		ContainerName:      "inline_container",
		ResourceGroupName:  "inline_rg",
		Key:                "inline.tfstate",
	}

	overrides := map[string]string{
		"storage_account_name": "override_acct",
		// container_name not overridden
		"resource_group_name": "override_rg",
		"key":                 "override.tfstate",
	}

	result := MergeBackendConfig(base, overrides)

	if result.StorageAccountName != "override_acct" {
		t.Errorf("StorageAccountName = %q, want %q", result.StorageAccountName, "override_acct")
	}
	if result.ContainerName != "inline_container" {
		t.Errorf("ContainerName = %q, want %q", result.ContainerName, "inline_container")
	}
	if result.ResourceGroupName != "override_rg" {
		t.Errorf("ResourceGroupName = %q, want %q", result.ResourceGroupName, "override_rg")
	}
	if result.Key != "override.tfstate" {
		t.Errorf("Key = %q, want %q", result.Key, "override.tfstate")
	}

	// Verify base was not modified
	if base.StorageAccountName != "inline_acct" {
		t.Error("base was modified")
	}
}

func TestBackendConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  BackendConfig
		wantErr bool
	}{
		{
			name:    "valid config",
			config:  BackendConfig{StorageAccountName: "acct", ContainerName: "container"},
			wantErr: false,
		},
		{
			name:    "valid config with key",
			config:  BackendConfig{StorageAccountName: "acct", ContainerName: "container", Key: "layer.tfstate"},
			wantErr: false,
		},
		{
			name:    "missing storage account",
			config:  BackendConfig{ContainerName: "container"},
			wantErr: true,
		},
		{
			name:    "missing container",
			config:  BackendConfig{StorageAccountName: "acct"},
			wantErr: true,
		},
		{
			name:    "both missing",
			config:  BackendConfig{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestDiscoverBackendConfig(t *testing.T) {
	t.Run("merges HCL and init args", func(t *testing.T) {
		dir := t.TempDir()
		err := os.WriteFile(filepath.Join(dir, "backend.tf"), []byte(`
terraform {
  backend "azurerm" {
    storage_account_name = "hclacct"
    container_name       = "hclcontainer"
    resource_group_name  = "hclrg"
  }
}
`), 0o644)
		if err != nil {
			t.Fatal(err)
		}

		initArgs := []string{
			"-backend-config=storage_account_name=overrideacct",
			"-reconfigure",
		}

		cfg, err := DiscoverBackendConfig(dir, initArgs)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if cfg.StorageAccountName != "overrideacct" {
			t.Errorf("StorageAccountName = %q, want %q", cfg.StorageAccountName, "overrideacct")
		}
		if cfg.ContainerName != "hclcontainer" {
			t.Errorf("ContainerName = %q, want %q", cfg.ContainerName, "hclcontainer")
		}
		if cfg.ResourceGroupName != "hclrg" {
			t.Errorf("ResourceGroupName = %q, want %q", cfg.ResourceGroupName, "hclrg")
		}
	})

	t.Run("fails when no config found", func(t *testing.T) {
		dir := t.TempDir()
		_, err := DiscoverBackendConfig(dir, nil)
		if err == nil {
			t.Fatal("expected error for empty config")
		}
	})

	t.Run("init args only", func(t *testing.T) {
		dir := t.TempDir()
		initArgs := []string{
			"-backend-config=storage_account_name=argacct",
			"-backend-config=container_name=argcontainer",
		}

		cfg, err := DiscoverBackendConfig(dir, initArgs)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if cfg.StorageAccountName != "argacct" {
			t.Errorf("StorageAccountName = %q, want %q", cfg.StorageAccountName, "argacct")
		}
		if cfg.ContainerName != "argcontainer" {
			t.Errorf("ContainerName = %q, want %q", cfg.ContainerName, "argcontainer")
		}
	})
}

func TestParseHCLBackend_CredentialFields(t *testing.T) {
	dir := t.TempDir()
	content := `
terraform {
  backend "azurerm" {
    storage_account_name = "credacct"
    container_name        = "credcontainer"
    client_id             = "hcl-client"
    tenant_id             = "hcl-tenant"
    use_oidc              = true
    use_msi               = false
  }
}
`
	if err := os.WriteFile(filepath.Join(dir, "backend.tf"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := ParseHCLBackend(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := map[string]string{
		"client_id": "hcl-client",
		"tenant_id": "hcl-tenant",
		"use_oidc":  "true",
		"use_msi":   "false",
	}
	for k, v := range want {
		if cfg.Credentials[k] != v {
			t.Errorf("Credentials[%q] = %q, want %q", k, cfg.Credentials[k], v)
		}
	}
	// Only the attributes present in the block should be populated.
	if len(cfg.Credentials) != len(want) {
		t.Errorf("Credentials = %v, want exactly %v", cfg.Credentials, want)
	}
}

func TestParseHCLBackend_NoCredentialFields(t *testing.T) {
	dir := t.TempDir()
	content := `
terraform {
  backend "azurerm" {
    storage_account_name = "acct"
    container_name        = "container"
  }
}
`
	if err := os.WriteFile(filepath.Join(dir, "backend.tf"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := ParseHCLBackend(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Credentials) != 0 {
		t.Errorf("expected no Credentials, got %v", cfg.Credentials)
	}
}

func TestParseInitArgs_CredentialKeys(t *testing.T) {
	args := []string{
		"-backend-config=client_id=cli-client",
		"-backend-config=tenant_id=cli-tenant",
		"-backend-config=use_oidc=true",
		"-backend-config=client_secret=cli-secret",
	}

	got := ParseInitArgs(".", args)

	want := map[string]string{
		"client_id":     "cli-client",
		"tenant_id":     "cli-tenant",
		"use_oidc":      "true",
		"client_secret": "cli-secret",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
}

func TestMergeBackendConfig_Credentials(t *testing.T) {
	base := &BackendConfig{
		StorageAccountName: "acct",
		ContainerName:      "container",
		Credentials: map[string]string{
			"client_id": "inline-client",
			"tenant_id": "inline-tenant",
		},
	}

	overrides := map[string]string{
		"tenant_id":     "override-tenant", // should win over inline
		"client_secret": "override-secret", // new key, not present inline
	}

	result := MergeBackendConfig(base, overrides)

	if result.Credentials["client_id"] != "inline-client" {
		t.Errorf("Credentials[client_id] = %q, want %q (preserved from base)", result.Credentials["client_id"], "inline-client")
	}
	if result.Credentials["tenant_id"] != "override-tenant" {
		t.Errorf("Credentials[tenant_id] = %q, want %q (override should win)", result.Credentials["tenant_id"], "override-tenant")
	}
	if result.Credentials["client_secret"] != "override-secret" {
		t.Errorf("Credentials[client_secret] = %q, want %q", result.Credentials["client_secret"], "override-secret")
	}

	// Base must not be mutated by the merge.
	if base.Credentials["tenant_id"] != "inline-tenant" {
		t.Errorf("base.Credentials[tenant_id] was mutated: got %q, want %q", base.Credentials["tenant_id"], "inline-tenant")
	}
	if _, ok := base.Credentials["client_secret"]; ok {
		t.Error("base.Credentials should not have gained client_secret")
	}
}

func TestMergeBackendConfig_NilBaseCredentials(t *testing.T) {
	base := &BackendConfig{StorageAccountName: "acct", ContainerName: "container"}
	overrides := map[string]string{"client_id": "override-client"}

	result := MergeBackendConfig(base, overrides)

	if result.Credentials["client_id"] != "override-client" {
		t.Errorf("Credentials[client_id] = %q, want %q", result.Credentials["client_id"], "override-client")
	}
	if base.Credentials != nil {
		t.Error("base.Credentials should remain nil")
	}
}

func TestDiscoverBackendConfig_CredentialsFromInlineAndOverride(t *testing.T) {
	dir := t.TempDir()
	content := `
terraform {
  backend "azurerm" {
    storage_account_name = "acct"
    container_name        = "container"
    client_id             = "inline-client"
    tenant_id             = "inline-tenant"
  }
}
`
	if err := os.WriteFile(filepath.Join(dir, "backend.tf"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	initArgs := []string{"-backend-config=tenant_id=override-tenant"}

	cfg, err := DiscoverBackendConfig(dir, initArgs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Credentials["client_id"] != "inline-client" {
		t.Errorf("Credentials[client_id] = %q, want %q", cfg.Credentials["client_id"], "inline-client")
	}
	if cfg.Credentials["tenant_id"] != "override-tenant" {
		t.Errorf("Credentials[tenant_id] = %q, want %q (backend-config override should win)", cfg.Credentials["tenant_id"], "override-tenant")
	}
}

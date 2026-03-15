package upload

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseHCLBackend(t *testing.T) {
	tests := []struct {
		name       string
		files      map[string]string
		wantType   BackendType
		wantSA     string // azurerm
		wantCN     string // azurerm
		wantRG     string // azurerm
		wantBucket string // s3/gcs
		wantErr    bool
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
			wantType: BackendAzurerm,
			wantSA:   "mystorageacct",
			wantCN:   "tfstate",
			wantRG:   "myrg",
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
			wantType: BackendAzurerm,
			wantSA:   "acct2",
			wantCN:   "state2",
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
			name: "s3 backend",
			files: map[string]string{
				"backend.tf": `
terraform {
  backend "s3" {
    bucket = "my-bucket"
    key    = "terraform.tfstate"
    region = "us-east-1"
  }
}
`,
			},
			wantType:   BackendS3,
			wantBucket: "my-bucket",
		},
		{
			name: "gcs backend",
			files: map[string]string{
				"backend.tf": `
terraform {
  backend "gcs" {
    bucket = "my-gcs-bucket"
    prefix = "terraform/state"
  }
}
`,
			},
			wantType:   BackendGCS,
			wantBucket: "my-gcs-bucket",
		},
		{
			name: "unsupported backend ignored",
			files: map[string]string{
				"backend.tf": `
terraform {
  backend "consul" {
    address = "demo.consul.io"
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
			wantType: BackendAzurerm,
			wantSA:   "goodacct",
			wantCN:   "goodcontainer",
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

			cfg, bt, err := ParseHCLBackend(dir)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if bt != tt.wantType {
				t.Errorf("BackendType = %q, want %q", bt, tt.wantType)
			}

			switch tt.wantType {
			case BackendAzurerm:
				az, ok := cfg.(*AzurermBackendConfig)
				if !ok {
					t.Fatalf("expected *AzurermBackendConfig, got %T", cfg)
				}
				if az.StorageAccountName != tt.wantSA {
					t.Errorf("StorageAccountName = %q, want %q", az.StorageAccountName, tt.wantSA)
				}
				if az.ContainerName != tt.wantCN {
					t.Errorf("ContainerName = %q, want %q", az.ContainerName, tt.wantCN)
				}
				if az.ResourceGroupName != tt.wantRG {
					t.Errorf("ResourceGroupName = %q, want %q", az.ResourceGroupName, tt.wantRG)
				}
			case BackendS3:
				s3, ok := cfg.(*S3BackendConfig)
				if !ok {
					t.Fatalf("expected *S3BackendConfig, got %T", cfg)
				}
				if s3.Bucket != tt.wantBucket {
					t.Errorf("Bucket = %q, want %q", s3.Bucket, tt.wantBucket)
				}
			case BackendGCS:
				gcs, ok := cfg.(*GCSBackendConfig)
				if !ok {
					t.Fatalf("expected *GCSBackendConfig, got %T", cfg)
				}
				if gcs.Bucket != tt.wantBucket {
					t.Errorf("Bucket = %q, want %q", gcs.Bucket, tt.wantBucket)
				}
			case "":
				if cfg != nil {
					t.Errorf("expected nil config for empty type, got %T", cfg)
				}
			}
		})
	}
}

func TestParseInitArgs(t *testing.T) {
	tests := []struct {
		name        string
		backendType BackendType
		args        []string
		want        map[string]string
	}{
		{
			name:        "standard backend-config args",
			backendType: BackendAzurerm,
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
			name:        "unrecognized keys ignored",
			backendType: BackendAzurerm,
			args: []string{
				"-backend-config=storage_account_name=acct",
				"-backend-config=key=terraform.tfstate",
				"-backend-config=access_key=secret123",
			},
			want: map[string]string{
				"storage_account_name": "acct",
			},
		},
		{
			name:        "raw key=value without prefix",
			backendType: BackendAzurerm,
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
			name:        "non-kv args skipped",
			backendType: BackendAzurerm,
			args:        []string{"-reconfigure", "-upgrade"},
			want:        map[string]string{},
		},
		{
			name:        "empty args",
			backendType: BackendAzurerm,
			args:        nil,
			want:        map[string]string{},
		},
		{
			name:        "s3 backend args",
			backendType: BackendS3,
			args: []string{
				"-backend-config=bucket=my-bucket",
				"-backend-config=region=us-east-1",
			},
			want: map[string]string{
				"bucket": "my-bucket",
				"region": "us-east-1",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseInitArgs(".", tt.backendType, tt.args)
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

	got := ParseInitArgs(dir, BackendAzurerm, args)

	if got["storage_account_name"] != "fileacct" {
		t.Errorf("storage_account_name = %q, want %q", got["storage_account_name"], "fileacct")
	}
	if got["container_name"] != "filecontainer" {
		t.Errorf("container_name = %q, want %q", got["container_name"], "filecontainer")
	}
	if got["resource_group_name"] != "filerg" {
		t.Errorf("resource_group_name = %q, want %q", got["resource_group_name"], "filerg")
	}
	// "key" should not be in the result (not a recognized field)
	if _, ok := got["key"]; ok {
		t.Error("key should not be in result (not recognized)")
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

	got := ParseInitArgs(dir, BackendAzurerm, args)

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

	got := ParseInitArgs(dir, BackendAzurerm, args)

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

	got := ParseInitArgs(".", BackendAzurerm, args)

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
	got := ParseInitArgs(dir, BackendAzurerm, args)

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
	got := ParseInitArgs(dir, BackendAzurerm, args)

	if got["storage_account_name"] != "relacct" {
		t.Errorf("storage_account_name = %q, want %q", got["storage_account_name"], "relacct")
	}
	if got["container_name"] != "relcontainer" {
		t.Errorf("container_name = %q, want %q", got["container_name"], "relcontainer")
	}
}

func TestMergeBackendConfig(t *testing.T) {
	base := &AzurermBackendConfig{
		StorageAccountName: "inline_acct",
		ContainerName:      "inline_container",
		ResourceGroupName:  "inline_rg",
	}

	overrides := map[string]string{
		"storage_account_name": "override_acct",
		// container_name not overridden
		"resource_group_name": "override_rg",
	}

	result := MergeBackendConfig(base, BackendAzurerm, overrides)

	az, ok := result.(*AzurermBackendConfig)
	if !ok {
		t.Fatalf("expected *AzurermBackendConfig, got %T", result)
	}

	if az.StorageAccountName != "override_acct" {
		t.Errorf("StorageAccountName = %q, want %q", az.StorageAccountName, "override_acct")
	}
	if az.ContainerName != "inline_container" {
		t.Errorf("ContainerName = %q, want %q", az.ContainerName, "inline_container")
	}
	if az.ResourceGroupName != "override_rg" {
		t.Errorf("ResourceGroupName = %q, want %q", az.ResourceGroupName, "override_rg")
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
			name:    "valid azurerm config",
			config:  &AzurermBackendConfig{StorageAccountName: "acct", ContainerName: "container"},
			wantErr: false,
		},
		{
			name:    "missing storage account",
			config:  &AzurermBackendConfig{ContainerName: "container"},
			wantErr: true,
		},
		{
			name:    "missing container",
			config:  &AzurermBackendConfig{StorageAccountName: "acct"},
			wantErr: true,
		},
		{
			name:    "both missing",
			config:  &AzurermBackendConfig{},
			wantErr: true,
		},
		{
			name:    "valid s3 config",
			config:  &S3BackendConfig{Bucket: "my-bucket"},
			wantErr: false,
		},
		{
			name:    "s3 missing bucket",
			config:  &S3BackendConfig{},
			wantErr: true,
		},
		{
			name:    "valid gcs config",
			config:  &GCSBackendConfig{Bucket: "gcs-bucket"},
			wantErr: false,
		},
		{
			name:    "gcs missing bucket",
			config:  &GCSBackendConfig{},
			wantErr: true,
		},
		{
			name:    "valid local config",
			config:  &LocalBackendConfig{Path: "/tmp/state"},
			wantErr: false,
		},
		{
			name:    "local missing path",
			config:  &LocalBackendConfig{},
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

		az, ok := cfg.(*AzurermBackendConfig)
		if !ok {
			t.Fatalf("expected *AzurermBackendConfig, got %T", cfg)
		}

		if az.StorageAccountName != "overrideacct" {
			t.Errorf("StorageAccountName = %q, want %q", az.StorageAccountName, "overrideacct")
		}
		if az.ContainerName != "hclcontainer" {
			t.Errorf("ContainerName = %q, want %q", az.ContainerName, "hclcontainer")
		}
		if az.ResourceGroupName != "hclrg" {
			t.Errorf("ResourceGroupName = %q, want %q", az.ResourceGroupName, "hclrg")
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
		// Write a backend block so the type is detected
		err := os.WriteFile(filepath.Join(dir, "backend.tf"), []byte(`
terraform {
  backend "azurerm" {}
}
`), 0o644)
		if err != nil {
			t.Fatal(err)
		}
		initArgs := []string{
			"-backend-config=storage_account_name=argacct",
			"-backend-config=container_name=argcontainer",
		}

		cfg, err := DiscoverBackendConfig(dir, initArgs)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		az, ok := cfg.(*AzurermBackendConfig)
		if !ok {
			t.Fatalf("expected *AzurermBackendConfig, got %T", cfg)
		}

		if az.StorageAccountName != "argacct" {
			t.Errorf("StorageAccountName = %q, want %q", az.StorageAccountName, "argacct")
		}
		if az.ContainerName != "argcontainer" {
			t.Errorf("ContainerName = %q, want %q", az.ContainerName, "argcontainer")
		}
	})

	t.Run("s3 backend", func(t *testing.T) {
		dir := t.TempDir()
		err := os.WriteFile(filepath.Join(dir, "backend.tf"), []byte(`
terraform {
  backend "s3" {
    bucket = "my-bucket"
    region = "us-east-1"
  }
}
`), 0o644)
		if err != nil {
			t.Fatal(err)
		}

		cfg, err := DiscoverBackendConfig(dir, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		s3, ok := cfg.(*S3BackendConfig)
		if !ok {
			t.Fatalf("expected *S3BackendConfig, got %T", cfg)
		}

		if s3.Bucket != "my-bucket" {
			t.Errorf("Bucket = %q, want %q", s3.Bucket, "my-bucket")
		}
		if s3.Region != "us-east-1" {
			t.Errorf("Region = %q, want %q", s3.Region, "us-east-1")
		}
	})
}

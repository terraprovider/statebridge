package upload

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
)

// fakeCredential is a distinguishable azcore.TokenCredential stand-in used to
// verify identity (pointer equality) of the credential returned by
// ResolveCredential, without performing any real token acquisition.
type fakeCredential struct{}

func (f *fakeCredential) GetToken(context.Context, policy.TokenRequestOptions) (azcore.AccessToken, error) {
	return azcore.AccessToken{}, nil
}

func TestResolveCredential_NilConfigReturnsBase(t *testing.T) {
	base := &fakeCredential{}

	got, err := ResolveCredential(base, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != azcore.TokenCredential(base) {
		t.Error("expected base credential to be returned unchanged for nil config")
	}
}

func TestResolveCredential_EmptyCredentialsReturnsBase(t *testing.T) {
	base := &fakeCredential{}
	config := &BackendConfig{StorageAccountName: "acct", ContainerName: "container"}

	got, err := ResolveCredential(base, config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != azcore.TokenCredential(base) {
		t.Error("expected base credential to be returned unchanged when Credentials is empty")
	}
}

func TestResolveCredential_WithCredentialsBuildsNewCredential(t *testing.T) {
	base := &fakeCredential{}
	config := &BackendConfig{
		StorageAccountName: "acct",
		ContainerName:      "container",
		Credentials: map[string]string{
			"client_id": "layer-client",
			"tenant_id": "layer-tenant",
			"use_cli":   "true",
		},
	}

	got, err := ResolveCredential(base, config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Fatal("expected a non-nil credential")
	}
	if got == azcore.TokenCredential(base) {
		t.Error("expected a distinct credential to be built, not the base credential")
	}
}

func TestResolveCredential_InvalidBoolValuePropagatesError(t *testing.T) {
	base := &fakeCredential{}
	config := &BackendConfig{
		StorageAccountName: "acct",
		ContainerName:      "container",
		Credentials: map[string]string{
			"client_id": "layer-client",
			"tenant_id": "layer-tenant",
			"use_msi":   "not-a-bool",
		},
	}

	_, err := ResolveCredential(base, config)
	if err == nil {
		t.Fatal("expected error for invalid use_msi value")
	}
}

func TestResolveCredential_MissingRequiredFieldsErrors(t *testing.T) {
	base := &fakeCredential{}
	// Only tenant_id supplied, no client_id from this source or the
	// environment (test relies on ARM_CLIENT_ID not being set in the test
	// environment).
	t.Setenv("ARM_CLIENT_ID", "")
	t.Setenv("ARM_TENANT_ID", "")
	config := &BackendConfig{
		StorageAccountName: "acct",
		ContainerName:      "container",
		Credentials: map[string]string{
			"tenant_id": "layer-tenant",
		},
	}

	_, err := ResolveCredential(base, config)
	if err == nil {
		t.Fatal("expected error due to missing client id")
	}
}

func writeAzurermBackendTf(t *testing.T, layerPath string, extraAttrs string) {
	t.Helper()
	if err := os.MkdirAll(layerPath, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "terraform {\n  backend \"azurerm\" {\n    storage_account_name = \"layeracct\"\n    container_name       = \"layercontainer\"\n" +
		extraAttrs + "  }\n}\n"
	if err := os.WriteFile(filepath.Join(layerPath, "backend.tf"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestManagerGetUploader_UsesPerLayerCredential(t *testing.T) {
	dir := t.TempDir()
	layerPath := filepath.Join(dir, "layers", "app")
	writeAzurermBackendTf(t, layerPath, `    client_id = "layer-client"
    tenant_id = "layer-tenant"
    use_cli   = true
`)

	base := &fakeCredential{}
	var gotCred azcore.TokenCredential
	mgr := NewManager(base, nil)
	mgr.WithUploaderFactory(func(_, _ string, cred azcore.TokenCredential) (BlobUploader, error) {
		gotCred = cred
		return newMockUploader(), nil
	})

	if _, err := mgr.getUploader(layerPath); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotCred == azcore.TokenCredential(base) {
		t.Error("expected uploader factory to receive a layer-specific credential, not the base credential")
	}
}

func TestManagerGetUploader_NoCredentialsUsesBase(t *testing.T) {
	dir := t.TempDir()
	layerPath := filepath.Join(dir, "layers", "app")
	writeAzurermBackendTf(t, layerPath, "")

	base := &fakeCredential{}
	var gotCred azcore.TokenCredential
	mgr := NewManager(base, nil)
	mgr.WithUploaderFactory(func(_, _ string, cred azcore.TokenCredential) (BlobUploader, error) {
		gotCred = cred
		return newMockUploader(), nil
	})

	if _, err := mgr.getUploader(layerPath); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotCred != azcore.TokenCredential(base) {
		t.Error("expected uploader factory to receive the base credential when the layer has no credential values")
	}
}

func TestManagerGetUploader_CredentialErrorPropagates(t *testing.T) {
	dir := t.TempDir()
	layerPath := filepath.Join(dir, "layers", "app")
	writeAzurermBackendTf(t, layerPath, `    client_id = "layer-client"
    tenant_id = "layer-tenant"
    use_msi   = "not-a-bool"
`)

	mgr := NewManager(&fakeCredential{}, nil)
	mgr.WithUploaderFactory(func(_, _ string, _ azcore.TokenCredential) (BlobUploader, error) {
		return newMockUploader(), nil
	})

	_, err := mgr.getUploader(layerPath)
	if err == nil {
		t.Fatal("expected error due to invalid use_msi value in layer backend config")
	}
	if !strings.Contains(err.Error(), layerPath) {
		t.Errorf("expected error to mention layer path %q, got: %v", layerPath, err)
	}
}

func TestManagerGetCredential_CachedPerLayer(t *testing.T) {
	dir := t.TempDir()
	layerPath := filepath.Join(dir, "layers", "app")
	writeAzurermBackendTf(t, layerPath, `    client_id = "layer-client"
    tenant_id = "layer-tenant"
    use_cli   = true
`)

	mgr := NewManager(&fakeCredential{}, nil)
	config, err := mgr.getBackendConfig(layerPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	first, err := mgr.getCredential(layerPath, config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	second, err := mgr.getCredential(layerPath, config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if first != second {
		t.Error("expected the same cached credential instance to be returned for the same layer path")
	}
}

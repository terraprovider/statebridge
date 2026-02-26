package auth

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"testing"
)

func TestNewCredentialConfiguration_MissingClientID(t *testing.T) {
	_, err := NewCredentialConfiguration(
		WithClientAndTenant("", "tenant-id"),
	)
	if err == nil {
		t.Fatal("expected error for empty client id")
	}
	if err.Error() != "client id is empty" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestNewCredentialConfiguration_MissingTenantID(t *testing.T) {
	_, err := NewCredentialConfiguration(
		WithClientAndTenant("client-id", ""),
	)
	if err == nil {
		t.Fatal("expected error for empty tenant id")
	}
	if err.Error() != "tenant id is empty" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestNewCredentialConfiguration_ValidClientAndTenant(t *testing.T) {
	cfg, err := NewCredentialConfiguration(
		WithClientAndTenant("my-client", "my-tenant"),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ClientId != "my-client" {
		t.Errorf("ClientId = %q, want %q", cfg.ClientId, "my-client")
	}
	if cfg.TenantId != "my-tenant" {
		t.Errorf("TenantId = %q, want %q", cfg.TenantId, "my-tenant")
	}
}

func TestWithClientSecret(t *testing.T) {
	cfg, err := NewCredentialConfiguration(
		WithClientAndTenant("client", "tenant"),
		WithClientSecret("my-secret"),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.clientSecret == nil || *cfg.clientSecret != "my-secret" {
		t.Errorf("clientSecret = %v, want %q", cfg.clientSecret, "my-secret")
	}
}

func TestWithAzCli(t *testing.T) {
	cfg, err := NewCredentialConfiguration(
		WithClientAndTenant("client", "tenant"),
		WithAzCli(true),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.useAzCli == nil || !*cfg.useAzCli {
		t.Error("expected useAzCli to be true")
	}
}

func TestWithAzCli_False(t *testing.T) {
	cfg, err := NewCredentialConfiguration(
		WithClientAndTenant("client", "tenant"),
		WithAzCli(false),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.useAzCli == nil || *cfg.useAzCli {
		t.Error("expected useAzCli to be false")
	}
}

func TestWithMsi(t *testing.T) {
	msiClientID := "msi-client-id"
	cfg, err := NewCredentialConfiguration(
		WithClientAndTenant("client", "tenant"),
		WithMsi(true, &msiClientID),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.useMsi == nil || !*cfg.useMsi {
		t.Error("expected useMsi to be true")
	}
	if cfg.msiClientId == nil || *cfg.msiClientId != msiClientID {
		t.Errorf("msiClientId = %v, want %q", cfg.msiClientId, msiClientID)
	}
}

func TestWithMsi_NilClientID(t *testing.T) {
	cfg, err := NewCredentialConfiguration(
		WithClientAndTenant("client", "tenant"),
		WithMsi(true, nil),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.useMsi == nil || !*cfg.useMsi {
		t.Error("expected useMsi to be true")
	}
	if cfg.msiClientId != nil {
		t.Errorf("expected msiClientId to be nil, got %v", cfg.msiClientId)
	}
}

func TestWithOidc(t *testing.T) {
	cfg, err := NewCredentialConfiguration(
		WithClientAndTenant("client", "tenant"),
		WithOidc(true),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.useOidc == nil || !*cfg.useOidc {
		t.Error("expected useOidc to be true")
	}
}

func TestWithClientCertificate_Valid(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}
	cert := &x509.Certificate{}
	cfg, err := NewCredentialConfiguration(
		WithClientAndTenant("client", "tenant"),
		WithClientCertificate(cert, key),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.clientCertificate != cert {
		t.Error("certificate not set correctly")
	}
	if cfg.clientCertificatePrivateKey == nil {
		t.Error("private key not set")
	}
}

func TestWithClientCertificate_NilCert(t *testing.T) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	_, err := NewCredentialConfiguration(
		WithClientAndTenant("client", "tenant"),
		WithClientCertificate(nil, key),
	)
	if err == nil {
		t.Fatal("expected error for nil certificate")
	}
	if err.Error() != "certificate is nil" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestWithClientCertificate_NilKey(t *testing.T) {
	cert := &x509.Certificate{}
	_, err := NewCredentialConfiguration(
		WithClientAndTenant("client", "tenant"),
		WithClientCertificate(cert, nil),
	)
	if err == nil {
		t.Fatal("expected error for nil key")
	}
	if err.Error() != "key is nil" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestWithClientCertificateFromFile_NonexistentFile(t *testing.T) {
	_, err := NewCredentialConfiguration(
		WithClientAndTenant("client", "tenant"),
		WithClientCertificateFromFile("/nonexistent/cert.pfx", "password"),
	)
	if err == nil {
		t.Fatal("expected error for nonexistent certificate file")
	}
}

func TestParseEnv_WithArmPrefix(t *testing.T) {
	t.Setenv("ARM_CLIENT_ID", "env-client")
	t.Setenv("ARM_TENANT_ID", "env-tenant")
	t.Setenv("ARM_CLIENT_SECRET", "env-secret")

	cfg, err := NewCredentialConfiguration(
		WithDefaultEnvironmentVariables(),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ClientId != "env-client" {
		t.Errorf("ClientId = %q, want %q", cfg.ClientId, "env-client")
	}
	if cfg.TenantId != "env-tenant" {
		t.Errorf("TenantId = %q, want %q", cfg.TenantId, "env-tenant")
	}
	if cfg.clientSecret == nil || *cfg.clientSecret != "env-secret" {
		t.Errorf("clientSecret = %v, want %q", cfg.clientSecret, "env-secret")
	}
}

func TestParseEnv_WithCustomPrefix(t *testing.T) {
	t.Setenv("CUSTOM_CLIENT_ID", "custom-client")
	t.Setenv("CUSTOM_TENANT_ID", "custom-tenant")

	cfg, err := NewCredentialConfiguration(
		WithEnvironmentVariablePrefixes("CUSTOM"),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ClientId != "custom-client" {
		t.Errorf("ClientId = %q, want %q", cfg.ClientId, "custom-client")
	}
	if cfg.TenantId != "custom-tenant" {
		t.Errorf("TenantId = %q, want %q", cfg.TenantId, "custom-tenant")
	}
}

func TestParseEnv_PrefixWithTrailingUnderscore(t *testing.T) {
	t.Setenv("ARM_CLIENT_ID", "env-client")
	t.Setenv("ARM_TENANT_ID", "env-tenant")

	// Prefix "ARM_" should be normalized to "ARM"
	cfg, err := NewCredentialConfiguration(
		WithEnvironmentVariablePrefixes("ARM_"),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ClientId != "env-client" {
		t.Errorf("ClientId = %q, want %q", cfg.ClientId, "env-client")
	}
}

func TestParseEnv_UseCliAndMsi(t *testing.T) {
	t.Setenv("ARM_CLIENT_ID", "client")
	t.Setenv("ARM_TENANT_ID", "tenant")
	t.Setenv("ARM_USE_CLI", "true")
	t.Setenv("ARM_USE_MSI", "false")

	cfg, err := NewCredentialConfiguration(
		WithDefaultEnvironmentVariables(),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.useAzCli == nil || !*cfg.useAzCli {
		t.Error("expected useAzCli to be true")
	}
	if cfg.useMsi == nil || *cfg.useMsi {
		t.Error("expected useMsi to be false")
	}
}

func TestParseEnv_OidcFields(t *testing.T) {
	t.Setenv("ARM_CLIENT_ID", "client")
	t.Setenv("ARM_TENANT_ID", "tenant")
	t.Setenv("ARM_USE_OIDC", "true")
	t.Setenv("ARM_OIDC_TOKEN", "my-oidc-token")
	t.Setenv("ARM_OIDC_REQUEST_URL", "https://token.example.com")
	t.Setenv("ARM_OIDC_REQUEST_TOKEN", "request-bearer")

	cfg, err := NewCredentialConfiguration(
		WithDefaultEnvironmentVariables(),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.useOidc == nil || !*cfg.useOidc {
		t.Error("expected useOidc to be true")
	}
	if cfg.oidcToken == nil || *cfg.oidcToken != "my-oidc-token" {
		t.Errorf("oidcToken = %v, want %q", cfg.oidcToken, "my-oidc-token")
	}
	if cfg.oidcTokenRequestURL == nil || *cfg.oidcTokenRequestURL != "https://token.example.com" {
		t.Errorf("oidcTokenRequestURL = %v, want %q", cfg.oidcTokenRequestURL, "https://token.example.com")
	}
	if cfg.oidcRequestToken == nil || *cfg.oidcRequestToken != "request-bearer" {
		t.Errorf("oidcRequestToken = %v, want %q", cfg.oidcRequestToken, "request-bearer")
	}
}

func TestParseEnv_OidcFallbackEnvVars(t *testing.T) {
	t.Setenv("ARM_CLIENT_ID", "client")
	t.Setenv("ARM_TENANT_ID", "tenant")
	t.Setenv("ACTIONS_ID_TOKEN_REQUEST_URL", "https://gh-actions.example.com")
	t.Setenv("ACTIONS_ID_TOKEN_REQUEST_TOKEN", "gh-bearer")

	cfg, err := NewCredentialConfiguration(
		WithDefaultEnvironmentVariables(),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.oidcTokenRequestURL == nil || *cfg.oidcTokenRequestURL != "https://gh-actions.example.com" {
		t.Errorf("oidcTokenRequestURL = %v, want GitHub Actions URL", cfg.oidcTokenRequestURL)
	}
	if cfg.oidcRequestToken == nil || *cfg.oidcRequestToken != "gh-bearer" {
		t.Errorf("oidcRequestToken = %v, want GitHub Actions token", cfg.oidcRequestToken)
	}
}

func TestParseEnv_AdoPipelineServiceConnection(t *testing.T) {
	t.Setenv("ARM_CLIENT_ID", "client")
	t.Setenv("ARM_TENANT_ID", "tenant")
	t.Setenv("SYSTEM_SERVICECONNECTIONID", "ado-conn-id")

	cfg, err := NewCredentialConfiguration(
		WithDefaultEnvironmentVariables(),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.adoPipelineServiceConnectionID == nil || *cfg.adoPipelineServiceConnectionID != "ado-conn-id" {
		t.Errorf("adoPipelineServiceConnectionID = %v, want %q", cfg.adoPipelineServiceConnectionID, "ado-conn-id")
	}
}

func TestParseEnv_CertificatePath(t *testing.T) {
	t.Setenv("ARM_CLIENT_ID", "client")
	t.Setenv("ARM_TENANT_ID", "tenant")
	t.Setenv("ARM_CLIENT_CERTIFICATE_PATH", "/path/to/cert.pfx")
	t.Setenv("ARM_CLIENT_CERTIFICATE_PASSWORD", "certpass")

	// This will fail at validation because the cert file doesn't exist,
	// but we can verify the env vars were parsed by checking the error path.
	_, err := NewCredentialConfiguration(
		WithDefaultEnvironmentVariables(),
	)
	if err == nil {
		t.Fatal("expected error for nonexistent certificate file")
	}
}

func TestParseEnv_EmptyEnvVarsIgnored(t *testing.T) {
	// With no env vars set, parsing should succeed but produce empty
	// client/tenant (which would fail validation)
	cfg := &CredentialConfiguration{}
	_ = cfg.parseEnv("ARM")

	if cfg.ClientId != "" {
		t.Errorf("expected empty ClientId, got %q", cfg.ClientId)
	}
}

func TestOptionComposition_MultipleOptions(t *testing.T) {
	cfg, err := NewCredentialConfiguration(
		WithClientAndTenant("client", "tenant"),
		WithClientSecret("secret"),
		WithAzCli(false),
		WithOidc(true),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ClientId != "client" {
		t.Error("ClientId not set")
	}
	if cfg.clientSecret == nil || *cfg.clientSecret != "secret" {
		t.Error("clientSecret not set")
	}
	if cfg.useAzCli == nil || *cfg.useAzCli {
		t.Error("useAzCli should be false")
	}
	if cfg.useOidc == nil || !*cfg.useOidc {
		t.Error("useOidc should be true")
	}
}

func TestOptionError_StopsChain(t *testing.T) {
	// WithClientCertificate with nil cert returns error; subsequent options
	// should not be applied
	var nilKey crypto.PrivateKey
	_, err := NewCredentialConfiguration(
		WithClientAndTenant("client", "tenant"),
		WithClientCertificate(nil, nilKey),
		WithClientSecret("should-not-reach"),
	)
	if err == nil {
		t.Fatal("expected error from nil certificate")
	}
}

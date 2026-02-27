package auth

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"math/big"
	"testing"

	"github.com/hashicorp/go-azure-sdk/sdk/environments"
)

func TestHcAzureSdk_Basic(t *testing.T) {
	cfg, err := NewCredentialConfiguration(
		WithClientAndTenant("my-client", "my-tenant"),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	creds, err := cfg.HcAzureSdk()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if creds.ClientID != "my-client" {
		t.Errorf("ClientID = %q, want %q", creds.ClientID, "my-client")
	}
	if creds.TenantID != "my-tenant" {
		t.Errorf("TenantID = %q, want %q", creds.TenantID, "my-tenant")
	}
	if creds.Environment.Authorization == nil {
		// Verify we're using AzurePublic
		expected := environments.AzurePublic()
		if creds.Environment.Name != expected.Name {
			t.Errorf("Environment = %q, want %q", creds.Environment.Name, expected.Name)
		}
	}
}

func TestHcAzureSdk_WithClientSecret(t *testing.T) {
	cfg, err := NewCredentialConfiguration(
		WithClientAndTenant("client", "tenant"),
		WithClientSecret("test-secret"),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	creds, err := cfg.HcAzureSdk()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !creds.EnableAuthenticatingUsingClientSecret {
		t.Error("expected EnableAuthenticatingUsingClientSecret to be true")
	}
	if creds.ClientSecret != "test-secret" {
		t.Errorf("ClientSecret = %q, want %q", creds.ClientSecret, "test-secret")
	}
}

func TestHcAzureSdk_WithMsi(t *testing.T) {
	cfg, err := NewCredentialConfiguration(
		WithClientAndTenant("client", "tenant"),
		WithMsi(true, nil),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	creds, err := cfg.HcAzureSdk()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !creds.EnableAuthenticatingUsingManagedIdentity {
		t.Error("expected EnableAuthenticatingUsingManagedIdentity to be true")
	}
}

func TestHcAzureSdk_WithMsiFalse(t *testing.T) {
	cfg, err := NewCredentialConfiguration(
		WithClientAndTenant("client", "tenant"),
		WithMsi(false, nil),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	creds, err := cfg.HcAzureSdk()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if creds.EnableAuthenticatingUsingManagedIdentity {
		t.Error("expected EnableAuthenticatingUsingManagedIdentity to be false")
	}
}

func TestHcAzureSdk_WithAzCli(t *testing.T) {
	cfg, err := NewCredentialConfiguration(
		WithClientAndTenant("client", "tenant"),
		WithAzCli(true),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	creds, err := cfg.HcAzureSdk()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !creds.EnableAuthenticatingUsingAzureCLI {
		t.Error("expected EnableAuthenticatingUsingAzureCLI to be true")
	}
}

func TestHcAzureSdk_WithOidc(t *testing.T) {
	cfg, err := NewCredentialConfiguration(
		WithClientAndTenant("client", "tenant"),
		WithOidc(true),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	creds, err := cfg.HcAzureSdk()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !creds.EnableAuthenticationUsingOIDC {
		t.Error("expected EnableAuthenticationUsingOIDC to be true")
	}
	if !creds.EnableAuthenticationUsingGitHubOIDC {
		t.Error("expected EnableAuthenticationUsingGitHubOIDC to be true")
	}
	if !creds.EnableAuthenticationUsingADOPipelineOIDC {
		t.Error("expected EnableAuthenticationUsingADOPipelineOIDC to be true")
	}
}

func TestHcAzureSdk_OidcWithTokenAndUrlFields(t *testing.T) {
	cfg := &CredentialConfiguration{
		ClientId: "client",
		TenantId: "tenant",
	}
	useOidc := true
	cfg.useOidc = &useOidc

	token := "assertion-token"
	cfg.oidcToken = &token

	reqURL := "https://token.example.com"
	cfg.oidcTokenRequestURL = &reqURL

	reqToken := "request-bearer"
	cfg.oidcRequestToken = &reqToken

	connID := "ado-connection-id"
	cfg.adoPipelineServiceConnectionID = &connID

	creds, err := cfg.HcAzureSdk()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if creds.OIDCAssertionToken != token {
		t.Errorf("OIDCAssertionToken = %q, want %q", creds.OIDCAssertionToken, token)
	}
	if creds.OIDCTokenRequestURL != reqURL {
		t.Errorf("OIDCTokenRequestURL = %q, want %q", creds.OIDCTokenRequestURL, reqURL)
	}
	if creds.OIDCTokenRequestToken != reqToken {
		t.Errorf("OIDCTokenRequestToken = %q, want %q", creds.OIDCTokenRequestToken, reqToken)
	}
	if creds.ADOPipelineServiceConnectionID != connID {
		t.Errorf("ADOPipelineServiceConnectionID = %q, want %q", creds.ADOPipelineServiceConnectionID, connID)
	}
}

func TestHcAzureSdk_WithCertificate(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	// Create a minimal self-signed certificate for testing
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
	}
	certDer, err := x509.CreateCertificate(rand.Reader, template, template, key.Public(), key)
	if err != nil {
		t.Fatalf("failed to create certificate: %v", err)
	}
	cert, err := x509.ParseCertificate(certDer)
	if err != nil {
		t.Fatalf("failed to parse certificate: %v", err)
	}

	cfg, err := NewCredentialConfiguration(
		WithClientAndTenant("client", "tenant"),
		WithClientCertificate(cert, key),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	creds, err := cfg.HcAzureSdk()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !creds.EnableAuthenticatingUsingClientCertificate {
		t.Error("expected EnableAuthenticatingUsingClientCertificate to be true")
	}
	if len(creds.ClientCertificateData) == 0 {
		t.Error("expected ClientCertificateData to be non-empty")
	}
	if creds.ClientCertificatePassword == "" {
		t.Error("expected ClientCertificatePassword to be non-empty")
	}
}

func TestHcAzureSdk_NoAuthMethodsEnabled(t *testing.T) {
	cfg, err := NewCredentialConfiguration(
		WithClientAndTenant("client", "tenant"),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	creds, err := cfg.HcAzureSdk()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// When no auth methods are configured, all should be false
	if creds.EnableAuthenticatingUsingClientSecret {
		t.Error("expected EnableAuthenticatingUsingClientSecret to be false")
	}
	if creds.EnableAuthenticatingUsingClientCertificate {
		t.Error("expected EnableAuthenticatingUsingClientCertificate to be false")
	}
	if creds.EnableAuthenticatingUsingManagedIdentity {
		t.Error("expected EnableAuthenticatingUsingManagedIdentity to be false")
	}
	if creds.EnableAuthenticatingUsingAzureCLI {
		t.Error("expected EnableAuthenticatingUsingAzureCLI to be false")
	}
	if creds.EnableAuthenticationUsingOIDC {
		t.Error("expected EnableAuthenticationUsingOIDC to be false")
	}
}

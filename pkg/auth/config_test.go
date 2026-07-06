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
	testCases := []struct {
		name   string
		key    string
		value  string
		wantID string
	}{
		{
			name:   "ARM_ADO_PIPELINE_SERVICE_CONNECTION_ID",
			key:    "ARM_ADO_PIPELINE_SERVICE_CONNECTION_ID",
			value:  "ado-conn-id",
			wantID: "ado-conn-id",
		},
		{
			name:   "ARM_OIDC_AZURE_SERVICE_CONNECTION_ID",
			key:    "ARM_OIDC_AZURE_SERVICE_CONNECTION_ID",
			value:  "ado-conn-id",
			wantID: "ado-conn-id",
		},
		{
			name:   "AZURESUBSCRIPTION_SERVICE_CONNECTION_ID",
			key:    "AZURESUBSCRIPTION_SERVICE_CONNECTION_ID",
			value:  "ado-conn-id",
			wantID: "ado-conn-id",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Setenv("ARM_CLIENT_ID", "client")
			t.Setenv("ARM_TENANT_ID", "tenant")
			t.Setenv(testCase.key, testCase.value)

			cfg, err := NewCredentialConfiguration(
				WithDefaultEnvironmentVariables(),
			)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cfg.adoPipelineServiceConnectionID == nil || *cfg.adoPipelineServiceConnectionID != testCase.wantID {
				t.Errorf("adoPipelineServiceConnectionID = %v, want %q", cfg.adoPipelineServiceConnectionID, testCase.wantID)
			}
		})
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

func TestWithBackendConfig_StringFields(t *testing.T) {
	cfg, err := NewCredentialConfiguration(
		WithBackendConfig(map[string]string{
			"client_id":                 "bc-client",
			"tenant_id":                 "bc-tenant",
			"client_secret":             "bc-secret",
			"oidc_token":                "bc-oidc-token",
			"oidc_request_url":          "https://bc.example.com/token",
			"oidc_request_token":        "bc-request-bearer",
			"ado_service_connection_id": "bc-ado-conn",
		}),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ClientId != "bc-client" {
		t.Errorf("ClientId = %q, want %q", cfg.ClientId, "bc-client")
	}
	if cfg.TenantId != "bc-tenant" {
		t.Errorf("TenantId = %q, want %q", cfg.TenantId, "bc-tenant")
	}
	if cfg.clientSecret == nil || *cfg.clientSecret != "bc-secret" {
		t.Errorf("clientSecret = %v, want %q", cfg.clientSecret, "bc-secret")
	}
	if cfg.oidcToken == nil || *cfg.oidcToken != "bc-oidc-token" {
		t.Errorf("oidcToken = %v, want %q", cfg.oidcToken, "bc-oidc-token")
	}
	if cfg.oidcTokenRequestURL == nil || *cfg.oidcTokenRequestURL != "https://bc.example.com/token" {
		t.Errorf("oidcTokenRequestURL = %v, want %q", cfg.oidcTokenRequestURL, "https://bc.example.com/token")
	}
	if cfg.oidcRequestToken == nil || *cfg.oidcRequestToken != "bc-request-bearer" {
		t.Errorf("oidcRequestToken = %v, want %q", cfg.oidcRequestToken, "bc-request-bearer")
	}
	if cfg.adoPipelineServiceConnectionID == nil || *cfg.adoPipelineServiceConnectionID != "bc-ado-conn" {
		t.Errorf("adoPipelineServiceConnectionID = %v, want %q", cfg.adoPipelineServiceConnectionID, "bc-ado-conn")
	}
}

func TestWithBackendConfig_CertificateFieldsWired(t *testing.T) {
	// client_certificate_path/password point to a nonexistent file, so
	// validate() fails while reading it — proving parseValues wired these
	// fields the same way parseEnv does (TestParseEnv_CertificatePath is the
	// environment-sourced equivalent of this test).
	_, err := NewCredentialConfiguration(
		WithBackendConfig(map[string]string{
			"client_id":                   "bc-client",
			"tenant_id":                   "bc-tenant",
			"client_certificate_path":     "/path/to/bc-cert.pfx",
			"client_certificate_password": "bc-cert-pass",
		}),
	)
	if err == nil {
		t.Fatal("expected error due to nonexistent certificate file")
	}
}

func TestWithBackendConfig_BoolFields(t *testing.T) {
	cfg, err := NewCredentialConfiguration(
		WithClientAndTenant("client", "tenant"),
		WithBackendConfig(map[string]string{
			"use_cli":  "true",
			"use_msi":  "false",
			"use_oidc": "true",
		}),
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
	if cfg.useOidc == nil || !*cfg.useOidc {
		t.Error("expected useOidc to be true")
	}
}

func TestWithBackendConfig_InvalidBoolReturnsError(t *testing.T) {
	testCases := []string{"use_cli", "use_msi", "use_oidc"}
	for _, key := range testCases {
		t.Run(key, func(t *testing.T) {
			_, err := NewCredentialConfiguration(
				WithClientAndTenant("client", "tenant"),
				WithBackendConfig(map[string]string{key: "not-a-bool"}),
			)
			if err == nil {
				t.Fatalf("expected error for invalid %s value", key)
			}
		})
	}
}

func TestWithBackendConfig_EmptyMapIsNoop(t *testing.T) {
	cfg, err := NewCredentialConfiguration(
		WithClientAndTenant("client", "tenant"),
		WithBackendConfig(nil),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ClientId != "client" || cfg.TenantId != "tenant" {
		t.Errorf("expected fields untouched, got ClientId=%q TenantId=%q", cfg.ClientId, cfg.TenantId)
	}
}

func TestWithBackendConfig_EmptyValuesIgnored(t *testing.T) {
	cfg, err := NewCredentialConfiguration(
		WithClientAndTenant("client", "tenant"),
		WithBackendConfig(map[string]string{
			"client_id": "",
			"tenant_id": "",
		}),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ClientId != "client" {
		t.Errorf("ClientId = %q, want unchanged %q", cfg.ClientId, "client")
	}
	if cfg.TenantId != "tenant" {
		t.Errorf("TenantId = %q, want unchanged %q", cfg.TenantId, "tenant")
	}
}

func TestWithBackendConfig_OverridesEnvironment(t *testing.T) {
	// Simulates the intended precedence: environment variables populate the
	// baseline first, then WithBackendConfig (sourced from a layer's backend
	// configuration) takes precedence on conflict — mirroring how OpenTofu
	// itself resolves azurerm backend authentication and enabling a layer's
	// state storage to authenticate against a different tenant/client than
	// the one running statebridge.
	t.Setenv("ARM_CLIENT_ID", "env-client")
	t.Setenv("ARM_TENANT_ID", "env-tenant")
	t.Setenv("ARM_USE_OIDC", "true")

	cfg, err := NewCredentialConfiguration(
		WithDefaultEnvironmentVariables(),
		WithBackendConfig(map[string]string{
			"client_id": "layer-client",
			"tenant_id": "layer-tenant",
			"use_oidc":  "false",
		}),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ClientId != "layer-client" {
		t.Errorf("ClientId = %q, want %q (backend-config should win)", cfg.ClientId, "layer-client")
	}
	if cfg.TenantId != "layer-tenant" {
		t.Errorf("TenantId = %q, want %q (backend-config should win)", cfg.TenantId, "layer-tenant")
	}
	if cfg.useOidc == nil || *cfg.useOidc {
		t.Error("expected useOidc to be false (backend-config should win over env)")
	}
}

func TestWithBackendConfig_PartialOverrideKeepsEnvValues(t *testing.T) {
	// Only tenant_id is overridden; client_id and the client secret sourced
	// from the environment must survive the merge untouched.
	t.Setenv("ARM_CLIENT_ID", "env-client")
	t.Setenv("ARM_TENANT_ID", "env-tenant")
	t.Setenv("ARM_CLIENT_SECRET", "env-secret")

	cfg, err := NewCredentialConfiguration(
		WithDefaultEnvironmentVariables(),
		WithBackendConfig(map[string]string{
			"tenant_id": "other-tenant",
		}),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ClientId != "env-client" {
		t.Errorf("ClientId = %q, want unchanged %q", cfg.ClientId, "env-client")
	}
	if cfg.TenantId != "other-tenant" {
		t.Errorf("TenantId = %q, want %q", cfg.TenantId, "other-tenant")
	}
	if cfg.clientSecret == nil || *cfg.clientSecret != "env-secret" {
		t.Errorf("clientSecret = %v, want unchanged %q", cfg.clientSecret, "env-secret")
	}
}

func TestWithBackendConfig_UnrecognizedKeysIgnored(t *testing.T) {
	cfg, err := NewCredentialConfiguration(
		WithClientAndTenant("client", "tenant"),
		WithBackendConfig(map[string]string{
			"subscription_id":      "sub-id",
			"storage_account_name": "not-a-credential-field",
		}),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ClientId != "client" || cfg.TenantId != "tenant" {
		t.Errorf("expected fields untouched, got ClientId=%q TenantId=%q", cfg.ClientId, cfg.TenantId)
	}
}

package auth

import (
	"crypto"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/terraprovider/statebridge/pkg/util"
	"golang.org/x/crypto/pkcs12"
)

// CredentialKeys lists the configuration attribute names — matching the
// names used by OpenTofu/Terraform's azurerm backend — that WithBackendConfig
// recognizes as sources of credential values. Any other key in the map
// passed to WithBackendConfig is ignored.
var CredentialKeys = []string{
	"client_id",
	"tenant_id",
	"client_secret",
	"client_certificate_path",
	"client_certificate_password",
	"use_cli",
	"use_msi",
	"use_oidc",
	"oidc_token",
	"oidc_request_url",
	"oidc_request_token",
	"ado_service_connection_id",
}

// CredentialConfiguration holds the configuration for Entra authentication.
// It supports multiple authentication methods including client secrets,
// certificates, Azure CLI, and Managed Service Identity (MSI).
type CredentialConfiguration struct {
	// ClientId is the Entra ID application (client) ID
	ClientId string
	// TenantId is the Entra ID tenant (directory) ID
	TenantId string

	// clientSecret holds the client secret for service principal authentication
	clientSecret *string

	// clientCertificatePath is the path to a PFX/PKCS12 certificate file
	clientCertificatePath *string
	// clientCertificatePassword is the password for the certificate file
	clientCertificatePassword *string
	// clientCertificate is the parsed X.509 certificate
	clientCertificate *x509.Certificate
	// clientCertificatePrivateKey is the private key extracted from the certificate
	clientCertificatePrivateKey crypto.PrivateKey

	// useAzCli enables Azure CLI credential authentication
	useAzCli *bool

	// useMsi enables Managed Service Identity authentication
	useMsi *bool
	// msiClientId is the client ID for user-assigned managed identity (optional)
	msiClientId *string

	// useOidc enables OpenID Connect authentication
	useOidc *bool
	// oidcToken is a direct OIDC assertion token
	oidcToken *string
	// oidcRequestToken is an auth token for requesting OIDC tokens from a URL
	oidcRequestToken *string
	// oidcTokenRequestURL is the URL to request OIDC tokens from (GitHub Actions / ADO)
	oidcTokenRequestURL *string
	// adoPipelineServiceConnectionID is the service connection ID for ADO Pipeline OIDC
	adoPipelineServiceConnectionID *string
}

// Option is a functional option for configuring CredentialConfiguration
type Option func(*CredentialConfiguration) error

// NewCredentialConfiguration creates a new CredentialConfiguration by applying
// the provided options in order. It validates the configuration before returning.
func NewCredentialConfiguration(options ...Option) (*CredentialConfiguration, error) {
	c := &CredentialConfiguration{}
	for _, option := range options {
		if err := option(c); err != nil {
			return nil, err
		}
	}
	return c.validate()
}

// validate checks that required fields are set and processes certificate files
// if a certificate path and password are provided.
func (c *CredentialConfiguration) validate() (*CredentialConfiguration, error) {
	if c.ClientId == "" {
		return nil, errors.New("client id is empty")
	}
	if c.TenantId == "" {
		return nil, errors.New("tenant id is empty")
	}

	// If certificate path and password are provided, load and parse the PFX file
	if c.clientCertificatePath != nil && c.clientCertificatePassword != nil {
		if _, err := os.Stat(*c.clientCertificatePath); err != nil {
			return nil, err
		}
		pfxData, err := os.ReadFile(*c.clientCertificatePath)
		if err != nil {
			return nil, err
		}

		// Decode PKCS12/PFX data to extract private key and certificate
		key, cert, err := pkcs12.Decode(pfxData, *c.clientCertificatePassword)
		if err != nil {
			return nil, err
		}
		c.clientCertificate = cert

		if key != nil {
			c.clientCertificatePrivateKey = key
		} else {
			return nil, errors.New("private key is nil")
		}
	}
	return c, nil
}

// WithDefaultEnvironmentVariables configures the credential from environment
// variables with the "ARM_" prefix (e.g., ARM_CLIENT_ID, ARM_TENANT_ID).
func WithDefaultEnvironmentVariables() Option {
	return func(c *CredentialConfiguration) error {
		return c.parseEnv("ARM")
	}
}

// WithEnvironmentVariablePrefixes configures the credential from environment
// variables with custom prefixes. Multiple prefixes can be provided and are
// processed in order.
func WithEnvironmentVariablePrefixes(prefixes ...string) Option {
	return func(c *CredentialConfiguration) error {
		return c.parseEnv(prefixes...)
	}
}

// parseEnv reads environment variables with the given prefixes and populates
// the configuration fields.
func (c *CredentialConfiguration) parseEnv(prefixes ...string) error {
	for _, prefix := range prefixes {
		// Normalize prefix by removing trailing underscore if present
		prefix = strings.TrimSuffix(prefix, "_")

		if v := util.Getenv[string](prefix + "_CLIENT_ID"); v != nil && *v != "" {
			c.ClientId = *v
		}
		if v := util.Getenv[string](prefix + "_TENANT_ID"); v != nil && *v != "" {
			c.TenantId = *v
		}
		if v := util.Getenv[string](prefix + "_CLIENT_SECRET"); v != nil && *v != "" {
			c.clientSecret = v
		}
		if v := util.Getenv[string](prefix + "_CLIENT_CERTIFICATE_PATH"); v != nil && *v != "" {
			c.clientCertificatePath = v
		}
		if v := util.Getenv[string](prefix + "_CLIENT_CERTIFICATE_PASSWORD"); v != nil && *v != "" {
			c.clientCertificatePassword = v
		}
		if v := util.Getenv[bool](prefix + "_USE_CLI"); v != nil {
			c.useAzCli = v
		}
		if v := util.Getenv[bool](prefix + "_USE_MSI"); v != nil {
			c.useMsi = v
		}
		if v := util.GetMultienv[string]("ARM_OIDC_TOKEN"); v != nil && *v != "" {
			c.oidcToken = v
		}
		if v := util.Getenv[bool]("ARM_USE_OIDC"); v != nil {
			c.useOidc = v
		}
		if v := util.GetMultienv[string]("ARM_OIDC_REQUEST_URL", "ACTIONS_ID_TOKEN_REQUEST_URL", "SYSTEM_OIDCREQUESTURI"); v != nil && *v != "" {
			c.oidcTokenRequestURL = v
		}
		if v := util.GetMultienv[string]("ARM_OIDC_REQUEST_TOKEN", "ACTIONS_ID_TOKEN_REQUEST_TOKEN", "SYSTEM_OIDCREQUESTTOKEN"); v != nil && *v != "" {
			c.oidcRequestToken = v
		}
		if v := util.GetMultienv[string]("ARM_ADO_PIPELINE_SERVICE_CONNECTION_ID", "ARM_OIDC_AZURE_SERVICE_CONNECTION_ID", "AZURESUBSCRIPTION_SERVICE_CONNECTION_ID"); v != nil && *v != "" {
			c.adoPipelineServiceConnectionID = v
		}
	}
	return nil
}

// WithBackendConfig configures credential fields from a plain string map
// keyed by the same attribute names used in OpenTofu/Terraform's azurerm
// backend block (see CredentialKeys) — e.g. "client_id", "tenant_id",
// "use_oidc". Unrecognized keys are ignored.
//
// This lets values discovered from a layer's backend configuration (inline
// HCL "backend \"azurerm\"" block, or -backend-config CLI flags/files) be
// merged into the credential configuration as an additional source, on top
// of whatever was already populated by an earlier option such as
// WithDefaultEnvironmentVariables. When both a source populated via
// WithDefaultEnvironmentVariables and this option set the same field, the
// value from this option wins, since options are applied in call order and
// each option simply overwrites any value set by an earlier one. Applying
// WithDefaultEnvironmentVariables first and WithBackendConfig second
// therefore reproduces the same precedence OpenTofu itself uses when
// resolving azurerm backend authentication: environment variables provide
// the baseline, and explicit backend configuration takes precedence.
func WithBackendConfig(values map[string]string) Option {
	return func(c *CredentialConfiguration) error {
		return c.parseValues(values)
	}
}

// parseValues populates credential fields from a plain key/value map using
// the attribute names listed in CredentialKeys. Unlike parseEnv (which
// silently warns and skips on an unparsable environment variable, since
// ambient env vars may be unrelated to the current invocation), an invalid
// boolean value here returns an error: these values are explicitly authored
// for this layer, so a typo should be surfaced rather than silently ignored.
func (c *CredentialConfiguration) parseValues(values map[string]string) error {
	if v, ok := values["client_id"]; ok && v != "" {
		c.ClientId = v
	}
	if v, ok := values["tenant_id"]; ok && v != "" {
		c.TenantId = v
	}
	if v, ok := values["client_secret"]; ok && v != "" {
		c.clientSecret = &v
	}
	if v, ok := values["client_certificate_path"]; ok && v != "" {
		c.clientCertificatePath = &v
	}
	if v, ok := values["client_certificate_password"]; ok && v != "" {
		c.clientCertificatePassword = &v
	}
	if v, ok := values["use_cli"]; ok && v != "" {
		parsed, err := util.Parse[bool](v)
		if err != nil {
			return fmt.Errorf("invalid value for use_cli: %w", err)
		}
		c.useAzCli = &parsed
	}
	if v, ok := values["use_msi"]; ok && v != "" {
		parsed, err := util.Parse[bool](v)
		if err != nil {
			return fmt.Errorf("invalid value for use_msi: %w", err)
		}
		c.useMsi = &parsed
	}
	if v, ok := values["use_oidc"]; ok && v != "" {
		parsed, err := util.Parse[bool](v)
		if err != nil {
			return fmt.Errorf("invalid value for use_oidc: %w", err)
		}
		c.useOidc = &parsed
	}
	if v, ok := values["oidc_token"]; ok && v != "" {
		c.oidcToken = &v
	}
	if v, ok := values["oidc_request_url"]; ok && v != "" {
		c.oidcTokenRequestURL = &v
	}
	if v, ok := values["oidc_request_token"]; ok && v != "" {
		c.oidcRequestToken = &v
	}
	if v, ok := values["ado_service_connection_id"]; ok && v != "" {
		c.adoPipelineServiceConnectionID = &v
	}
	return nil
}

func WithClientAndTenant(clientId, tenantId string) Option {
	return func(c *CredentialConfiguration) error {
		c.ClientId = clientId
		c.TenantId = tenantId
		return nil
	}
}

// WithClientSecret configures client secret authentication for a service principal.
func WithClientSecret(secret string) Option {
	return func(c *CredentialConfiguration) error {
		c.clientSecret = &secret
		return nil
	}
}

// WithClientCertificateFromFile configures certificate authentication using a
// PFX/PKCS12 file. The certificate is loaded and parsed during validation.
func WithClientCertificateFromFile(path, password string) Option {
	return func(c *CredentialConfiguration) error {
		c.clientCertificatePath = &path
		c.clientCertificatePassword = &password
		return nil
	}
}

// WithClientCertificate configures certificate authentication using a pre-loaded
// X.509 certificate and private key.
func WithClientCertificate(cert *x509.Certificate, key crypto.PrivateKey) Option {
	return func(c *CredentialConfiguration) error {
		if cert == nil {
			return errors.New("certificate is nil")
		}
		if key == nil {
			return errors.New("key is nil")
		}
		c.clientCertificate = cert
		c.clientCertificatePrivateKey = key
		return nil
	}
}

// WithAzCli enables or disables Azure CLI credential authentication.
// This is useful for local development when the user is logged in via `az login`.
func WithAzCli(useAzCli bool) Option {
	return func(c *CredentialConfiguration) error {
		c.useAzCli = &useAzCli
		return nil
	}
}

// WithMsi enables or disables Managed Service Identity authentication.
// For user-assigned managed identities, provide the msiClientId.
// For system-assigned managed identities, pass nil for msiClientId.
func WithMsi(useMsi bool, msiClientId *string) Option {
	return func(c *CredentialConfiguration) error {
		c.useMsi = &useMsi
		c.msiClientId = msiClientId
		return nil
	}
}

// WithOidc enables or disables OpenID Connect authentication.
func WithOidc(useOidc bool) Option {
	return func(c *CredentialConfiguration) error {
		c.useOidc = &useOidc
		return nil
	}
}

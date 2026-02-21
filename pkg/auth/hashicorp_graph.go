package auth

import (
	"crypto/rand"

	"github.com/hashicorp/go-azure-sdk/sdk/auth"
	"github.com/hashicorp/go-azure-sdk/sdk/environments"
	"software.sslmate.com/src/go-pkcs12"
)

// HcAzureSdk construct a credential for the hashicorp Azure SDK
func (c *CredentialConfiguration) HcAzureSdk() (auth.Credentials, error) {
	creds := auth.Credentials{
		Environment: *environments.AzurePublic(),
		ClientID:    c.ClientId,
		TenantID:    c.TenantId,
	}

	if c.useMsi != nil && *c.useMsi {
		creds.EnableAuthenticatingUsingManagedIdentity = true
	}

	if c.clientSecret != nil {
		creds.EnableAuthenticatingUsingClientSecret = true
		creds.ClientSecret = *c.clientSecret
	}

	if c.clientCertificate != nil && c.clientCertificatePrivateKey != nil {
		password := rand.Text()
		pfxData, err := pkcs12.Modern.Encode(c.clientCertificatePrivateKey, c.clientCertificate, nil, password)
		if err != nil {
			return auth.Credentials{}, err
		}

		creds.EnableAuthenticatingUsingClientCertificate = true
		creds.ClientCertificateData = pfxData
		creds.ClientCertificatePassword = password
	}

	if c.useAzCli != nil && *c.useAzCli {
		creds.EnableAuthenticatingUsingAzureCLI = true
	}

	if c.useOidc != nil && *c.useOidc {
		creds.EnableAuthenticationUsingOIDC = true
		creds.EnableAuthenticationUsingADOPipelineOIDC = true
		creds.EnableAuthenticationUsingGitHubOIDC = true
		if c.oidcToken != nil {
			creds.OIDCAssertionToken = *c.oidcToken
		}
		if c.oidcTokenRequestURL != nil {
			creds.OIDCTokenRequestURL = *c.oidcTokenRequestURL
		}
		if c.oidcRequestToken != nil {
			creds.OIDCTokenRequestToken = *c.oidcRequestToken
		}
		if c.adoPipelineServiceConnectionID != nil {
			creds.ADOPipelineServiceConnectionID = *c.adoPipelineServiceConnectionID
		}
	}

	return creds, nil
}

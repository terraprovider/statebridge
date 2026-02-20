package auth

import (
	"context"
	"crypto/x509"
	"errors"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
)

// AzCore constructs a chained Azure token credential based on the configuration.
// It attempts to add credentials in the following order of priority:
// 1. Managed Service Identity (MSI) - wrapped with a timeout to handle unavailability
// 2. Client Secret credential
// 3. Client Certificate credential
// 4. Azure CLI credential
//
// Returns a ChainedTokenCredential that will try each configured credential in order
// until one succeeds.
func (c *CredentialConfiguration) AzCore() (azcore.TokenCredential, error) {
	var chain []azcore.TokenCredential

	// Add Managed Identity credential if enabled
	if c.useMsi != nil && *c.useMsi {
		if c.msiClientId != nil {
			// User-assigned managed identity with explicit client ID
			msiClientId := azidentity.ClientID(*c.msiClientId)
			if cred, err := azidentity.NewManagedIdentityCredential(&azidentity.ManagedIdentityCredentialOptions{ID: msiClientId}); err == nil {
				// Wrap with timeout to prevent hanging when MSI is unavailable
				chain = append(chain, &timeoutWrapper{cred: cred, timeout: time.Second})
			}
		} else {
			// System-assigned managed identity
			if cred, err := azidentity.NewManagedIdentityCredential(nil); err == nil {
				chain = append(chain, &timeoutWrapper{cred: cred, timeout: time.Second})
			}
		}
	}

	// Add Client Secret credential if configured
	if c.clientSecret != nil {
		if cred, err := azidentity.NewClientSecretCredential(c.TenantId, c.ClientId, *c.clientSecret, nil); err == nil {
			chain = append(chain, cred)
		}
	}

	// Add Client Certificate credential if both certificate and private key are configured
	if c.clientCertificate != nil && c.clientCertificatePrivateKey != nil {
		if cred, err := azidentity.NewClientCertificateCredential(c.TenantId, c.ClientId, []*x509.Certificate{c.clientCertificate}, c.clientCertificatePrivateKey, nil); err == nil {
			chain = append(chain, cred)
		}
	}

	// Add Azure CLI credential if enabled (useful for local development)
	if c.useAzCli != nil && *c.useAzCli {
		if cred, err := azidentity.NewAzureCLICredential(&azidentity.AzureCLICredentialOptions{
			TenantID: c.TenantId,
		}); err == nil {
			chain = append(chain, cred)
		}
	}

	if len(chain) == 0 {
		return nil, errors.New("no credentials configured")
	}

	// Create chained credential that tries each credential in order
	return azidentity.NewChainedTokenCredential(chain, nil)
}

// timeoutWrapper wraps a ManagedIdentityCredential with a timeout mechanism.
// This signals ChainedTokenCredential to try another credential when managed identity
// is unavailable or times out (common in local development environments).
type timeoutWrapper struct {
	cred    *azidentity.ManagedIdentityCredential
	timeout time.Duration
}

// GetToken implements the azcore.TokenCredential interface.
// It adds timeout handling to detect when managed identity is unavailable,
// allowing the credential chain to fall through to the next credential.
func (w *timeoutWrapper) GetToken(ctx context.Context, opts policy.TokenRequestOptions) (azcore.AccessToken, error) {
	var tk azcore.AccessToken
	var err error

	if w.timeout > 0 {
		// Apply timeout for the first token request to detect MSI availability
		c, cancel := context.WithTimeout(ctx, w.timeout)
		defer cancel()

		tk, err = w.cred.GetToken(c, opts)

		if ce := c.Err(); errors.Is(ce, context.DeadlineExceeded) {
			// Timeout reached - likely no managed identity endpoint available.
			// Return CredentialUnavailableError to signal the chain to try the next credential.
			err = azidentity.NewCredentialUnavailableError("managed identity timed out")
		} else {
			// MSI responded (success or auth error) - disable timeout for future calls
			// since we know the endpoint is available
			w.timeout = 0
		}
	} else {
		// Timeout disabled - MSI endpoint is known to be available
		tk, err = w.cred.GetToken(ctx, opts)
	}

	return tk, err
}

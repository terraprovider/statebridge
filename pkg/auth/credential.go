package auth

import (
	"context"
	"net/http"
	"strings"
	"sync"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/hashicorp/go-azure-sdk/sdk/auth"
	"github.com/hashicorp/go-azure-sdk/sdk/environments"
)

// hashicorpCredential wraps HashiCorp go-azure-sdk auth to implement azcore.TokenCredential.
// It lazily creates and caches authorizers per requested scope set.
type hashicorpCredential struct {
	creds           auth.Credentials
	mu              sync.Mutex
	authorizerCache map[string]auth.Authorizer
}

// TokenCredential creates an azcore.TokenCredential backed by the HashiCorp
// go-azure-sdk auth library. This provides transparent support for all
// authentication methods including OIDC (GitHub Actions, ADO Pipeline, generic).
func (c *CredentialConfiguration) TokenCredential() (azcore.TokenCredential, error) {
	creds, err := c.HcAzureSdk()
	if err != nil {
		return nil, err
	}
	return &hashicorpCredential{
		creds:           creds,
		authorizerCache: make(map[string]auth.Authorizer),
	}, nil
}

// GetToken implements azcore.TokenCredential by delegating to the HashiCorp SDK
// authorizer. The requested scopes are used to derive the target API, and
// authorizers are cached per scope set for efficiency.
func (h *hashicorpCredential) GetToken(ctx context.Context, opts policy.TokenRequestOptions) (azcore.AccessToken, error) {
	scopeKey := strings.Join(opts.Scopes, " ")

	h.mu.Lock()
	authorizer, ok := h.authorizerCache[scopeKey]
	if !ok {
		api := apiFromScopes(opts.Scopes)
		var err error
		authorizer, err = auth.NewAuthorizerFromCredentials(ctx, h.creds, api)
		if err != nil {
			h.mu.Unlock()
			return azcore.AccessToken{}, err
		}
		h.authorizerCache[scopeKey] = authorizer
	}
	h.mu.Unlock()

	token, err := authorizer.Token(ctx, &http.Request{})
	if err != nil {
		return azcore.AccessToken{}, err
	}
	return azcore.AccessToken{
		Token:     token.AccessToken,
		ExpiresOn: token.Expiry,
	}, nil
}

// apiFromScopes derives an environments.Api from Azure SDK token request scopes.
// Azure scopes follow the pattern "https://resource.azure.com/.default";
// we strip the "/.default" suffix to obtain the resource identifier used
// by the HashiCorp SDK to construct its token request.
func apiFromScopes(scopes []string) environments.Api {
	if len(scopes) == 0 {
		return environments.NewApiEndpoint("", "", nil)
	}
	resource := strings.TrimSuffix(scopes[0], "/.default")
	return environments.NewApiEndpoint("", resource, nil)
}

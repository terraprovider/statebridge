package upload

import (
	"fmt"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"

	"github.com/terraprovider/statebridge/pkg/auth"
)

// ResolveCredential returns the Azure credential to use for a specific
// layer's blob storage operations. When the layer's backend configuration
// supplies no credential values (config is nil or config.Credentials is
// empty), base is returned unchanged.
//
// Otherwise, a new credential is built by starting from the process's
// environment-sourced configuration (ARM_* variables) and merging the
// layer's backend-config-sourced values on top: environment variables are
// read first, and any credential value present in config.Credentials takes
// precedence on conflict. This mirrors how OpenTofu itself resolves azurerm
// backend authentication, and allows a layer's state storage to live in a
// different tenant/service principal than the one running statebridge.
func ResolveCredential(base azcore.TokenCredential, config *BackendConfig) (azcore.TokenCredential, error) {
	if config == nil || len(config.Credentials) == 0 {
		return base, nil
	}

	credCfg, err := auth.NewCredentialConfiguration(
		auth.WithDefaultEnvironmentVariables(),
		auth.WithBackendConfig(config.Credentials),
	)
	if err != nil {
		return nil, fmt.Errorf("configuring layer credentials: %w", err)
	}

	cred, err := credCfg.TokenCredential()
	if err != nil {
		return nil, fmt.Errorf("creating layer credential: %w", err)
	}
	return cred, nil
}

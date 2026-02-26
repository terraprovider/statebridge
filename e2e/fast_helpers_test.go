//go:build e2e_fast

package e2e

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	tfjson "github.com/hashicorp/terraform-json"
)

var (
	providerCacheDir  string
	providerCacheOnce sync.Once
)

// ensureProviderCache creates a shared provider plugin cache directory and sets
// TF_PLUGIN_CACHE_DIR so that all tofu init calls reuse cached providers instead
// of downloading them from the registry for every parallel test.
func ensureProviderCache(t *testing.T) {
	t.Helper()
	providerCacheOnce.Do(func() {
		dir, err := os.MkdirTemp("", "tfmigrate-e2e-fast-plugin-cache-*")
		if err != nil {
			t.Fatalf("creating provider cache dir: %v", err)
		}
		providerCacheDir = dir
		os.Setenv("TF_PLUGIN_CACHE_DIR", dir)
		t.Logf("provider cache dir: %s", dir)
	})
}

// setupFastProject copies the fastproject to a temp directory and ensures
// the shared provider cache is initialised.
func setupFastProject(t *testing.T) (rootDir string, prefix string, vars map[string]string) {
	t.Helper()

	ensureProviderCache(t)

	prefix = uniquePrefix(t)
	rootDir = t.TempDir()

	projectSrc := filepath.Join(testdataDir(), "fastproject")
	copyDir(t, projectSrc, filepath.Join(rootDir, "fastproject"))
	rootDir = filepath.Join(rootDir, "fastproject")

	vars = map[string]string{
		"prefix": prefix,
	}

	return rootDir, prefix, vars
}

// resourceAttribute returns a stringified attribute value for a resource address.
func resourceAttribute(t *testing.T, layerDir, addr, key string) string {
	t.Helper()
	tf := newTerraform(t, layerDir)
	ctx := context.Background()

	st, err := tf.Show(ctx)
	if err != nil {
		t.Fatalf("tofu show in %s: %v", layerDir, err)
	}
	if st.Values == nil || st.Values.RootModule == nil {
		t.Fatalf("missing state values in %s", layerDir)
	}

	value, ok := findResourceAttribute(st.Values.RootModule, addr, key)
	if !ok {
		t.Fatalf("attribute %q not found for %q in %s", key, addr, layerDir)
	}
	return value
}

func findResourceAttribute(mod *tfjson.StateModule, addr, key string) (string, bool) {
	for _, r := range mod.Resources {
		if r.Address != addr {
			continue
		}
		val, ok := r.AttributeValues[key]
		if !ok {
			return "", false
		}
		return fmt.Sprint(val), true
	}
	for _, child := range mod.ChildModules {
		if value, ok := findResourceAttribute(child, addr, key); ok {
			return value, true
		}
	}
	return "", false
}

// storageInitArgs returns init arguments that provide backend-config for
// DiscoverBackendConfig. Since fastproject layers use local backend (no
// azurerm backend block), these init args are the sole source of storage
// account and container name.
func storageInitArgs(storageAccountName, containerName string) []string {
	return []string{
		"-backend-config=storage_account_name=" + storageAccountName,
		"-backend-config=container_name=" + containerName,
	}
}

// ---------------------------------------------------------------------------
// Storage test environment
// ---------------------------------------------------------------------------

// storageTestEnv holds common state for storage-related E2E tests.
type storageTestEnv struct {
	rootDir, sharedDir, appDir string
	prefix                     string
	vars                       map[string]string
	ctx                        context.Context
	cred                       azcore.TokenCredential
	storageAccount             string
	containerName              string
	initArgs                   []string
}

// setupStorageTest creates a fast project, blob container, and returns the
// common test environment shared by all storage tests.
func setupStorageTest(t *testing.T) *storageTestEnv {
	t.Helper()
	storageAccount := requireEnv(t, "E2E_STORAGE_ACCOUNT_NAME")
	ctx := context.Background()
	cred := getCredential(t)

	rootDir, prefix, vars := setupFastProject(t)
	containerName := prefix
	createContainer(t, ctx, cred, storageAccount, containerName)
	t.Cleanup(func() { deleteContainer(t, ctx, cred, storageAccount, containerName) })

	return &storageTestEnv{
		rootDir:        rootDir,
		sharedDir:      filepath.Join(rootDir, "layers", "shared"),
		appDir:         filepath.Join(rootDir, "layers", "app"),
		prefix:         prefix,
		vars:           vars,
		ctx:            ctx,
		cred:           cred,
		storageAccount: storageAccount,
		containerName:  containerName,
		initArgs:       storageInitArgs(storageAccount, containerName),
	}
}

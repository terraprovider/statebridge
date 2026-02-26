//go:build e2e_fast

package e2e

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/hashicorp/terraform-exec/tfexec"
	tfjson "github.com/hashicorp/terraform-json"

	"github.com/redtenant/tfmigrate/pkg/auth"
	"github.com/redtenant/tfmigrate/pkg/engine"
	"github.com/redtenant/tfmigrate/pkg/state"
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

// uniquePrefix returns a short, globally-unique, lowercase prefix suitable for local resources.
func uniquePrefix(t *testing.T) string {
	t.Helper()
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("generating random prefix: %v", err)
	}
	return "tfe2e" + hex.EncodeToString(b)[:4]
}

// copyDir recursively copies the directory tree at src to dst.
func copyDir(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)

		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}

		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()

		out, err := os.Create(target)
		if err != nil {
			return err
		}
		defer out.Close()

		_, err = io.Copy(out, in)
		return err
	})
	if err != nil {
		t.Fatalf("copying %s to %s: %v", src, dst, err)
	}
}

// tofuExecPath returns the path to the tofu binary, failing the test if not found.
func tofuExecPath(t *testing.T) string {
	t.Helper()
	p, err := exec.LookPath("tofu")
	if err != nil {
		t.Fatalf("tofu binary not found in PATH: %v", err)
	}
	return p
}

// newTerraform creates a tfexec.Terraform instance for the given working directory.
func newTerraform(t *testing.T, workDir string) *tfexec.Terraform {
	t.Helper()
	tf, err := tfexec.NewTerraform(workDir, tofuExecPath(t))
	if err != nil {
		t.Fatalf("initializing terraform-exec in %s: %v", workDir, err)
	}
	tf.SetStdout(os.Stderr)
	tf.SetStderr(os.Stderr)
	return tf
}

// varOpts converts a map of variable key-value pairs to tfexec options.
func varOpts(vars map[string]string) []string {
	keys := make([]string, 0, len(vars))
	for k := range vars {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	opts := make([]string, 0, len(vars))
	for _, k := range keys {
		opts = append(opts, k+"="+vars[k])
	}
	return opts
}

// tofuInit runs tofu init in the given directory.
func tofuInit(t *testing.T, workDir string) {
	t.Helper()
	tf := newTerraform(t, workDir)
	ctx := context.Background()
	if err := tf.Init(ctx); err != nil {
		t.Fatalf("tofu init in %s: %v", workDir, err)
	}
}

// tofuApply runs tofu apply in the given directory with the provided variables.
func tofuApply(t *testing.T, workDir string, vars map[string]string) {
	t.Helper()
	tf := newTerraform(t, workDir)
	ctx := context.Background()

	var opts []tfexec.ApplyOption
	for _, v := range varOpts(vars) {
		opts = append(opts, tfexec.Var(v))
	}

	if err := tf.Apply(ctx, opts...); err != nil {
		t.Fatalf("tofu apply in %s: %v", workDir, err)
	}
}

// tofuPlan runs tofu plan in the given directory and returns whether changes were detected.
func tofuPlan(t *testing.T, workDir string, vars map[string]string) bool {
	t.Helper()
	tf := newTerraform(t, workDir)
	ctx := context.Background()

	var opts []tfexec.PlanOption
	for _, v := range varOpts(vars) {
		opts = append(opts, tfexec.Var(v))
	}

	hasChanges, err := tf.Plan(ctx, opts...)
	if err != nil {
		t.Fatalf("tofu plan in %s: %v", workDir, err)
	}
	return hasChanges
}

// tofuDestroy runs tofu destroy in the given directory with the provided variables.
// It re-runs tofu init first to ensure the lock file and provider cache are
// consistent (they can become stale when tests rewrite .tf files between init
// and cleanup). Errors are logged but do not fail the test (cleanup best-effort).
func tofuDestroy(t *testing.T, workDir string, vars map[string]string) {
	t.Helper()
	tf := newTerraform(t, workDir)
	ctx := context.Background()

	// Re-init to refresh the lock file before destroying.
	if err := tf.Init(ctx); err != nil {
		t.Logf("WARNING: tofu init in %s before destroy failed: %v", workDir, err)
	}

	var opts []tfexec.DestroyOption
	for _, v := range varOpts(vars) {
		opts = append(opts, tfexec.Var(v))
	}

	if err := tf.Destroy(ctx, opts...); err != nil {
		t.Logf("WARNING: tofu destroy in %s failed: %v", workDir, err)
	}
}

// tofuStateList returns the list of resource addresses in the state.
func tofuStateList(t *testing.T, workDir string) []string {
	t.Helper()
	tf := newTerraform(t, workDir)
	ctx := context.Background()

	st, err := tf.Show(ctx)
	if err != nil {
		t.Fatalf("tofu show in %s: %v", workDir, err)
	}

	var addrs []string
	if st.Values != nil && st.Values.RootModule != nil {
		addrs = collectResources(st.Values.RootModule)
	}
	return addrs
}

// collectResources recursively extracts resource addresses from a state module.
func collectResources(mod *tfjson.StateModule) []string {
	var addrs []string
	for _, r := range mod.Resources {
		addrs = append(addrs, r.Address)
	}
	for _, child := range mod.ChildModules {
		addrs = append(addrs, collectResources(child)...)
	}
	return addrs
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

// writeMigration creates a migration YAML file in <dir>/migrations/<name>.
func writeMigration(t *testing.T, dir, name, content string) string {
	t.Helper()
	migDir := filepath.Join(dir, "migrations")
	if err := os.MkdirAll(migDir, 0o755); err != nil {
		t.Fatalf("creating migrations dir: %v", err)
	}
	path := filepath.Join(migDir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing migration %s: %v", name, err)
	}
	return migDir
}

// runGenerate runs the tfmigrate engine to process migration files and generate HCL.
func runGenerate(t *testing.T, migrationPaths []string) []string {
	t.Helper()
	result := runGenerateResult(t, migrationPaths)
	return result.OutputFiles
}

// runGenerateResult runs the engine and returns the full ProcessResult,
// including SkippedFiles for condition/retired/error tests.
func runGenerateResult(t *testing.T, migrationPaths []string) *engine.ProcessResult {
	t.Helper()

	reader, err := state.NewTofuStateReaderFromPath(nil)
	if err != nil {
		t.Fatalf("creating state reader: %v", err)
	}

	cfg := engine.Config{
		StateReader: reader,
		DryRun:      false,
	}
	eng := engine.New(cfg)
	ctx := context.Background()

	result, err := eng.ProcessFiles(ctx, migrationPaths)
	if err != nil {
		t.Fatalf("processing migration files: %v", err)
	}
	return result
}

// updateTfFile overwrites a .tf file in the given directory.
func updateTfFile(t *testing.T, dir, filename, content string) {
	t.Helper()
	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}

// containsResource checks if the given address is in the resource list.
func containsResource(resources []string, addr string) bool {
	for _, r := range resources {
		if r == addr {
			return true
		}
	}
	return false
}

// assertResourceInState asserts the given address exists in the state of the layer.
func assertResourceInState(t *testing.T, layerDir, addr string) {
	t.Helper()
	resources := tofuStateList(t, layerDir)
	if !containsResource(resources, addr) {
		t.Errorf("expected %q in state of %s, got: %v", addr, layerDir, resources)
	}
}

// assertResourceNotInState asserts the given address does NOT exist in the state.
func assertResourceNotInState(t *testing.T, layerDir, addr string) {
	t.Helper()
	resources := tofuStateList(t, layerDir)
	if containsResource(resources, addr) {
		t.Errorf("expected %q NOT in state of %s, got: %v", addr, layerDir, resources)
	}
}

// assertCleanPlan asserts that tofu plan shows no changes.
func assertCleanPlan(t *testing.T, layerDir string, vars map[string]string) {
	t.Helper()
	if hasChanges := tofuPlan(t, layerDir, vars); hasChanges {
		t.Errorf("expected clean plan in %s, but changes were detected", layerDir)
	}
}

// assertFileContains asserts that the file at path contains the provided substring.
func assertFileContains(t *testing.T, path, substr string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	if !strings.Contains(string(data), substr) {
		t.Errorf("expected %s to contain %q", path, substr)
	}
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

// testdataDir returns the absolute path to the e2e directory containing fastproject.
func testdataDir() string {
	dir, _ := filepath.Abs(".")
	return dir
}

// cleanupMigrationFiles removes all migration.*.tf files from a layer directory.
func cleanupMigrationFiles(t *testing.T, layerDir string) {
	t.Helper()
	matches, _ := filepath.Glob(filepath.Join(layerDir, "migration.*.tf"))
	for _, m := range matches {
		if err := os.Remove(m); err != nil {
			t.Logf("WARNING: removing %s: %v", m, err)
		}
	}
}

// ---------------------------------------------------------------------------
// Azure Blob Storage helpers (gated by E2E_STORAGE_ACCOUNT_NAME)
// ---------------------------------------------------------------------------

// requireEnv returns the value of the named environment variable,
// skipping the test if it is not set or empty.
func requireEnv(t *testing.T, key string) string {
	t.Helper()
	v := os.Getenv(key)
	if v == "" {
		t.Skipf("skipping: %s not set", key)
	}
	return v
}

// getCredential creates an azcore.TokenCredential from ARM_* environment variables.
func getCredential(t *testing.T) azcore.TokenCredential {
	t.Helper()
	cfg, err := auth.NewCredentialConfiguration(auth.WithDefaultEnvironmentVariables())
	if err != nil {
		t.Fatalf("creating credential config: %v", err)
	}
	cred, err := cfg.TokenCredential()
	if err != nil {
		t.Fatalf("creating token credential: %v", err)
	}
	return cred
}

// createContainer creates a blob container in the given storage account.
func createContainer(t *testing.T, ctx context.Context, cred azcore.TokenCredential, storageAccountName, containerName string) {
	t.Helper()
	serviceURL := fmt.Sprintf("https://%s.blob.core.windows.net", storageAccountName)
	client, err := azblob.NewClient(serviceURL, cred, nil)
	if err != nil {
		t.Fatalf("creating blob client for %s: %v", storageAccountName, err)
	}
	if _, err = client.CreateContainer(ctx, containerName, nil); err != nil {
		t.Fatalf("creating container %s: %v", containerName, err)
	}
}

// deleteContainer deletes a blob container. Errors are logged but do not fail
// the test (cleanup best-effort).
func deleteContainer(t *testing.T, ctx context.Context, cred azcore.TokenCredential, storageAccountName, containerName string) {
	t.Helper()
	serviceURL := fmt.Sprintf("https://%s.blob.core.windows.net", storageAccountName)
	client, err := azblob.NewClient(serviceURL, cred, nil)
	if err != nil {
		t.Logf("WARNING: creating blob client for cleanup: %v", err)
		return
	}
	if _, err = client.DeleteContainer(ctx, containerName, nil); err != nil {
		t.Logf("WARNING: deleting container %s: %v", containerName, err)
	}
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

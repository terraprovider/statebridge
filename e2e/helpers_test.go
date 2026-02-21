//go:build e2e

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
	"testing"

	"github.com/hashicorp/terraform-exec/tfexec"
	tfjson "github.com/hashicorp/terraform-json"

	"github.com/redtenant/tfmigrate/pkg/engine"
	"github.com/redtenant/tfmigrate/pkg/state"
)

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

// uniquePrefix returns a short, globally-unique, lowercase prefix suitable
// for Azure resource names. Format: "tfe2e" + 4 random hex chars = 9 chars.
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
	tf.SetStdout(os.Stderr) // route tofu output to test stderr
	tf.SetStderr(os.Stderr)
	return tf
}

// varOpts converts a map of variable key-value pairs to tfexec options.
func varOpts(vars map[string]string) []string {
	// Sort keys for deterministic ordering
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
// Errors are logged but do not fail the test (cleanup best-effort).
func tofuDestroy(t *testing.T, workDir string, vars map[string]string) {
	t.Helper()
	tf := newTerraform(t, workDir)
	ctx := context.Background()

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

// writeMigration creates a migration YAML file in <dir>/migrations/<name>
// and returns the path to the migrations directory.
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
// workDir is the root directory (parent of layers/), migrationPaths are paths to
// YAML files or directories. Returns the list of generated file paths.
func runGenerate(t *testing.T, workDir string, migrationPaths []string) []string {
	t.Helper()

	// Change to workDir so relative layer paths resolve correctly
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getting working directory: %v", err)
	}
	if err := os.Chdir(workDir); err != nil {
		t.Fatalf("changing to work dir %s: %v", workDir, err)
	}
	defer func() {
		if err := os.Chdir(origDir); err != nil {
			t.Fatalf("restoring working directory: %v", err)
		}
	}()

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

	files, err := eng.ProcessFiles(ctx, migrationPaths)
	if err != nil {
		t.Fatalf("processing migration files: %v", err)
	}
	return files
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

// setupTestProject copies the testproject to a temp directory, returns the
// root directory and a cleanup function that destroys resources.
func setupTestProject(t *testing.T) (rootDir string, prefix string, vars map[string]string) {
	t.Helper()

	location := os.Getenv("E2E_LOCATION")
	if location == "" {
		location = "westeurope"
	}

	prefix = uniquePrefix(t)
	rootDir = t.TempDir()

	// Find testproject relative to this test file
	testprojectSrc := filepath.Join(testdataDir(), "testproject")
	copyDir(t, testprojectSrc, filepath.Join(rootDir, "testproject"))
	rootDir = filepath.Join(rootDir, "testproject")

	vars = map[string]string{
		"prefix":   prefix,
		"location": location,
	}

	return rootDir, prefix, vars
}

// testdataDir returns the absolute path to the e2e directory containing testproject.
func testdataDir() string {
	// This file lives in e2e/, so the testproject is at e2e/testproject/
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

// formatMigrationYAML is a helper that uses fmt.Sprintf to format migration YAML.
func formatMigrationYAML(format string, args ...interface{}) string {
	return fmt.Sprintf(format, args...)
}

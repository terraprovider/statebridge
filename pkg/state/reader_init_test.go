package state

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/hashicorp/terraform-exec/tfexec"
)

// TestRunInit_DoesNotLeakStateJSONAfterward is a regression test for a bug
// where TofuStateReader's auto-init retry path leaked the entire raw state
// JSON to stderr in CI logs.
//
// runInit sets tf.SetStdout(os.Stderr)/tf.SetStderr(os.Stderr) so that `tofu
// init`'s own human-readable progress is visible. But terraform-exec merges
// those configured writers into *every* subsequent command run through the
// same *tfexec.Terraform instance -- including the internal buffer that
// JSON-returning calls like Show() use to capture their result. Since
// ReadState reuses the same tf instance for the retry Show() call made right
// after runInit returns, an unreset writer would cause that call to also
// stream the complete state JSON to stderr. This test exercises exactly that
// sequence (runInit, then Show() on the same instance) and asserts nothing
// leaks.
func TestRunInit_DoesNotLeakStateJSONAfterward(t *testing.T) {
	tofuPath, err := exec.LookPath("tofu")
	if err != nil {
		t.Skip("tofu binary not found in PATH; skipping")
	}

	dir := t.TempDir()
	// A bare terraform{} block needs no providers and no network access, so
	// `tofu init` succeeds trivially and `tofu show -json` returns a small,
	// deterministic, non-empty JSON payload ({"format_version":"1.0"}).
	if err := os.WriteFile(filepath.Join(dir, "main.tf"), []byte("terraform {}\n"), 0o644); err != nil {
		t.Fatalf("writing main.tf: %v", err)
	}

	tf, err := tfexec.NewTerraform(dir, tofuPath)
	if err != nil {
		t.Fatalf("creating terraform-exec instance: %v", err)
	}

	// Redirect the process-wide os.Stderr for the duration of the test, since
	// runInit hardcodes os.Stderr (matching production behaviour) rather than
	// accepting an injectable writer.
	origStderr := os.Stderr
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatalf("creating pipe: %v", err)
	}
	os.Stderr = pw

	captured := make(chan []byte, 1)
	go func() {
		data, _ := io.ReadAll(pr)
		captured <- data
	}()

	reader := &TofuStateReader{}
	ctx := context.Background()
	initErr := reader.runInit(ctx, tf, dir)

	// Mirror ReadState's auto-init retry: call Show() again on the SAME
	// *tfexec.Terraform instance used for init.
	_, showErr := tf.Show(ctx)

	if err := pw.Close(); err != nil {
		t.Logf("closing pipe writer: %v", err)
	}
	os.Stderr = origStderr
	out := <-captured
	if err := pr.Close(); err != nil {
		t.Logf("closing pipe reader: %v", err)
	}

	if initErr != nil {
		t.Fatalf("tofu init failed: %v\ncaptured output:\n%s", initErr, out)
	}
	if showErr != nil {
		t.Fatalf("tofu show failed: %v\ncaptured output:\n%s", showErr, out)
	}

	if bytes.Contains(out, []byte(`"format_version"`)) {
		t.Errorf("runInit leaked state JSON to the configured stderr writer during the subsequent Show() call; captured output:\n%s", out)
	}
}

package state

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	tfjson "github.com/hashicorp/terraform-json"
)

// failThenSucceedReader fails the first N reads for a layer path, then
// returns the given state on subsequent reads. Simulates a layer that
// becomes readable after init.
type failThenSucceedReader struct {
	state     *tfjson.State
	failsLeft map[string]*int32
}

func newFailThenSucceedReader(state *tfjson.State, failCounts map[string]int32) *failThenSucceedReader {
	fails := make(map[string]*int32)
	for k, v := range failCounts {
		val := v
		fails[k] = &val
	}
	return &failThenSucceedReader{state: state, failsLeft: fails}
}

func (r *failThenSucceedReader) ReadState(_ context.Context, layerPath string) (*tfjson.State, error) {
	if counter, ok := r.failsLeft[layerPath]; ok {
		if atomic.AddInt32(counter, -1) >= 0 {
			return nil, fmt.Errorf("state read failed for %q (not initialized)", layerPath)
		}
	}
	return r.state, nil
}

// alwaysFailReader always returns an error. Used to test init failure cases.
type alwaysFailReader struct{}

func (r *alwaysFailReader) ReadState(_ context.Context, layerPath string) (*tfjson.State, error) {
	return nil, fmt.Errorf("permanent failure for %q", layerPath)
}

func TestInitStateReader_NoInitNeeded(t *testing.T) {
	testState := &tfjson.State{FormatVersion: "1.0"}
	inner := newFailThenSucceedReader(testState, nil) // never fails

	reader := NewInitStateReader(inner, "/usr/bin/true", nil)
	s, err := reader.ReadState(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s != testState {
		t.Error("expected state to be returned directly")
	}
}

func TestInitStateReader_InitTriggeredOnFailure(t *testing.T) {
	layerDir := t.TempDir()
	testState := &tfjson.State{FormatVersion: "1.0"}
	// Fail the first read, succeed on retry (after init)
	inner := newFailThenSucceedReader(testState, map[string]int32{layerDir: 1})

	// Use "true" as the tofu binary — it just exits 0 (simulates successful init)
	reader := NewInitStateReader(inner, "/usr/bin/true", nil)
	s, err := reader.ReadState(context.Background(), layerDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s != testState {
		t.Error("expected state to be returned after init retry")
	}
}

func TestInitStateReader_InitWithArgs(t *testing.T) {
	layerDir := t.TempDir()
	testState := &tfjson.State{FormatVersion: "1.0"}
	inner := newFailThenSucceedReader(testState, map[string]int32{layerDir: 1})

	// "true" ignores all args, just validates the flow
	reader := NewInitStateReader(inner, "/usr/bin/true", []string{"-backend-config=bucket=test", "-reconfigure"})
	s, err := reader.ReadState(context.Background(), layerDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s != testState {
		t.Error("expected state to be returned after init with args")
	}
}

func TestInitStateReader_InitFailurePropagates(t *testing.T) {
	layerDir := t.TempDir()
	inner := &alwaysFailReader{}

	// Use "false" as the tofu binary — it exits 1 (simulates failed init)
	reader := NewInitStateReader(inner, "/usr/bin/false", nil)
	_, err := reader.ReadState(context.Background(), layerDir)
	if err == nil {
		t.Fatal("expected error when init fails")
	}
	if !strings.Contains(err.Error(), "init failed") {
		t.Errorf("expected error to mention init failure, got: %v", err)
	}
}

func TestInitStateReader_RetryFailurePropagates(t *testing.T) {
	layerDir := t.TempDir()
	// Always fails, even after init succeeds
	inner := &alwaysFailReader{}

	// "true" succeeds (init "works") but state read still fails
	reader := NewInitStateReader(inner, "/usr/bin/true", nil)
	_, err := reader.ReadState(context.Background(), layerDir)
	if err == nil {
		t.Fatal("expected error when retry fails after successful init")
	}
	if !strings.Contains(err.Error(), "after init") {
		t.Errorf("expected error to mention 'after init', got: %v", err)
	}
}

func TestInitStateReader_NoDoubleInit(t *testing.T) {
	layerDir := t.TempDir()
	// Fail for both reads — init should only be attempted once
	inner := &alwaysFailReader{}

	reader := NewInitStateReader(inner, "/usr/bin/true", nil)

	// First call: init runs, retry fails
	_, err1 := reader.ReadState(context.Background(), layerDir)
	if err1 == nil {
		t.Fatal("expected error on first call")
	}

	// Second call: init should NOT run again (already initialized), just fails directly
	_, err2 := reader.ReadState(context.Background(), layerDir)
	if err2 == nil {
		t.Fatal("expected error on second call")
	}
	// The error should mention "after init" since the layer was already initialized
	if !strings.Contains(err2.Error(), "after init") {
		t.Errorf("expected 'after init' error, got: %v", err2)
	}
}

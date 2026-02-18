package engine

import (
	"strings"
	"testing"
)

func TestWildcardTracker_NonOverlappingClaims(t *testing.T) {
	tracker := newWildcardTracker()
	key := wildcardSourceKey{layer: "./layers/old", baseAddr: "aws_resource.items"}

	tracker.setAllKeys(key, []string{"eng_admin", "eng_reader", "fin_admin", "fin_reader"})
	tracker.markPrefixFiltered(key)

	if err := tracker.claimKeys(key, []string{"eng_admin", "eng_reader"}, 0); err != nil {
		t.Fatalf("unexpected error claiming eng_ keys: %v", err)
	}

	if err := tracker.claimKeys(key, []string{"fin_admin", "fin_reader"}, 1); err != nil {
		t.Fatalf("unexpected error claiming fin_ keys: %v", err)
	}

	if err := tracker.checkCompleteness(); err != nil {
		t.Fatalf("unexpected completeness error: %v", err)
	}
}

func TestWildcardTracker_OverlappingClaims(t *testing.T) {
	tracker := newWildcardTracker()
	key := wildcardSourceKey{layer: "./layers/old", baseAddr: "aws_resource.items"}

	tracker.setAllKeys(key, []string{"shared_key", "other_key"})
	tracker.markPrefixFiltered(key)

	if err := tracker.claimKeys(key, []string{"shared_key"}, 0); err != nil {
		t.Fatalf("unexpected error on first claim: %v", err)
	}

	err := tracker.claimKeys(key, []string{"shared_key"}, 1)
	if err == nil {
		t.Fatal("expected error for overlapping claim")
	}
	if !strings.Contains(err.Error(), "operation[0]") || !strings.Contains(err.Error(), "operation[1]") {
		t.Errorf("expected error to mention both operation indices, got: %v", err)
	}
	if !strings.Contains(err.Error(), "shared_key") {
		t.Errorf("expected error to mention the overlapping key, got: %v", err)
	}
}

func TestWildcardTracker_ShouldEmitRemoved(t *testing.T) {
	tracker := newWildcardTracker()
	key := wildcardSourceKey{layer: "./layers/old", baseAddr: "aws_resource.items"}

	// First call should return true
	if !tracker.shouldEmitRemoved(key) {
		t.Error("expected first shouldEmitRemoved to return true")
	}

	// Second call should return false (deduplicated)
	if tracker.shouldEmitRemoved(key) {
		t.Error("expected second shouldEmitRemoved to return false")
	}

	// Different key should return true
	otherKey := wildcardSourceKey{layer: "./layers/other", baseAddr: "aws_resource.other"}
	if !tracker.shouldEmitRemoved(otherKey) {
		t.Error("expected shouldEmitRemoved for different key to return true")
	}
}

func TestWildcardTracker_CompletenessAllCovered(t *testing.T) {
	tracker := newWildcardTracker()
	key := wildcardSourceKey{layer: "./layers/old", baseAddr: "aws_resource.items"}

	tracker.setAllKeys(key, []string{"a", "b", "c"})
	tracker.markPrefixFiltered(key)
	if err := tracker.claimKeys(key, []string{"a", "b", "c"}, 0); err != nil {
		t.Fatal(err)
	}

	if err := tracker.checkCompleteness(); err != nil {
		t.Fatalf("expected no completeness error, got: %v", err)
	}
}

func TestWildcardTracker_CompletenessUncoveredKeys(t *testing.T) {
	tracker := newWildcardTracker()
	key := wildcardSourceKey{layer: "./layers/old", baseAddr: "aws_resource.items"}

	tracker.setAllKeys(key, []string{"eng_admin", "fin_admin", "other_admin"})
	tracker.markPrefixFiltered(key)

	// Only claim eng_ and fin_ keys, leaving other_admin uncovered
	if err := tracker.claimKeys(key, []string{"eng_admin"}, 0); err != nil {
		t.Fatal(err)
	}
	if err := tracker.claimKeys(key, []string{"fin_admin"}, 1); err != nil {
		t.Fatal(err)
	}

	err := tracker.checkCompleteness()
	if err == nil {
		t.Fatal("expected completeness error for uncovered keys")
	}
	if !strings.Contains(err.Error(), "other_admin") {
		t.Errorf("expected error to list uncovered key 'other_admin', got: %v", err)
	}
	if !strings.Contains(err.Error(), "completeness check failed") {
		t.Errorf("expected error message to mention completeness, got: %v", err)
	}
}

func TestWildcardTracker_SkipsNonPrefixFiltered(t *testing.T) {
	tracker := newWildcardTracker()
	key := wildcardSourceKey{layer: "./layers/old", baseAddr: "aws_resource.items"}

	// Set all keys but don't mark as prefix-filtered
	tracker.setAllKeys(key, []string{"a", "b", "c"})
	// Don't call markPrefixFiltered — simulates a wildcard move without key_prefix

	// Completeness check should pass (non-prefix-filtered sources are skipped)
	if err := tracker.checkCompleteness(); err != nil {
		t.Fatalf("expected no error for non-prefix-filtered source, got: %v", err)
	}
}

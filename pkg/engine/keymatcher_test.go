package engine

import (
	"testing"
)

func TestKeyMatcher_ExactOnly(t *testing.T) {
	keys := map[string]string{
		"key_a": "new_a",
		"key_b": "new_b",
	}
	m, err := newKeyMatcher(keys)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tmpl, ok := m.Match("key_a")
	if !ok || tmpl != "new_a" {
		t.Errorf("expected match new_a, got %q (matched=%v)", tmpl, ok)
	}

	tmpl, ok = m.Match("key_b")
	if !ok || tmpl != "new_b" {
		t.Errorf("expected match new_b, got %q (matched=%v)", tmpl, ok)
	}

	_, ok = m.Match("key_c")
	if ok {
		t.Error("expected no match for key_c")
	}
}

func TestKeyMatcher_PrefixOnly(t *testing.T) {
	keys := map[string]string{
		"eng_*": `{{ .Key | trimPrefix "eng_" }}`,
		"fin_*": `{{ .Key | trimPrefix "fin_" }}`,
	}
	m, err := newKeyMatcher(keys)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tmpl, ok := m.Match("eng_admin")
	if !ok || tmpl != `{{ .Key | trimPrefix "eng_" }}` {
		t.Errorf("expected eng_ template match, got %q (matched=%v)", tmpl, ok)
	}

	tmpl, ok = m.Match("fin_reader")
	if !ok || tmpl != `{{ .Key | trimPrefix "fin_" }}` {
		t.Errorf("expected fin_ template match, got %q (matched=%v)", tmpl, ok)
	}

	_, ok = m.Match("other_key")
	if ok {
		t.Error("expected no match for other_key")
	}
}

func TestKeyMatcher_LongestPrefixWins(t *testing.T) {
	keys := map[string]string{
		"abc_*":    "short",
		"abcdef_*": "long",
	}
	m, err := newKeyMatcher(keys)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// "abcdef_xyz" matches both "abc_" and "abcdef_", longest wins
	tmpl, ok := m.Match("abcdef_xyz")
	if !ok || tmpl != "long" {
		t.Errorf("expected longest prefix match 'long', got %q (matched=%v)", tmpl, ok)
	}

	// "abc_xyz" matches only "abc_"
	tmpl, ok = m.Match("abc_xyz")
	if !ok || tmpl != "short" {
		t.Errorf("expected short prefix match, got %q (matched=%v)", tmpl, ok)
	}
}

func TestKeyMatcher_CatchAll(t *testing.T) {
	keys := map[string]string{
		"specific": "exact_match",
		"*":        "fallback",
	}
	m, err := newKeyMatcher(keys)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tmpl, ok := m.Match("specific")
	if !ok || tmpl != "exact_match" {
		t.Errorf("expected exact match, got %q (matched=%v)", tmpl, ok)
	}

	tmpl, ok = m.Match("anything_else")
	if !ok || tmpl != "fallback" {
		t.Errorf("expected catch-all match, got %q (matched=%v)", tmpl, ok)
	}
}

func TestKeyMatcher_MixedPatterns(t *testing.T) {
	keys := map[string]string{
		"exact_key":  "exact_value",
		"prefix_a_*": "prefix_a_template",
		"prefix_b_*": "prefix_b_template",
		"*":          "catch_all",
	}
	m, err := newKeyMatcher(keys)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tests := []struct {
		key  string
		want string
	}{
		{"exact_key", "exact_value"},
		{"prefix_a_foo", "prefix_a_template"},
		{"prefix_b_bar", "prefix_b_template"},
		{"unknown", "catch_all"},
	}

	for _, tt := range tests {
		tmpl, ok := m.Match(tt.key)
		if !ok || tmpl != tt.want {
			t.Errorf("Match(%q) = (%q, %v), want (%q, true)", tt.key, tmpl, ok, tt.want)
		}
	}
}

func TestKeyMatcher_ExactTakesPriorityOverPrefix(t *testing.T) {
	keys := map[string]string{
		"eng_admin": "exact_admin",
		"eng_*":     "prefix_template",
	}
	m, err := newKeyMatcher(keys)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// "eng_admin" should match exact, not prefix
	tmpl, ok := m.Match("eng_admin")
	if !ok || tmpl != "exact_admin" {
		t.Errorf("expected exact match for eng_admin, got %q (matched=%v)", tmpl, ok)
	}

	// "eng_reader" should match prefix
	tmpl, ok = m.Match("eng_reader")
	if !ok || tmpl != "prefix_template" {
		t.Errorf("expected prefix match for eng_reader, got %q (matched=%v)", tmpl, ok)
	}
}

func TestKeyMatcher_MatchedKeys(t *testing.T) {
	keys := map[string]string{
		"eng_*": "eng_template",
		"fin_*": "fin_template",
	}
	m, err := newKeyMatcher(keys)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	allKeys := []string{"eng_admin", "fin_reader", "other_key", "eng_dev"}
	matches := m.MatchedKeys(allKeys)

	if len(matches) != 3 {
		t.Fatalf("expected 3 matches, got %d", len(matches))
	}

	// Verify matched keys (order follows allKeys order)
	expected := map[string]string{
		"eng_admin":  "eng_template",
		"fin_reader": "fin_template",
		"eng_dev":    "eng_template",
	}
	for _, match := range matches {
		want, ok := expected[match.Key]
		if !ok {
			t.Errorf("unexpected match key %q", match.Key)
		}
		if match.DestTemplate != want {
			t.Errorf("match %q: expected template %q, got %q", match.Key, want, match.DestTemplate)
		}
	}
}

func TestKeyMatcher_DuplicateExactKey(t *testing.T) {
	// map[string]string can't have duplicate keys in Go, so this
	// test verifies that the matcher doesn't error with normal maps.
	keys := map[string]string{
		"key_a": "value_a",
	}
	_, err := newKeyMatcher(keys)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestKeyMatcher_EmptyKeys(t *testing.T) {
	m, err := newKeyMatcher(map[string]string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, ok := m.Match("anything")
	if ok {
		t.Error("expected no match with empty keys map")
	}
}

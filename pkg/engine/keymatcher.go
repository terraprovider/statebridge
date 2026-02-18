package engine

import (
	"fmt"
	"sort"
	"strings"
)

// keyMatcher matches for_each state keys against a map of key patterns.
// It supports three kinds of patterns:
//   - Exact keys:  "some_key" → matched by equality
//   - Prefix keys: "some_prefix_*" → matched when the state key starts with "some_prefix_"
//   - Catch-all:   "*" → matches any key not matched by the above
//
// Match priority: exact > longest prefix > catch-all.
type keyMatcher struct {
	// exact maps literal key names to their destination templates.
	exact map[string]string

	// prefixes holds prefix patterns sorted by prefix length (longest first)
	// so that more specific prefixes are tried before shorter ones.
	prefixes []prefixEntry

	// catchAll holds the destination template for the "*" catch-all pattern.
	// nil if no catch-all is defined.
	catchAll *string
}

// prefixEntry represents a single prefix pattern with its destination template.
type prefixEntry struct {
	prefix   string
	template string
}

// newKeyMatcher builds a keyMatcher from a keys map. Keys ending with "*"
// (but longer than just "*") are treated as prefix patterns; a sole "*" is
// the catch-all; all other keys are exact matches.
func newKeyMatcher(keys map[string]string) (*keyMatcher, error) {
	m := &keyMatcher{
		exact: make(map[string]string),
	}

	for pattern, tmpl := range keys {
		switch {
		case pattern == "*":
			if m.catchAll != nil {
				return nil, fmt.Errorf("duplicate catch-all (*) pattern")
			}
			t := tmpl
			m.catchAll = &t

		case strings.HasSuffix(pattern, "*"):
			prefix := strings.TrimSuffix(pattern, "*")
			if prefix == "" {
				return nil, fmt.Errorf("empty prefix before wildcard in pattern %q", pattern)
			}
			m.prefixes = append(m.prefixes, prefixEntry{
				prefix:   prefix,
				template: tmpl,
			})

		default:
			if _, exists := m.exact[pattern]; exists {
				return nil, fmt.Errorf("duplicate exact key pattern %q", pattern)
			}
			m.exact[pattern] = tmpl
		}
	}

	// Sort prefixes by length descending (longest first) so that more
	// specific prefixes are matched before shorter ones.
	sort.Slice(m.prefixes, func(i, j int) bool {
		return len(m.prefixes[i].prefix) > len(m.prefixes[j].prefix)
	})

	return m, nil
}

// Match returns the destination template for the given state key.
// It tries exact matches first, then prefix matches (longest first),
// then the catch-all. Returns matched=false if no pattern matches.
func (m *keyMatcher) Match(key string) (destTemplate string, matched bool) {
	// 1. Exact match
	if tmpl, ok := m.exact[key]; ok {
		return tmpl, true
	}

	// 2. Prefix match (longest first)
	for _, pe := range m.prefixes {
		if strings.HasPrefix(key, pe.prefix) {
			return pe.template, true
		}
	}

	// 3. Catch-all
	if m.catchAll != nil {
		return *m.catchAll, true
	}

	return "", false
}

// MatchedKeys returns the list of state keys from allKeys that match any
// pattern, along with the destination template for each.
func (m *keyMatcher) MatchedKeys(allKeys []string) []keyMatch {
	var matches []keyMatch
	for _, key := range allKeys {
		if tmpl, ok := m.Match(key); ok {
			matches = append(matches, keyMatch{
				Key:          key,
				DestTemplate: tmpl,
			})
		}
	}
	return matches
}

// keyMatch pairs a matched state key with its destination template.
type keyMatch struct {
	Key          string
	DestTemplate string
}

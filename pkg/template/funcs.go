package template

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"text/template"
)

// regexCache caches compiled regular expressions keyed by pattern string.
// Bounded to maxRegexCacheSize entries to prevent unbounded memory growth.
var regexCache sync.Map

var (
	regexCacheLen atomic.Int64
	regexCacheMu  sync.Mutex
)

const maxRegexCacheSize = 1000

// funcMap is the shared, immutable function map for all template evaluations.
// These supplement Go's built-in template functions with string manipulation
// helpers commonly needed for key transformations.
//
// All string transformation functions accept the input string as the last parameter
// so they work naturally with Go template pipes (e.g., {{ .Key | replace "-" "_" }}).
var funcMap = template.FuncMap{
	// String replacement.
	// Usage: {{ .Key | replace "old" "new" }}
	"replace": func(old, new, s string) string {
		return strings.ReplaceAll(s, old, new)
	},
	// Usage: {{ .Key | replaceN "old" "new" 1 }}
	"replaceN": func(old, new string, n int, s string) string {
		return strings.Replace(s, old, new, n)
	},
	// Usage: {{ .Key | trimPrefix "prefix-" }}
	"trimPrefix": func(prefix, s string) string {
		return strings.TrimPrefix(s, prefix)
	},
	// Usage: {{ .Key | trimSuffix "-suffix" }}
	"trimSuffix": func(suffix, s string) string {
		return strings.TrimSuffix(s, suffix)
	},
	"trimSpace": strings.TrimSpace,

	// Case transformations
	"lower": strings.ToLower,
	"upper": strings.ToUpper,
	"title": strings.ToTitle,

	// String splitting and joining.
	// Usage: {{ .Key | split "-" | join "_" }}
	"split": func(sep, s string) []string {
		return strings.Split(s, sep)
	},
	"join": func(sep string, elems []string) string {
		return strings.Join(elems, sep)
	},

	// Index into a string slice. Pipe-compatible alternative to the
	// built-in index function (which takes collection first).
	// Usage: {{ .Key | split "/" | at 1 }}
	"at": atFunc,

	// String testing.
	// Usage: {{ if hasPrefix .Key "prod" }}...{{ end }}
	"hasPrefix": func(prefix, s string) bool {
		return strings.HasPrefix(s, prefix)
	},
	"hasSuffix": func(suffix, s string) bool {
		return strings.HasSuffix(s, suffix)
	},
	"contains": func(substr, s string) bool {
		return strings.Contains(s, substr)
	},

	// Map access for nested attribute lookups.
	// Usage: {{ attr .Attributes "tags" "Name" }}
	"attr": attrLookup,

	// Default value: returns the first non-empty argument.
	// Usage: {{ .Key | default "fallback" }}
	"default": defaultFunc,

	// Quote wraps a string in double quotes.
	// Usage: {{ .Key | quote }}
	"quote": func(s string) string {
		return fmt.Sprintf("%q", s)
	},

	// Sprintf for custom formatting.
	// Usage: {{ printf "%s-%s" .Type .Name }}
	"printf": fmt.Sprintf,

	// Regex replacement.
	// Usage: {{ .Key | regexReplace "[^a-zA-Z0-9]+" "_" }}
	"regexReplace": regexReplaceFunc,

	// Sanitize a string into a safe for_each key: lowercase and replace
	// all non-alphanumeric characters with underscores, collapsing runs.
	// Usage: {{ printf "%s_%s" .Attributes.package_key .Attributes.role | sanitizeKey }}
	// Equivalent to the Terraform expression:
	//   lower(replace(format(...), "/[^a-zA-Z0-9]+/", "_"))
	"sanitizeKey": sanitizeKeyFunc,

	// Build a for_each key from multiple values: formats them with the
	// given format string, then sanitizes the result (lowercase + replace
	// non-alphanumeric chars with underscore).
	// Usage: {{ formatKey "%s_%s" .Attributes.access_package_key .Attributes.role }}
	"formatKey": formatKeyFunc,
}

// attrLookup navigates nested maps to retrieve a value.
// It takes a root map and a variadic list of string keys to traverse.
// Returns the value at the final key, or an error if any key is missing.
func attrLookup(m map[string]interface{}, keys ...string) (interface{}, error) {
	if len(keys) == 0 {
		return nil, fmt.Errorf("attr requires at least one key argument")
	}

	var current interface{} = m
	for i, key := range keys {
		currentMap, ok := current.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("attr: value at key path %v is not a map (at key %q)", keys[:i], key)
		}
		val, exists := currentMap[key]
		if !exists {
			return nil, fmt.Errorf("attr: key %q not found at path %v", key, keys[:i+1])
		}
		current = val
	}

	return current, nil
}

// defaultFunc returns the value if it is non-empty, otherwise returns the fallback.
func defaultFunc(fallback string, val string) string {
	if val == "" {
		return fallback
	}
	return val
}

// atFunc returns the element at position i from a string slice.
// The index is the first parameter for pipe compatibility:
//
//	{{ .Key | split "/" | at 1 }}
func atFunc(i int, s []string) (string, error) {
	if i < 0 || i >= len(s) {
		return "", fmt.Errorf("at: index %d out of range for slice of length %d", i, len(s))
	}
	return s[i], nil
}

// nonAlphanumRegex matches one or more non-alphanumeric characters.
var nonAlphanumRegex = regexp.MustCompile(`[^a-zA-Z0-9]+`)

// regexReplaceFunc replaces all matches of a regex pattern with a replacement string.
// The input string is the last parameter for pipe compatibility.
func regexReplaceFunc(pattern, repl, s string) (string, error) {
	var re *regexp.Regexp
	if cached, ok := regexCache.Load(pattern); ok {
		re = cached.(*regexp.Regexp)
	} else {
		var err error
		re, err = regexp.Compile(pattern)
		if err != nil {
			return "", fmt.Errorf("regexReplace: invalid pattern %q: %w", pattern, err)
		}
		regexCacheMu.Lock()
		if regexCacheLen.Load() < maxRegexCacheSize {
			if _, loaded := regexCache.LoadOrStore(pattern, re); !loaded {
				regexCacheLen.Add(1)
			}
		}
		regexCacheMu.Unlock()
	}
	return re.ReplaceAllString(s, repl), nil
}

// sanitizeKeyFunc lowercases a string and replaces all runs of
// non-alphanumeric characters with a single underscore. Trailing
// underscores are stripped.
//
// This mirrors the common Terraform pattern:
//
//	lower(replace(value, "/[^a-zA-Z0-9]+/", "_"))
func sanitizeKeyFunc(s string) string {
	s = strings.ToLower(s)
	s = nonAlphanumRegex.ReplaceAllString(s, "_")
	s = strings.Trim(s, "_")
	return s
}

// formatKeyFunc formats a string from the given arguments using fmt.Sprintf,
// then sanitizes the result into a safe for_each key.
//
// This is a convenience combining printf + sanitizeKey in one call:
//
//	{{ formatKey "%s_%s" .Attributes.access_package_key .Attributes.role }}
//
// is equivalent to:
//
//	{{ printf "%s_%s" .Attributes.access_package_key .Attributes.role | sanitizeKey }}
//
// which mirrors the Terraform expression:
//
//	lower(replace(format("%s_%s", item.access_package_key, item.role), "/[^a-zA-Z0-9]+/", "_"))
func formatKeyFunc(format string, args ...interface{}) string {
	return sanitizeKeyFunc(fmt.Sprintf(format, args...))
}

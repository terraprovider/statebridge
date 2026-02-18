package template

import (
	"fmt"
	"strings"
	"text/template"
)

// FuncMap returns custom template functions available in migration templates.
// These supplement Go's built-in template functions with string manipulation
// helpers commonly needed for key transformations.
//
// All string transformation functions accept the input string as the last parameter
// so they work naturally with Go template pipes (e.g., {{ .Key | replace "-" "_" }}).
func FuncMap() template.FuncMap {
	return template.FuncMap{
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
	}
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

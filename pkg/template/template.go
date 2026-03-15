// Package template provides Go text/template evaluation for tfmigrate
// migration address and import ID transformations.
package template

import (
	"bytes"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"text/template"
)

// templateCache caches parsed templates keyed by template string.
// Bounded to maxTemplateCacheSize entries to prevent unbounded memory growth.
var templateCache sync.Map

var templateCacheLen atomic.Int64

const maxTemplateCacheSize = 1000

// funcMap is re-exported from funcs.go where it is defined as a package-level var.

// TemplateContext is the data made available to Go templates when evaluating
// address and import_id expressions. It contains the full state context
// for a single resource instance.
type TemplateContext struct {
	// Address is the full Terraform address of the source resource instance
	// (e.g., "aws_s3_bucket.data[\"key-1\"]").
	Address string

	// Type is the resource type (e.g., "aws_s3_bucket").
	Type string

	// Name is the resource name (e.g., "data").
	Name string

	// Index is the raw for_each key (string) or count index (float64), or nil.
	Index interface{}

	// Key is the string representation of Index.
	Key string

	// Attributes contains all attribute values from the Terraform state.
	// Nested objects are represented as map[string]interface{}.
	Attributes map[string]interface{}

	// Item is the current list element when expanding an attribute list
	// (via ImportSource.Expand). It is nil when not in an expansion context.
	// For objects, it is a map[string]interface{} supporting field access
	// (e.g., .Item.resource_app_id).
	Item interface{}

	// ItemIndex is the 0-based index of the current Item within the expanded
	// list. It is 0 when not in an expansion context.
	ItemIndex int
}

// Evaluate processes a Go template string with the given context and returns
// the rendered result. Returns an error if the template is syntactically invalid
// or if evaluation fails (e.g., accessing a missing attribute).
func Evaluate(tmplStr string, ctx *TemplateContext) (string, error) {
	var tmpl *template.Template
	if cached, ok := templateCache.Load(tmplStr); ok {
		tmpl = cached.(*template.Template)
	} else {
		var err error
		tmpl, err = template.New("migration").
			Funcs(funcMap).
			Option("missingkey=error").
			Parse(tmplStr)
		if err != nil {
			return "", fmt.Errorf("parsing template %q: %w", tmplStr, err)
		}
		if templateCacheLen.Load() < maxTemplateCacheSize {
			templateCache.Store(tmplStr, tmpl)
			templateCacheLen.Add(1)
		}
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, ctx); err != nil {
		return "", fmt.Errorf("evaluating template %q: %w", tmplStr, err)
	}

	return buf.String(), nil
}

// IsTemplate returns true if the string contains Go template syntax ({{ }}).
func IsTemplate(s string) bool {
	return strings.Contains(s, "{{") && strings.Contains(s, "}}")
}

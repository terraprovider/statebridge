// Package template provides Go text/template evaluation for tfmigrate
// migration address and import ID transformations.
package template

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"
)

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
}

// Evaluate processes a Go template string with the given context and returns
// the rendered result. Returns an error if the template is syntactically invalid
// or if evaluation fails (e.g., accessing a missing attribute).
func Evaluate(tmplStr string, ctx *TemplateContext) (string, error) {
	tmpl, err := template.New("migration").
		Funcs(FuncMap()).
		Option("missingkey=error").
		Parse(tmplStr)
	if err != nil {
		return "", fmt.Errorf("parsing template %q: %w", tmplStr, err)
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

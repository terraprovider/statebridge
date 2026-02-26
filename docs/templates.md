# Go Template Reference

Templates can be used in `keys` map values and `import_id` fields. They are evaluated once per matched resource instance using Go's `text/template` syntax.

## Context Variables

| Field | Type | Example | Description |
|-------|------|---------|-------------|
| `.Address` | `string` | `aws_s3_bucket.data["key-1"]` | Full source resource address |
| `.Type` | `string` | `aws_s3_bucket` | Resource type |
| `.Name` | `string` | `data` | Resource name |
| `.Index` | `any` | `"key-1"` or `0` | Raw for_each key or count index |
| `.Key` | `string` | `"key-1"` | String form of `.Index` |
| `.Attributes` | `map` | `{"id": "...", "bucket": "..."}` | All state attributes |

## Template Functions

### String Manipulation

| Function | Usage | Description |
|----------|-------|-------------|
| `replace` | `{{ .Key \| replace "-" "_" }}` | Replace all occurrences |
| `replaceN` | `{{ .Key \| replaceN "-" "_" 1 }}` | Replace first N occurrences |
| `trimPrefix` | `{{ .Key \| trimPrefix "prefix-" }}` | Remove prefix |
| `trimSuffix` | `{{ .Key \| trimSuffix "-suffix" }}` | Remove suffix |
| `trimSpace` | `{{ .Key \| trimSpace }}` | Strip whitespace |
| `lower` | `{{ .Key \| lower }}` | Lowercase |
| `upper` | `{{ .Key \| upper }}` | Uppercase |
| `split` | `{{ .Key \| split "-" }}` | Split into list |
| `join` | `{{ .Key \| split "-" \| join "_" }}` | Join list |
| `at` | `{{ .Key \| split "/" \| at 1 }}` | Index into list (pipe-compatible) |

### Testing

| Function | Usage | Description |
|----------|-------|-------------|
| `hasPrefix` | `{{ if .Key \| hasPrefix "prod" }}...{{ end }}` | Test prefix |
| `hasSuffix` | `{{ if .Key \| hasSuffix "-prod" }}...{{ end }}` | Test suffix |
| `contains` | `{{ if .Key \| contains "x" }}...{{ end }}` | Test substring |

### Regex

| Function | Usage | Description |
|----------|-------|-------------|
| `regexReplace` | `{{ .Key \| regexReplace "[^a-z0-9]+" "_" }}` | Regex-based replacement |

### Key Sanitization

| Function | Usage | Description |
|----------|-------|-------------|
| `sanitizeKey` | `{{ .Key \| sanitizeKey }}` | Lowercase + replace non-alphanumeric runs with `_` + trim edges |
| `formatKey` | `{{ formatKey "%s_%s" .Attributes.pkg .Attributes.role }}` | `printf` + `sanitizeKey` in one step |

### Nested Attributes

| Function | Usage | Description |
|----------|-------|-------------|
| `attr` | `{{ attr .Attributes "tags" "Name" }}` | Traverse nested maps |

### Utilities

| Function | Usage | Description |
|----------|-------|-------------|
| `default` | `{{ .Key \| default "fallback" }}` | Use fallback if empty |
| `quote` | `{{ .Key \| quote }}` | Wrap in double quotes |
| `printf` | `{{ printf "%s-%s" .Type .Name }}` | Format string |

## Examples

### Simple key transformation

```yaml
keys:
  "old_prefix_*": '{{ .Key | trimPrefix "old_prefix_" }}'
```

### Composite import ID from attributes

```yaml
import_id: "{{ .Attributes.project_id }}/{{ .Attributes.id }}"
```

### Complex multi-step transformation

```yaml
keys:
  "mrt_customer_*": 'customer_approval_{{ .Key | trimPrefix "mrt_customer_" | split "_entra_group_" | at 0 }}_AadGroup_{{ .Attributes.catalog_resource_association_id | split "/" | at 1 }}'
```

### Regex-based cleanup

```yaml
keys:
  "*": '{{ .Key | regexReplace "[^a-z0-9]+" "_" }}'
```

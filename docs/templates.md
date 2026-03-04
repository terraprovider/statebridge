# Go Template Reference

Templates can be used in `keys` map values, `import_id` fields, and `id`/`key` fields in source-based imports. They are evaluated once per matched resource instance (or once per expanded list element when `source.expand` is set) using Go's `text/template` syntax.

## Context Variables

| Field | Type | Example | Description |
|-------|------|---------|-------------|
| `.Address` | `string` | `aws_s3_bucket.data["key-1"]` | Full source resource address |
| `.Type` | `string` | `aws_s3_bucket` | Resource type |
| `.Name` | `string` | `data` | Resource name |
| `.Index` | `any` | `"key-1"` or `0` | Raw for_each key or count index |
| `.Key` | `string` | `"key-1"` | String form of `.Index` |
| `.Attributes` | `map` | `{"id": "...", "bucket": "..."}` | All state attributes |
| `.Item` | `any` | `{"resource_app_id": "..."}` | Current list element (only in `source.expand` context) |
| `.ItemIndex` | `int` | `0` | 0-based index of `.Item` in the expanded list |

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

## Source-Based Import Examples

### Derive import ID from another resource's attributes

```yaml
imports:
  - address: "azuread_application_registration.all"
    id: '{{ .Attributes.id }}'
    source:
      layer: "./layers/identity"
      address: "azuread_application.all"
```

### Remap for_each keys during source-based import

```yaml
imports:
  - address: "random_id.derived"
    id: '{{ .Attributes.b64_std }}'
    key: 'app_{{ .Key }}'
    source:
      layer: "./layers/shared"
      address: "random_id.source"
```

### Expand a list attribute into multiple imports

Each element of the `required_resource_access` list becomes a separate import:

```yaml
imports:
  - address: "azuread_api_access.all"
    id: '{{ .Attributes.id }}/apiAccess/{{ .Item.resource_app_id }}'
    key: '{{ .Key }}_{{ .Item.resource_app_id }}'
    source:
      layer: "./layers/identity"
      address: "azuread_application.all"
      expand: "required_resource_access"
```

`.Item` contains each list element and `.ItemIndex` is its zero-based position.

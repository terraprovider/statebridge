---
description: "Use when editing or creating tfmigrate YAML migration files. Covers the YAML schema, operation types (move, rename, remove, import), validation rules, and template syntax."
applyTo: "migrations/**/*.yaml"
---

# tfmigrate Migration YAML

See [AGENTS.md](../../AGENTS.md) for the full schema reference and translation patterns.

## Quick Reference

### Top-level structure
```yaml
description: "<required>"
schema_version: "2"
status: retired          # optional: skip file entirely
condition: {}            # optional: explicit pre-checks
operations:
  - type: <move|rename|remove|import>
```

### Operation types
- **move** — `source_layer`, `destination_layer`, `resources` list (or `all_resources: true`)
- **rename** — `layer`, `renames` list (`from`/`to` pairs)
- **remove** — `layer`, `entries` list (`address`, optional `destroy`)
- **import** — `layer`, `imports` list (`address`, `id`, optional `source`)

### Key rules
- `description` is always required at top level
- `operations` must be non-empty
- Template expressions (`{{ }}`) only in: `keys` values, `import_id`, source-based `id`/`key`
- `address_prefix` cannot combine with `source_prefix`/`destination_prefix`
- `all_resources` cannot combine with `address_prefix`
- Same-layer moves generate `moved` blocks by default (override with `use_moved_blocks: false`)

### File naming
```
NNNN_short_description.yaml
```
Use the next available numeric prefix in the `migrations/` directory.

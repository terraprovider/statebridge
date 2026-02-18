# AGENTS.md — AI Agent Instructions for tfmigrate

This file provides context for AI coding agents (Claude Code, Copilot, Cursor, etc.) to generate tfmigrate YAML migration files from natural language descriptions.

## What This Project Does

tfmigrate is a CLI tool that generates OpenTofu/Terraform HCL migration code (`import`, `moved`, `removed` blocks) from declarative YAML files. Users describe what resources need to move, rename, import, or remove, and the tool produces the correct HCL in each affected layer directory.

A **layer** is a Terraform/OpenTofu root module identified by its filesystem path (e.g., `./layers/networking`).

## How to Generate Migration Files

When a user asks you to create a migration, produce a YAML file following the schema below. Place it in the project's migrations directory with a numeric prefix for ordering (e.g., `migrations/001_description.yaml`).

Always ask or infer:
1. Which layers (filesystem paths) are involved
2. The full Terraform resource addresses
3. Whether import IDs are known or should be auto-resolved from state

---

## YAML Schema Reference

Every migration file has this top-level structure:

```yaml
description: "<required: what this migration does>"
schema_version: "1"  # optional
operations:
  - type: <move|rename|remove|import>
    # ... fields depend on type
```

### Operation: `move`

Moves a resource between two layers. Generates `removed` in source + `import` in destination.

Required fields: `source.layer`, `source.address`, `destination.layer`, `destination.address`
Optional fields: `description`, `import_id` (auto-resolved from state if omitted)

```yaml
- type: move
  description: "Move web server to app layer"
  source:
    layer: "./layers/compute"
    address: "aws_instance.web"
  destination:
    layer: "./layers/app"
    address: "aws_instance.web"
  import_id: "i-0abc123def456"  # omit to auto-resolve from source state
```

**Wildcard moves** — use `[*]` suffix on the source address to expand all for_each/count instances. Combine with Go templates in the destination address and import_id:

```yaml
- type: move
  source:
    layer: "./layers/old"
    address: "aws_s3_bucket.data[*]"
  destination:
    layer: "./layers/new"
    address: 'aws_s3_bucket.data["{{ .Attributes.bucket }}"]'
  import_id: "{{ .Attributes.id }}"
```

### Operation: `rename`

Renames a resource within a single layer. Generates a `moved` block.

Required fields: `layer`, `from`, `to`

```yaml
- type: rename
  description: "Rename VPC module"
  layer: "./layers/networking"
  from: "module.old_vpc"
  to: "module.new_vpc"
```

### Operation: `remove`

Removes a resource from state. Generates a `removed` block. Default: `destroy = false` (keeps infrastructure).

Required fields: `layer`, `address`
Optional fields: `description`, `destroy` (default: false)

```yaml
- type: remove
  description: "Stop managing deprecated role"
  layer: "./layers/iam"
  address: "aws_iam_role.deprecated"
  destroy: false
```

### Operation: `import`

Imports an existing resource into state. Generates an `import` block.

Required fields: `layer`, `address`, `import_id`
Optional fields: `description`, `provider`

```yaml
- type: import
  description: "Import existing database"
  layer: "./layers/database"
  address: "aws_db_instance.primary"
  import_id: "my-database-identifier"
  provider: "aws.useast1"
```

---

## Go Template Reference

Templates can be used in `destination.address` and `import_id` fields for move operations. They are evaluated once per resource instance (relevant for wildcard `[*]` expansions).

### Context Variables

| Variable | Type | Example | Description |
|----------|------|---------|-------------|
| `.Address` | string | `aws_s3_bucket.data["key-1"]` | Full source resource address |
| `.Type` | string | `aws_s3_bucket` | Resource type |
| `.Name` | string | `data` | Resource name |
| `.Index` | any | `"key-1"` or `0` | Raw for_each key or count index |
| `.Key` | string | `"key-1"` | String form of `.Index` |
| `.Attributes` | map | `{"id": "...", "bucket": "..."}` | All state attributes |

### Available Functions

All string functions are pipe-compatible (input comes last).

**String manipulation:**
- `{{ .Key | replace "old" "new" }}` — replace all occurrences
- `{{ .Key | replaceN "old" "new" 1 }}` — replace first N
- `{{ .Key | trimPrefix "prefix-" }}` — strip prefix
- `{{ .Key | trimSuffix "-suffix" }}` — strip suffix
- `{{ .Key | trimSpace }}` — strip whitespace
- `{{ .Key | lower }}` — lowercase
- `{{ .Key | upper }}` — uppercase
- `{{ .Key | split "-" | join "_" }}` — split then rejoin

**Testing:**
- `{{ if .Key | hasPrefix "prod" }}...{{ end }}`
- `{{ if .Key | hasSuffix "-prod" }}...{{ end }}`
- `{{ if .Key | contains "special" }}...{{ end }}`

**Nested attribute access:**
- `{{ attr .Attributes "tags" "Name" }}` — traverse nested maps

**Utilities:**
- `{{ .Key | default "fallback" }}` — use fallback if empty
- `{{ .Key | quote }}` — wrap in double quotes
- `{{ printf "%s-%s" .Type .Name }}` — formatted string

---

## Translation Patterns

Use these patterns to translate natural language into the correct YAML.

### "Move X from layer A to layer B"

```yaml
- type: move
  source:
    layer: "<layer A path>"
    address: "<resource address>"
  destination:
    layer: "<layer B path>"
    address: "<resource address>"
```

If the user provides a specific import ID, include `import_id`. Otherwise omit it (auto-resolved from state).

### "Rename X to Y in layer A"

```yaml
- type: rename
  layer: "<layer A path>"
  from: "<old address>"
  to: "<new address>"
```

Works for resources (`aws_instance.old` → `aws_instance.new`), modules (`module.old` → `module.new`), and for_each key changes (`aws_instance.x["old-key"]` → `aws_instance.x["new-key"]`).

### "Remove X from layer A" / "Stop managing X"

```yaml
- type: remove
  layer: "<layer A path>"
  address: "<resource address>"
```

If the user says "delete" or "destroy", set `destroy: true`. If they say "remove from state" or "stop managing", use the default `destroy: false`.

### "Import X into layer A"

```yaml
- type: import
  layer: "<layer A path>"
  address: "<resource address>"
  import_id: "<cloud resource ID>"
```

The user must provide the import ID (ARN, resource ID, etc.) or you must ask for it.

### "Re-key all X resources" / "Change the for_each key"

Use wildcard `[*]` with a template:

```yaml
- type: move
  source:
    layer: "<layer path>"
    address: "<resource>[*]"
  destination:
    layer: "<layer path>"        # can be the same layer
    address: '<resource>["{{ <template expression> }}"]'
  import_id: "{{ .Attributes.id }}"
```

### "Move everything of type X from A to B"

Use wildcard for all instances:

```yaml
- type: move
  source:
    layer: "<layer A>"
    address: "<resource_type>.<name>[*]"
  destination:
    layer: "<layer B>"
    address: '<resource_type>.<name>["{{ .Key }}"]'
```

---

## Validation Rules

When generating YAML, ensure:

1. `description` at the top level is always present
2. `operations` list is non-empty
3. Every operation has a `type` field
4. `move` requires both `source` and `destination`, each with `layer` and `address`
5. `rename` requires `layer`, `from`, and `to`
6. `remove` requires `layer` and `address`
7. `import` requires `layer`, `address`, and `import_id`
8. Template expressions (`{{ }}`) are only valid in `destination.address` and `import_id` of move operations
9. Wildcard `[*]` is only valid in `source.address` of move operations
10. Layer paths are relative to where `tfmigrate generate` is run

## File Naming Convention

Migration files should be named with a sortable numeric prefix:

```
NNN_short_description.yaml
```

Examples:
- `001_move_compute_to_app.yaml`
- `002_rename_vpc_module.yaml`
- `003_import_rds_instance.yaml`

When generating files, check for existing migrations and use the next available number.

## Running After Generation

After creating a migration file, the user runs:

```bash
# Preview without writing
tfmigrate generate --dry-run migrations/

# Generate the HCL files
tfmigrate generate migrations/

# Then in each affected layer:
cd <layer-path>
tofu plan    # verify the migration
tofu apply   # execute it
```

## Project Structure

Key source files for understanding the codebase:

- `pkg/migration/schema.go` — YAML types and operation definitions
- `pkg/migration/validation.go` — validation rules
- `pkg/template/funcs.go` — available template functions
- `pkg/template/template.go` — template evaluation logic
- `pkg/engine/engine.go` — orchestration pipeline
- `pkg/engine/resolver.go` — import ID resolution and wildcard expansion
- `pkg/generator/` — HCL block rendering (import, moved, removed)
- `cmd/generate.go` — CLI entry point

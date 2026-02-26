# Migration File Format

Complete reference for the YAML migration file schema.

## Structure

Migration files are YAML documents with a description and a list of operations:

```yaml
description: "Human-readable description of this migration"
schema_version: "2"  # optional
status: retired      # optional: "retired" skips file entirely
condition:           # optional: see Conditions doc
  # ...
operations:
  - type: <move|rename|remove|import>
    # ... operation-specific fields
```

## File Naming Convention

Migration files should be named with a sortable numeric prefix:

```
NNN_short_description.yaml
```

Examples:
- `001_move_compute_to_app.yaml`
- `002_rename_vpc_module.yaml`
- `003_import_rds_instance.yaml`

## Common Fields

### `address_prefix`

All operation types support an optional `address_prefix` field that is prepended (with a dot separator) to all resource addresses in the operation:

```yaml
- type: move
  address_prefix: "module.identity_governance"
  resources:
    - from: "azuread_access_package.all"
    # Full address: module.identity_governance.azuread_access_package.all
```

## Operation Types

### `move` — Cross-Layer Resource Move

Moves resources between OpenTofu layers. Generates `removed` blocks in the source layer and `import` blocks in the destination layer.

**Required fields:** `source_layer`, `destination_layer`, and either `resources` (non-empty list) or `all_resources: true`

```yaml
- type: move
  description: "Move resources to app layer"
  source_layer: "./layers/compute"
  destination_layer: "./layers/app"
  address_prefix: "module.main"              # optional
  resources:
    - from: "aws_instance.web"
      import_id: "i-0abc123def456"           # optional: auto-resolved from state if omitted
    - from: "aws_instance.api"
```

When `import_id` is omitted, tfmigrate reads the source layer's state and extracts the resource's `id` attribute automatically.

#### `to` — Address Remapping

When the destination base address differs from the source:

```yaml
resources:
  - from: "module.old.resource.all"
    to: "module.new.resource.all"
    keys:
      key1: key1
```

#### Module-Level Move

Move an entire module (all managed resources under it) to a new layer:

```yaml
resources:
  - from: "module.foo"
  - from: "module.foo"
    to: "module.bar"   # optional: remap module prefix
```

When a module address is specified (e.g., `module.foo`), tfmigrate discovers all managed resources under that module from the source layer's state and generates import + removed blocks for each. The removed blocks are automatically consolidated into a single `removed { from = module.foo }`.

Module moves:
- Do not support `keys` or `import_id` (import IDs are auto-resolved from state)
- If `to` is provided, it must also be a module address
- Work with `address_prefix` and at any nesting depth (nested sub-modules are included)

#### `all_resources` — Full Layer Move

Move all managed resources from the source layer to the destination layer:

```yaml
- type: move
  description: "Move entire layer"
  source_layer: "./layers/old"
  destination_layer: "./layers/new"
  all_resources: true
```

Data sources are excluded. Module-level consolidation applies automatically to the removed blocks.

##### Overrides

Optional `overrides` entries can rename specific resources during a bulk move:

```yaml
- type: move
  source_layer: "./layers/old"
  destination_layer: "./layers/new"
  all_resources: true
  overrides:
    - from: "aws_instance.web"
      to: "aws_instance.api"   # rename this one; all others keep their address
```

Override constraints:
- `to` or `import_id` (or both) is required
- `keys` is not allowed
- Module addresses cannot be used as overrides
- `address_prefix` cannot be combined with `all_resources`

Use `import_id` on override entries to provide custom import IDs for resources whose auto-resolved `id` attribute doesn't match the provider's expected import format:

```yaml
overrides:
  - from: "azuredevops_serviceendpoint_azurerm.key_vault"
    import_id: "{{ .Attributes.project_id }}/{{ .Attributes.id }}"
```

##### `omit` — Exclude Resources

Exclude specific resources from import during an `all_resources` move:

```yaml
- type: move
  source_layer: "./layers/old"
  destination_layer: "./layers/new"
  all_resources: true
  omit:
    - address: "aws_instance.ephemeral"
    - address: "aws_route.dynamic"
      destroy: true
```

Omitted resources get `removed` blocks in the source layer (with `destroy = false` by default) but no `import` blocks in the destination layer. Set `destroy: true` per entry if the resource should also be destroyed.

`omit` is only valid with `all_resources: true`, and omit addresses cannot overlap with `overrides` addresses.

---

### `rename` — In-Layer Rename

Renames resources or modules within a single layer. Generates `moved` blocks.

**Required fields:** `layer`, `renames` (non-empty list); each entry requires `from` and `to`

```yaml
- type: rename
  description: "Rename VPC module and subnet"
  layer: "./layers/networking"
  address_prefix: "module.vpc"               # optional
  renames:
    - from: "aws_subnet.old"
      to: "aws_subnet.new"
    - from: "aws_route_table.legacy"
      to: "aws_route_table.main"
```

---

### `remove` — Remove from State

Removes resources from state tracking. By default, the underlying infrastructure is preserved (`destroy = false`).

**Required fields:** `layer`, `entries` (non-empty list); each entry requires `address`

```yaml
- type: remove
  description: "Stop managing deprecated IAM resources"
  layer: "./layers/iam"
  destroy: false                             # default; set to true to also destroy
  entries:
    - address: "aws_iam_role.deprecated"
    - address: "aws_iam_policy.old_policy"
      destroy: true                          # per-entry override
```

---

### `import` — Import Existing Resources

Imports existing cloud resources into OpenTofu state. Generates `import` blocks.

**Required fields:** `layer`, `imports` (non-empty list); each entry requires `address` and `id`

```yaml
- type: import
  description: "Import existing databases"
  layer: "./layers/database"
  provider: "aws.useast1"                    # optional: operation-level default
  imports:
    - address: "aws_db_instance.primary"
      id: "my-database-identifier"
    - address: "aws_db_instance.replica"
      id: "my-replica-identifier"
      provider: "aws.uswest2"               # per-entry override
```

---

## Retiring Migration Files

When a migration has been fully applied and is no longer needed, mark it as retired rather than deleting it:

```yaml
description: "Moved compute resources (completed 2024-01)"
status: retired
operations: []
```

Retired files are skipped immediately during processing — no validation, no state reads, no condition evaluation. This is the cheapest way to disable old migration files.

## Validation Rules

When writing YAML, ensure:

1. `description` at the top level is always present
2. `operations` list is non-empty
3. Every operation has a `type` field
4. `move` requires `source_layer`, `destination_layer`, and either non-empty `resources` list or `all_resources: true`
5. Each resource requires `from` (except when using `all_resources` without overrides)
6. `all_resources` is only valid on `move` operations; cannot be combined with `address_prefix`
7. When `all_resources: true`, `overrides` entries require `to` or `import_id` (or both), cannot use `keys`, and cannot use module addresses
8. `omit` is only valid with `all_resources: true`; each entry requires `address`; omit addresses cannot overlap with `overrides` addresses
9. `keys` map entries: `*` only at the end of a pattern (e.g., `"prefix_*"`)
10. `rename` requires `layer` and non-empty `renames` list; each entry requires `from` and `to`
11. `remove` requires `layer` and non-empty `entries` list; each entry requires `address`
12. `import` requires `layer` and non-empty `imports` list; each entry requires `address` and `id`
13. Template expressions (`{{ }}`) are only valid in `keys` map values, `import_id` fields (move), and `id` fields (import)
14. Layer paths are relative to where `tfmigrate generate` is run
15. `status` is optional; if present, must be `"retired"` (unknown values are errors). Retired files skip all validation.

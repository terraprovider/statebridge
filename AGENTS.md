# AGENTS.md — AI Agent Instructions for statebridge

This file provides context for AI coding agents (Claude Code, Copilot, Cursor, etc.) to generate statebridge YAML migration files from natural language descriptions.

## What This Project Does

statebridge is a CLI tool that generates OpenTofu/Terraform HCL migration code (`import`, `moved`, `removed` blocks) from declarative YAML files. Users describe what resources need to move, rename, import, or remove, and the tool produces the correct HCL in each affected layer directory.

A **layer** is a Terraform/OpenTofu root module identified by its filesystem path (e.g., `./layers/networking`).

## Development Guide

### Build & Test Commands

```bash
# Build (static, stripped binary)
just build
# or: GOFLAGS="-trimpath" go build -ldflags "-s -w -extldflags '-static'"

# Lint
golangci-lint run ./...

# Unit tests (no external deps needed)
go test ./...

# E2E fast tests (requires Azure auth + storage account)
go test -tags=e2e_fast -timeout=10m -count=1 ./e2e/...

# E2E full tests (requires Azure auth + OpenTofu + real resources)
go test -tags=e2e -timeout=60m -count=1 ./e2e/...
```

E2E environment: `ARM_CLIENT_ID`, `ARM_TENANT_ID`, `ARM_SUBSCRIPTION_ID`, `ARM_USE_OIDC=true`, optionally `E2E_STORAGE_ACCOUNT_NAME` and `E2E_LOCATION`.

### Code Conventions

- **Go 1.26**, module `github.com/terraprovider/statebridge`
- **Error handling:** Always wrap with context — `fmt.Errorf("context: %w", err)`
- **Validation:** Collect all errors before returning, don't fast-fail (see `pkg/migration/validation.go`)
- **Tests:** Table-driven with `t.Run()` subtests. Use helpers from `internal/testutil/` (no external test frameworks)
- **Interfaces:** Define at consumer site — `StateReader`, `Block`, `BlobUploader`. Keep them small
- **Imports:** 3-group style — stdlib, external, internal
- **Build tags:** `e2e` (full Azure tests), `e2e_fast` (isolated feature tests), none (unit tests)

### Architecture

```
cmd/           → CLI layer (cobra). Calls pkg/engine.ProcessFiles()
pkg/engine/    → Orchestration core. Coordinates parsing → state → template → generation
pkg/migration/ → YAML schema types + validation (no external pkg/ deps)
pkg/state/     → OpenTofu state reading (terraform-exec, caching, auto-init)
pkg/generator/ → HCL block rendering + metadata embedding
pkg/template/  → Go template evaluation + custom functions
pkg/upload/    → Azure Blob Storage operations + overwrite guard
pkg/download/  → Download orchestration with condition evaluation
pkg/conditions/→ Shared condition evaluation (upload guard + download)
pkg/auth/      → Azure credential management (HashiCorp SDK → azcore bridge)
pkg/tofu/      → OpenTofu plan execution + migration target scanning
internal/testutil/ → Test helpers (layer setup, mock state, assertions)
e2e/           → End-to-end tests with real Azure resources
```

Key entry point: `pkg/engine.ProcessFiles()` runs the full pipeline.

---

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
schema_version: "2"  # optional: documents current schema version for forward compatibility
status: retired      # optional: "retired" skips file entirely (no state reads, no processing)
condition:           # optional: skip file if checks fail
  resources_exist:
    - layer: "<layer path>"
      addresses:
        - "<resource address>"
  resources_not_exist:
    - layer: "<layer path>"
      addresses:
        - "<resource address>"
  layer_exists:                    # check directory existence (no state reading)
    - "<layer path>"
  layer_not_exists:
    - "<layer path>"
operations:
  - type: <move|rename|remove|import>
    # ... fields depend on type
```

### File-Level Condition

Optional. Adds explicit conditions that are merged with auto-inferred conditions (see below). Controls whether the migration file is processed at generation time.

- `resources_exist`: ALL addresses must be found in the layer's state
- `resources_not_exist`: NONE of the addresses must be found in the layer's state
- All checks are ANDed
- Base addresses (e.g., `aws_instance.web`) match any for_each instance
- Full addresses (e.g., `aws_instance.web["key"]`) match only that instance

```yaml
condition:
  resources_exist:
    - layer: "./layers/compute"
      addresses:
        - "aws_instance.web"
  resources_not_exist:
    - layer: "./layers/app"
      addresses:
        - "aws_instance.web"
```

### Auto-Inferred Conditions

Conditions are automatically inferred from block types and embedded in generated `.tf` metadata. This makes migrations idempotent by default without requiring explicit `condition:` blocks.

| Block type | Inferred condition | Rationale |
|------------|-------------------|-----------|
| `removed`  | `resources_exist` for `from` | Skip if resource already gone |
| `import`   | `resources_not_exist` for `to` | Skip if already imported |
| `moved`    | `resources_exist` for `from` + `resources_not_exist` for `to` | Skip if rename already done |

Inferred conditions always use layer `"."` (the owning layer). Explicit YAML conditions are merged additively with inferred ones (addresses deduplicated per layer).

### Status Field: `status`

The optional `status` field controls the lifecycle state of a migration file:

- Omitted or empty: active — processed normally
- `"retired"`: skipped entirely — no validation, no state reads, no condition evaluation

Use `status: retired` to disable completed migration files cheaply, without deleting them. This is the recommended way to "archive" old migration files that have already been applied.

```yaml
description: "Moved compute resources (completed 2024-01)"
status: retired
operations: []
```

### Common Field: `address_prefix`

All operation types support an optional `address_prefix` field that is prepended (with a dot `.` separator) to all resource addresses in the operation. Useful for factoring out common module paths:

```yaml
- type: move
  address_prefix: "module.identity_governance"
  resources:
    - from: "azuread_access_package.all"
    # Resolved address: module.identity_governance.azuread_access_package.all
```

### Move-Only Fields: `source_prefix` / `destination_prefix`

Move operations additionally support `source_prefix` and `destination_prefix` for independent control over the address prefix applied to source and destination sides:

```yaml
- type: move
  source_layer: "./layers/old"
  destination_layer: "./layers/new"
  source_prefix: "module.old_root"
  destination_prefix: "module.new_root"
  resources:
    - from: "aws_instance.web"
    # Source address:      module.old_root.aws_instance.web
    # Destination address: module.new_root.aws_instance.web
```

Rules:
- Cannot be combined with `address_prefix` — use one or the other
- If only `source_prefix` is set, destination addresses have no prefix (and vice versa)
- `address_prefix` remains a shorthand that applies the same prefix to both sides
- Only valid on `move` operations; `rename`, `remove`, and `import` reject them
- Cannot be combined with `all_resources: true`

### Operation: `move`

Moves resources between two layers, or renames them within the same layer. When `source_layer` and `destination_layer` differ, generates `removed` in source + `import` in destination. When they are the same (same-layer move), generates `moved` blocks by default (controllable via `use_moved_blocks`).

Required fields: `source_layer`, `destination_layer`, and either `resources` (non-empty list) or `all_resources: true`
Each resource requires: `from`
Optional fields: `description`, `address_prefix`, `source_prefix`, `destination_prefix`, `all_resources`, `use_moved_blocks`, per-resource `to`, `keys`, `import_id`, `merge_duplicates`, `use_moved_blocks`

**Simple move (single or non-for_each resource):**

```yaml
- type: move
  description: "Move web server to app layer"
  source_layer: "./layers/compute"
  destination_layer: "./layers/app"
  resources:
    - from: "aws_instance.web"
      import_id: "i-0abc123def456"  # omit to auto-resolve from source state
```

**Keyed move (for_each resource with key mapping):**

```yaml
- type: move
  source_layer: "./layers/old"
  destination_layer: "./layers/new"
  resources:
    - from: "azuread_access_package_catalog.all"
      keys:
        mrt_customer: customer_approval               # exact key → exact key
        mrt_outbound_provisioning: resource_tenant_access
        mrt_privileged_access: privileged_access
        mrt_vaw: vaw
```

**Key pattern types:**

| Pattern | Meaning | Example |
|---|---|---|
| `exact_key` | Matches exactly that for_each key | `mrt_customer: customer_approval` |
| `prefix_*` | Matches all keys starting with `prefix_` | `"eng_*": '{{ .Key \| trimPrefix "eng_" }}'` |
| `*` | Catch-all: matches all remaining keys | `"*": '{{ .Key }}'` |

Match priority: exact > longest prefix > catch-all.

**Completeness rules:**
- When `keys` is present, ALL state keys must be matched. Unmatched keys cause an error.
- Overlapping key claims across operations cause an error.
- Same source resource can appear in multiple operations with different destination layers.

**Without `keys` map:**
- Single resource: generates one `removed` + one `import`
- For_each resource: expands all instances with same keys

**`merge_duplicates`** — Deduplicate when multiple source resources produce the same destination address:

```yaml
resources:
  - from: "azurerm_role_management_policy.permanent_active"
    to: "azurerm_role_management_policy.all"
    merge_duplicates: true
    keys:
      key_a: shared_key
      key_b: unique_active
  - from: "azurerm_role_management_policy.permanent_eligible"
    to: "azurerm_role_management_policy.all"
    merge_duplicates: true
    keys:
      key_x: shared_key
      key_y: unique_eligible
```

When `merge_duplicates: true`, the first block for a destination address wins and subsequent duplicates are silently skipped. For cross-layer moves, import IDs must match (error if they differ). For same-layer moves, duplicates targeting the same destination are always compatible. Only valid when `keys` is present. Not valid on module moves or `all_resources` overrides. Both resources involved in the collision must have this flag set.

**`to`** — Override when the destination base address differs from source:

```yaml
resources:
  - from: "module.old.resource.all"
    to: "module.new.resource.all"
    keys:
      key1: key1
```

**Module-level move** — specify a module address to move all managed resources under it:

```yaml
resources:
  - from: "module.foo"                           # moves all resources under module.foo
  - from: "module.foo"
    to: "module.bar"                             # remaps module prefix
```

Constraints: `keys` and `import_id` are not allowed on module moves. `to` must also be a module address if specified. Import IDs are auto-resolved from state. Works with `address_prefix` and at any nesting depth (nested sub-modules are included). The removed blocks are automatically consolidated into a single module-level `removed { from = module.foo }`.

**Full-layer move (`all_resources: true`)** — move all managed resources from the source layer:

```yaml
- type: move
  source_layer: "./layers/old"
  destination_layer: "./layers/new"
  all_resources: true
```

Discovers all managed resources from the source layer's state and generates removed + import blocks for each. Data sources are excluded. Module-level consolidation applies automatically.

Optional `overrides` entries alongside `all_resources` serve as destination address overrides (renaming during bulk move):

```yaml
- type: move
  source_layer: "./layers/old"
  destination_layer: "./layers/new"
  all_resources: true
  overrides:
    - from: "aws_instance.web"
      to: "aws_instance.api"                    # rename this one; all others keep their address
```

Override constraints: `to` or `import_id` (or both) is required, `keys` is not allowed, module addresses cannot be used as overrides, and `address_prefix` cannot be combined with `all_resources`. Use `import_id` to override automatic import ID resolution for resources that need composite IDs (e.g., `"{{ .Attributes.project_id }}/{{ .Attributes.id }}"`).

**`omit`** — Exclude resources from import during an `all_resources` move:

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

Omitted resources get `removed` blocks in the source layer (with `destroy = false` by default) but no `import` blocks in the destination layer. Set `destroy: true` per entry to also destroy the resource. `omit` is only valid with `all_resources: true`, and omit addresses cannot overlap with `overrides` addresses.

**Same-layer moves** — When `source_layer` and `destination_layer` are the same, all move sub-types generate `moved` blocks by default instead of `removed` + `import`:

```yaml
- type: move
  source_layer: "./layers/app"
  destination_layer: "./layers/app"
  source_prefix: "module.v1"
  destination_prefix: "module.v2"
  resources:
    - from: "aws_instance.web"
    # Generates: moved { from = module.v1.aws_instance.web; to = module.v2.aws_instance.web }
```

Same-layer behavior:
- Identity moves (where `from` and `to` resolve to the same address) are silently skipped
- `merge_duplicates` is supported — the first `moved` block for a destination wins, subsequent duplicates are skipped
- Module-level same-layer moves generate a single `moved` block for the module
- `all_resources: true` generates a `moved` block per resource instance, skipping identities
- Keyed moves generate `moved` blocks per matched key

**`use_moved_blocks`** — Override same-layer block generation behavior:

By default (`true`), same-layer moves generate `moved` blocks. Set to `false` to force `removed` + `import` block generation even when source and destination layers are the same. Can be set at operation level or per-resource (per-resource overrides operation-level).

```yaml
- type: move
  source_layer: "./layers/app"
  destination_layer: "./layers/app"
  use_moved_blocks: false                    # force removed + import for all resources
  resources:
    - from: "aws_instance.old"
      to: "aws_instance.new"
```

Per-resource override:

```yaml
- type: move
  source_layer: "./layers/app"
  destination_layer: "./layers/app"
  resources:
    - from: "aws_instance.old"
      to: "aws_instance.new"
      use_moved_blocks: false                # this resource gets removed + import
    - from: "aws_instance.other"
      to: "aws_instance.renamed"            # this resource gets moved block (default)
```

Constraints: `use_moved_blocks` is only valid on `move` operations. `use_moved_blocks: false` is not supported on module-level moves.

### Operation: `rename`

Renames resources within a single layer. Generates `moved` blocks.

Required fields: `layer`, `renames` (non-empty list)
Each rename requires: `from`, `to`

```yaml
- type: rename
  description: "Rename VPC module and subnets"
  layer: "./layers/networking"
  address_prefix: "module.vpc"               # optional
  renames:
    - from: "aws_subnet.old"
      to: "aws_subnet.new"
    - from: "aws_route_table.legacy"
      to: "aws_route_table.main"
```

### Operation: `remove`

Removes resources from state. Generates `removed` blocks. Default: `destroy = false` (keeps infrastructure).

Required fields: `layer`, `entries` (non-empty list)
Each entry requires: `address`
Optional fields: `destroy` (default: false), per-entry `destroy` override

```yaml
- type: remove
  description: "Stop managing deprecated resources"
  layer: "./layers/iam"
  destroy: false
  entries:
    - address: "aws_iam_role.deprecated"
    - address: "aws_iam_policy.old_policy"
      destroy: true                              # per-entry override
```

### Operation: `import`

Imports existing resources into state. Generates `import` blocks.

Required fields: `layer`, `imports` (non-empty list)
Each import requires: `address`, `id`
Optional: `provider` at operation level (default for all entries), per-entry `provider` override

```yaml
- type: import
  description: "Import existing databases"
  layer: "./layers/database"
  provider: "aws.useast1"                       # optional: operation-level default
  imports:
    - address: "aws_db_instance.primary"
      id: "my-database-identifier"
    - address: "aws_db_instance.replica"
      id: "my-replica-identifier"
      provider: "aws.uswest2"                   # per-entry override
```

**Source-based imports** — derive import IDs from another resource's state:

Each import entry supports an optional `source` block that enables template-based ID resolution against a source resource in state. When `source` is set, `id` can be a Go template expression evaluated against the source resource's context.

```yaml
- type: import
  layer: "./layers/identity"
  imports:
    - address: "azuread_application_registration.all"
      id: '{{ .Attributes.id }}'
      source:
        layer: "./layers/identity"
        address: "azuread_application.all"
```

Source fields:
- `source.layer` — layer path containing the source resource (required)
- `source.address` — base address to look up in state (required); for for_each resources, all instances are iterated
- `source.expand` — optional attribute name containing a list; each list element produces a separate import, with `.Item` and `.ItemIndex` available in templates

When `source` is set without `expand`: one import per source instance. An optional `key` template overrides the destination for_each key (defaults to the source key).

When `source.expand` is set: `key` is required (must generate unique keys from list elements). Each list element is available as `.Item` in templates.

**Attribute expansion example** — split `required_resource_access` into separate resources:

```yaml
- type: import
  layer: "blueprints/20-launchpad"
  imports:
    - address: "azuread_api_access.all"
      id: '{{ .Attributes.id }}/apiAccess/{{ .Item.resource_app_id }}'
      key: '{{ .Key }}_{{ .Item.resource_app_id }}'
      source:
        layer: "blueprints/20-launchpad"
        address: "azuread_application.all"
        expand: "required_resource_access"
```

This generates one `import` block per `required_resource_access` entry per application instance. For an application with key `"myapp"` and two `required_resource_access` entries, it produces:
- `import { to = azuread_api_access.all["myapp_graph-api-id"] ... }`
- `import { to = azuread_api_access.all["myapp_sharepoint-id"] ... }`

---

## Go Template Reference

Templates can be used in `keys` map values, `import_id` fields, and `id`/`key` fields in source-based imports. They are evaluated once per matched resource instance (or once per expanded list element when `source.expand` is set).

### Context Variables

| Variable | Type | Example | Description |
|----------|------|---------|-------------|
| `.Address` | string | `aws_s3_bucket.data["key-1"]` | Full source resource address |
| `.Type` | string | `aws_s3_bucket` | Resource type |
| `.Name` | string | `data` | Resource name |
| `.Index` | any | `"key-1"` or `0` | Raw for_each key or count index |
| `.Key` | string | `"key-1"` | String form of `.Index` |
| `.Attributes` | map | `{"id": "...", "bucket": "..."}` | All state attributes |
| `.Item` | any | `{"resource_app_id": "..."}` | Current list element (only in `source.expand` context) |
| `.ItemIndex` | int | `0` | 0-based index of `.Item` in the expanded list |

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
- `{{ .Key | split "/" | at 1 }}` — split then index into result (pipe-compatible)

**Testing:**
- `{{ if .Key | hasPrefix "prod" }}...{{ end }}`
- `{{ if .Key | hasSuffix "-prod" }}...{{ end }}`
- `{{ if .Key | contains "special" }}...{{ end }}`

**Nested attribute access:**
- `{{ attr .Attributes "tags" "Name" }}` — traverse nested maps

**Regex:**
- `{{ .Key | regexReplace "[^a-z0-9]+" "_" }}` — regex-based replacement

**Key sanitization:**
- `{{ .Key | sanitizeKey }}` — lowercase + replace non-alphanumeric runs with `_` + trim edges
- `{{ formatKey "%s_%s" .Attributes.pkg .Attributes.role }}` — `printf` + `sanitizeKey` in one step

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
  source_layer: "<layer A path>"
  destination_layer: "<layer B path>"
  resources:
    - from: "<resource address>"
```

If the user provides a specific import ID, add `import_id` to the resource. Otherwise omit it (auto-resolved from state).

### "Move multiple resources between layers"

```yaml
- type: move
  source_layer: "<source>"
  destination_layer: "<destination>"
  address_prefix: "<common module path>"     # if resources share a prefix
  resources:
    - from: "<resource 1>"
    - from: "<resource 2>"
    - from: "<resource 3>"
```

### "Move entire module from layer A to layer B"

```yaml
- type: move
  source_layer: "<layer A path>"
  destination_layer: "<layer B path>"
  resources:
    - from: "module.<module_name>"
    - from: "module.<module_name>"
      to: "module.<new_name>"                    # if renaming the module
```

Module addresses (`module.foo`, `module.foo.module.bar`) are automatically detected. All managed resources under the module are discovered from state and moved. No `keys` or `import_id` needed.

### "Rename X to Y in layer A"

```yaml
- type: rename
  layer: "<layer A path>"
  renames:
    - from: "<old address>"
      to: "<new address>"
```

Works for resources, modules, and for_each key changes.

### "Multiple renames in one layer"

```yaml
- type: rename
  layer: "<layer path>"
  renames:
    - from: "<old 1>"
      to: "<new 1>"
    - from: "<old 2>"
      to: "<new 2>"
```

### "Remove X from layer A" / "Stop managing X"

```yaml
- type: remove
  layer: "<layer A path>"
  entries:
    - address: "<resource address>"
```

If the user says "delete" or "destroy", set `destroy: true` (either at operation level or per-entry). If they say "remove from state" or "stop managing", use the default `destroy: false`.

### "Import X into layer A"

```yaml
- type: import
  layer: "<layer A path>"
  imports:
    - address: "<resource address>"
      id: "<cloud resource ID>"
```

The user must provide the import ID (ARN, resource ID, etc.) or you must ask for it.

### "Import resources derived from another resource's attributes" / "Split resource into sub-resources"

Use source-based imports to derive import IDs from an existing resource's state:

```yaml
- type: import
  layer: "<layer path>"
  imports:
    - address: "<new resource type>.all"
      id: '{{ .Attributes.id }}'
      source:
        layer: "<layer path>"
        address: "<existing resource>.all"
```

When the source resource has a list attribute that should be expanded into multiple new resources:

```yaml
- type: import
  layer: "<layer path>"
  imports:
    - address: "<new resource type>.all"
      id: '{{ .Attributes.id }}/subResource/{{ .Item.<field> }}'
      key: '{{ .Key }}_{{ .Item.<field> }}'
      source:
        layer: "<layer path>"
        address: "<existing resource>.all"
        expand: "<list attribute name>"
```

### "Re-key for_each resources" / "Rename instance keys"

Use a keyed move with exact key mappings:

```yaml
- type: move
  source_layer: "<layer path>"
  destination_layer: "<layer path>"          # can be the same layer
  resources:
    - from: "<resource>.all"
      keys:
        old_key_1: new_key_1
        old_key_2: new_key_2
```

When source and destination layers are the same, generates `moved` blocks by default. Set `use_moved_blocks: false` to force `removed` + `import` instead. When different, always generates `removed` + `import`.

### "Move resources between module paths" / "Change module prefix"

Use `source_prefix` and `destination_prefix` for independent prefix control:

```yaml
- type: move
  source_layer: "<source layer>"
  destination_layer: "<destination layer>"
  source_prefix: "<old module path>"
  destination_prefix: "<new module path>"
  resources:
    - from: "<resource address>"
```

Works with both cross-layer and same-layer moves. For same-layer moves, generates `moved` blocks by default (override with `use_moved_blocks: false`).

### "Rename resources within a layer using move" / "Same-layer move"

When `source_layer` equals `destination_layer`, the move operation generates `moved` blocks by default:

```yaml
- type: move
  source_layer: "<layer path>"
  destination_layer: "<layer path>"
  resources:
    - from: "<old address>"
      to: "<new address>"
```

This is equivalent to a `rename` but supports all move features (keyed moves, prefix remapping, module moves, `all_resources`). Set `use_moved_blocks: false` to force `removed` + `import` generation instead:

```yaml
- type: move
  source_layer: "<layer path>"
  destination_layer: "<layer path>"
  use_moved_blocks: false
  resources:
    - from: "<old address>"
      to: "<new address>"
```

### "Merge multiple source resources into one destination" / "Deduplicate import blocks"

When two source resources produce the same destination address via keyed moves, use `merge_duplicates: true`:

```yaml
- type: move
  source_layer: "<source>"
  destination_layer: "<destination>"
  resources:
    - from: "<resource_type>.<name_a>"
      to: "<resource_type>.<unified_name>"
      merge_duplicates: true
      keys:
        <key_a>: <shared_dest_key>
        <key_b>: <unique_key_b>
    - from: "<resource_type>.<name_b>"
      to: "<resource_type>.<unified_name>"
      merge_duplicates: true
      keys:
        <key_x>: <shared_dest_key>
        <key_y>: <unique_key_y>
```

Both resources must have the flag set. The shared destination key's import block is generated only once (first wins). Import IDs must match for the shared key — if they differ, the tool raises an error.

### "Move all resources from layer A to layer B" / "Move entire layer"

```yaml
- type: move
  source_layer: "<layer A path>"
  destination_layer: "<layer B path>"
  all_resources: true
```

To rename specific resources during the bulk move, add override entries:

```yaml
- type: move
  source_layer: "<layer A path>"
  destination_layer: "<layer B path>"
  all_resources: true
  overrides:
    - from: "<resource to rename>"
      to: "<new address>"
```

### "Move all instances of a for_each resource"

Without a `keys` map, all instances are moved with the same keys:

```yaml
- type: move
  source_layer: "<source>"
  destination_layer: "<destination>"
  resources:
    - from: "<resource_type>.<name>"
```

### "Split resource by key prefix" / "Route different for_each keys to different layers"

Use multiple move operations with prefix patterns:

```yaml
operations:
  - type: move
    source_layer: "<source layer>"
    destination_layer: "<layer A>"
    resources:
      - from: "<resource>.all"
        keys:
          "<prefix_a>_*": '{{ .Key | trimPrefix "<prefix_a>_" }}'
  - type: move
    source_layer: "<source layer>"
    destination_layer: "<layer B>"
    resources:
      - from: "<resource>.all"
        keys:
          "<prefix_b>_*": '{{ .Key | trimPrefix "<prefix_b>_" }}'
```

All keys in the source state must be collectively covered by the operations. The user must provide the prefixes or you must ask for them.

### "Make migration idempotent"

Migrations are idempotent by default thanks to auto-inferred conditions. No explicit `condition` block is needed — the generated files automatically include `resources_exist`/`resources_not_exist` checks derived from the block types.

For cross-layer checks or custom logic, add an explicit `condition` block (merged with inferred):

```yaml
description: "Move web server (with cross-layer check)"
condition:
  resources_exist:
    - layer: "<source layer>"
      addresses:
        - "<resource address>"
  resources_not_exist:
    - layer: "<destination layer>"
      addresses:
        - "<resource address>"
operations:
  - type: move
    source_layer: "<source layer>"
    destination_layer: "<destination layer>"
    resources:
      - from: "<resource address>"
```

### "Skip migration when source layer no longer exists"

```yaml
condition:
  layer_exists:
    - "./layers/source"
```

`layer_exists` and `layer_not_exists` conditions check directory existence without reading state — much cheaper than `resources_exist`. Use `layer_exists` to skip migrations whose source layers have been deleted. Use `layer_not_exists` to skip migrations once a destination has been removed.

```yaml
condition:
  layer_not_exists:
    - "./layers/deprecated"
```

### "Run in CI where backends aren't initialized" / "Auto-init layers"

Use the `--backend-config` CLI flag on `generate`, `upload`, or `download` to pass backend configuration to `tofu init`. This mirrors the `tofu init -backend-config=...` syntax:

```bash
# Generate with backend config (auto-inits layers on state read failure)
statebridge generate --backend-config=bucket=my-state-bucket --backend-config=key=terraform.tfstate migrations/

# Upload with backend config
statebridge upload --backend-config=storage_account_name=myacct ./layers/compute

# Download with backend config
statebridge download --backend-config=storage_account_name=myacct

# Point to a backend config file
statebridge generate --backend-config=backend.hcl migrations/

# Strict mode: treat missing layer directories as errors
statebridge generate --strict migrations/
```

- `--strict` (generate only): Treat missing layer directories as hard errors instead of auto-skipping

---

### "Clean up old migration blobs" / "Remove completed migrations"

```bash
# Dry run: see what would be pruned
statebridge prune --dry-run ./layers/compute ./layers/networking

# Prune completed migrations (evaluates conditions)
statebridge prune ./layers/compute ./layers/networking

# Force delete all migration blobs
statebridge prune --force ./layers/compute
```

The prune command lists migration blobs in Azure Blob Storage, evaluates their embedded conditions, and deletes blobs whose conditions no longer hold (migration completed). Blobs without conditions are kept.

## Validation Rules

When generating YAML, ensure:

1. `description` at the top level is always present
2. `operations` list is non-empty
3. Every operation has a `type` field
4. `move` requires `source_layer`, `destination_layer`, and either non-empty `resources` list or `all_resources: true`
5. Each resource requires `from` (except when using `all_resources` without overrides)
5a. `all_resources` is only valid on `move` operations; cannot be combined with `address_prefix`
5b. When `all_resources: true`, `overrides` entries require `to` or `import_id` (or both), cannot use `keys`, and cannot use module addresses
5c. `omit` is only valid with `all_resources: true`; each entry requires `address`; omit addresses cannot overlap with `overrides` addresses
6. `keys` map entries: `*` only at the end of a pattern (e.g., `"prefix_*"`)
7. `rename` requires `layer` and non-empty `renames` list; each entry requires `from` and `to`
8. `remove` requires `layer` and non-empty `entries` list; each entry requires `address`
9. `import` requires `layer` and non-empty `imports` list; each entry requires `address` and `id`
9a. Import entries may have an optional `source` block: requires `source.layer` (non-empty) and `source.address` (non-empty). When `source.expand` is set, `key` is required. `key` without `source` is invalid.
10. Template expressions (`{{ }}`) are only valid in `keys` map values, `import_id` fields (move), and `id`/`key` fields (import with `source`)
11. Layer paths are relative to where `statebridge generate` is run
12. When `keys` is present, all state keys must be covered (completeness check)
13. A key matching multiple operations is an overlap error
14. `condition` is optional; if present, each resource check requires `layer` (non-empty) and `addresses` (non-empty list of non-empty strings)
15. `resources_exist`: all listed addresses must exist in the specified layer's state for the migration to proceed
16. `resources_not_exist`: none of the listed addresses may exist in the specified layer's state
17. `status` is optional; if present, must be `"retired"` (unknown values are errors). Retired files skip all validation.
18. `condition.layer_exists` and `condition.layer_not_exists` entries must be non-empty strings
19. Non-strict mode (default): migration files referencing non-existent operational layers (`source_layer`, `layer`) are auto-skipped. Strict mode (`--strict`) makes these hard errors.
20. `merge_duplicates` is optional on resource move entries; only valid when `keys` is present. Not valid on module moves or `all_resources` overrides. Both resources involved in a destination collision must have `merge_duplicates: true`.
21. `source_prefix` and `destination_prefix` are only valid on `move` operations; `rename`, `remove`, and `import` reject them
22. `address_prefix` cannot be used together with `source_prefix` or `destination_prefix` on the same operation
23. `source_prefix` / `destination_prefix` cannot be combined with `all_resources: true`
24. Same-layer moves (`source_layer == destination_layer`) generate `moved` blocks by default instead of `removed` + `import`
25. `use_moved_blocks` is optional on `move` operations (both operation-level and per-resource); not valid on `rename`, `remove`, or `import`
26. `use_moved_blocks: false` is not supported on module-level moves (module addresses like `module.foo`)

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

## Output File Naming

Each migration YAML file produces a separate `.tf` file per layer with a content-addressed filename:

```
<layer>/migration.<yaml_stem>.<sha256_8hex>.tf
```

For example, `001_move_web.yaml` → `./layers/app/migration.001_move_web.a1b2c3d4.tf`

Blocks within each output file are sorted deterministically:
1. `removed` blocks first, then `moved`, then `import`
2. Within each type, sorted alphabetically by address

The SHA-256 hash ensures filenames change when content changes but remain stable across identical runs.

## Running After Generation

After creating a migration file, the user runs:

```bash
# Preview without writing
statebridge generate --dry-run migrations/

# Generate the HCL files
statebridge generate migrations/

# Generate with backend config for auto-init
statebridge generate --backend-config=storage_account_name=myacct migrations/

# Generate and upload to Azure Blob Storage in one step
statebridge generate --upload --backend-config=storage_account_name=myacct migrations/
```

### CI Workflow

The full lifecycle for applying migrations in CI:

```bash
# 1. Generate and upload (from repo root, once)
statebridge generate --upload --backend-config=storage_account_name=myacct migrations/

# 2. Per layer: download applicable migrations
cd layers/compute
statebridge download --backend-config=storage_account_name=myacct

# 3. Plan and apply
statebridge plan --out=tfplan --detailed-exitcode
if [ $? -eq 2 ]; then
  tofu apply tfplan
fi
```

### Resilient Multi-File Processing

When processing multiple migration YAML files, statebridge is resilient to individual file failures. If one YAML file fails — for example, because its source resource no longer exists in state after a partial pipeline run — it is skipped with an informational message to stderr, and remaining files continue to be processed:

```
Skipping "migrations/001_move.yaml": operation[0] (move): no resources matching "aws_instance.gone" found in state
```

This allows unrelated migrations to be generated even when some migrations reference resources that have already been moved. Parse errors and YAML validation errors remain fatal. If all files are skipped, the command returns an error.

### Data Source Exclusion

Data sources (`data.*` resources) are automatically excluded from all migration operations. They are auto-computed and never need import or removed blocks. The filtering happens in the resolver (`pkg/engine/resolver.go`), which only returns managed resources from state lookups. If a resource address matches only data sources, the resolver returns an error indicating no managed resources were found.

### Module-Level Consolidation

When all managed resources within a module are being moved out, individual `removed` blocks are automatically consolidated into a single module-level removal block (e.g., `removed { from = module.foo }`). This works at any nesting depth — deepest modules are consolidated first, then the algorithm checks if parent modules can also be consolidated.

The consolidation logic lives in `pkg/engine/consolidate.go` and runs as a post-processing step in `ProcessFiles`, after all blocks are generated but before they are written. Data sources in the module state are ignored (they don't prevent consolidation).

For condition handling, `ResourceExists` in `pkg/state/types.go` was extended to support module addresses: `ResourceExists(s, "module.foo")` returns true if any resource under `module.foo` exists in state. This ensures download-time condition evaluation works correctly with module-level removed blocks.

## Uploading Migrations to Azure Blob Storage

Generated migration files can be persisted to Azure Blob Storage using either the `--upload` flag on `generate` or the standalone `upload` command.

### Generate and Upload

```bash
statebridge generate --upload --backend-config=storage_account_name=myacct migrations/
```

Runs the full pipeline, writes files to disk, then uploads each generated `.tf` file to `migrations/<filename>` in the Azure Blob Storage container configured in that layer's backend. Cannot be combined with `--dry-run`. The `--backend-config` flag is used both for auto-init during state reads and for backend config discovery during upload.

### Standalone Upload

```bash
statebridge upload [layer-dirs...] [flags]
```

Uploads pre-generated `migration.*.tf` files from layer directories. Useful when generation and upload are separate CI steps.

**Flags:**

| Flag | Description |
|------|-------------|
| `--backend-config` | Backend configuration passed to tofu init, as `key=value` or path to a file (repeatable) |
| `--force` | Force upload even if existing migrations are still active (overwrite protection bypass) |
| `--tofu-path <path>` | Override path to the tofu binary (default: auto-detect from PATH) |

**Examples:**

```bash
# Upload from specific layers
statebridge upload ./layers/compute ./layers/networking

# Override backend config
statebridge upload --backend-config=storage_account_name=myacct ./layers/compute

# Use a backend config file
statebridge upload --backend-config=backend.hcl ./layers/compute
```

### Backend Configuration Discovery

The upload target (storage account + container) is resolved per layer:

1. Parse `.tf` files in the layer directory for `terraform { backend "azurerm" { ... } }`
2. Extract `key=value` pairs from `--backend-config` CLI flags. File paths are also supported: `--backend-config=path/to/file.hcl` reads key=value pairs from the file (HCL or plain text format)
3. Merge: `--backend-config` overrides inline HCL values

Required fields: `storage_account_name`, `container_name`.

### Version Cleanup

Before uploading, existing blobs matching `migrations/migration.<yaml_stem>.*.tf` are checked. Old versions (different content hash) are deleted automatically. Messages are printed to stderr:

```
Removed old version: migrations/migration.001_move.oldold00.tf
Uploaded: migrations/migration.001_move.newnew99.tf
```

### Authentication

Uses `pkg/auth` for Azure credentials via environment variables:
- `ARM_CLIENT_ID`, `ARM_TENANT_ID`, `ARM_CLIENT_SECRET` (service principal)
- `ARM_USE_CLI` (Azure CLI)
- `ARM_USE_MSI` (managed identity)
- `ARM_USE_OIDC` (OIDC federation — GitHub Actions, ADO Pipeline, generic)
- `ARM_OIDC_TOKEN` (direct OIDC assertion token)
- `ARM_OIDC_REQUEST_URL` / `ACTIONS_ID_TOKEN_REQUEST_URL` (OIDC token request URL)
- `ARM_OIDC_REQUEST_TOKEN` / `ACTIONS_ID_TOKEN_REQUEST_TOKEN` (OIDC request auth token)

### Upload Guard (Overwrite Protection)

When uploading, statebridge checks whether existing migration blobs are still "active" (their metadata conditions still pass against the layer's state). If an existing blob is still needed — for example, because a cross-layer migration was only partially applied — the upload is refused:

```
Error: refusing to overwrite "migrations/migration.001_move.a1b2c3d4.tf": migration is still active in layer "./layers/app" (conditions pass); use --force to override
```

This protects against a common CI failure mode: a pipeline partially applies migrations across layers (e.g., L10 applied, L30 fails, L50 pending), then re-runs `generate --upload` which would otherwise overwrite the still-needed import blocks.

The guard requires the `tofu` binary to read layer state. Since `tofu` is a hard requirement for all commands, the guard is always active. Use `--force` to explicitly bypass the guard when intentional overwrite is needed.

The guard logic lives in `pkg/upload/upload.go` (`checkActiveBlobs` method) and uses shared condition evaluation from `pkg/conditions/evaluate.go`.

### Auto-Pruning Stale Blobs

When using `--upload`, migration files that are retired (`status: retired`) or auto-skipped due to missing layers have their previously-uploaded blobs automatically pruned from blob storage. This keeps storage clean without requiring manual `statebridge prune` for the common case.

Only blobs in layers that are being actively uploaded to are auto-pruned. For fully orphaned layers (no active migrations target them), use `statebridge prune` manually.

## Downloading Migrations from Azure Blob Storage

```bash
statebridge download [flags]
```

Downloads applicable migration files from the layer's blob container to the current working directory. Operates per-layer (must run from a layer directory).

**Flags:**

| Flag | Description |
|------|-------------|
| `--backend-config` | Backend configuration passed to tofu init, as `key=value` or path to a file (repeatable) |
| `--tofu-path` | Override tofu binary path |
| `--dry-run` | Print what would be downloaded without writing |

**Flow:**

1. Discovers backend config from `.tf` files in cwd (+ `--backend-config` overrides)
2. Lists `migrations/migration.*.tf` blobs in the layer's container
3. Cleans up all existing `migration.*.tf` files in the target directory (blob storage is source of truth)
4. Downloads each blob and parses embedded metadata
5. Evaluates auto-inferred + explicit conditions: for `layer == "."`, reads state and checks; for cross-layer conditions, warns and treats as met
6. Writes only applicable files; skipped files print a message to stderr

## Plan Command

```bash
statebridge plan [flags]
```

Run targeted `tofu plan` scoped to resources in migration file metadata. Must run from a layer directory containing `migration.*.tf` files.

**Flags:**

| Flag | Description |
|------|-------------|
| `--no-target` | Run without `-target` flags (full plan) |
| `--tofu-path` | Override tofu binary path |
| `--detailed-exitcode` | Return exit code 2 when plan has changes |
| `--out <path>` | Save the plan to a file |
| `--var <key=value>` | Set a variable (repeatable) |
| `--var-file <path>` | Variable file path (repeatable) |
| `--lock` | Lock the state file (default: true) |
| `--lock-timeout <duration>` | Duration to retry a state lock |

**Flow:**

1. Scans cwd for `migration.*.tf` files
2. Parses metadata from each to extract resource addresses
3. If no migration files found, prints message and exits with code 0
4. Runs `tofu plan` with `-target=<addr>` for each address (via terraform-exec)
5. Returns tofu's exit code (0 = no changes, 2 with `--detailed-exitcode` = changes detected)

To apply changes, save the plan and use tofu directly:

```bash
statebridge plan --out=tfplan --detailed-exitcode
if [ $? -eq 2 ]; then
  tofu apply tfplan
fi
```

## Migration Metadata

Generated `.tf` files include a structured JSON metadata block as comments:

```hcl
# Generated by statebridge - do not edit manually
#
# statebridge:metadata:begin
# {"conditions":{"resources_exist":[{"layer":".","addresses":["azurerm_vm.web"]}]},"resources":["azurerm_vm.web","azurerm_vnet.main"]}
# statebridge:metadata:end

import {
  ...
}
```

**Fields:**
- `conditions` — auto-inferred from block types (`resources_exist` for removed/moved, `resources_not_exist` for import/moved), merged with any explicit YAML conditions. The owning layer is represented as `"."` for portability.
- `resources` — all resource addresses touched by blocks in the file (used for `-target` flags)

The metadata is produced by the generator during `ProcessFiles()` and embedded by the `Writer` at render time. The `download` and `plan` commands parse it using `generator.ParseMetadataComment()`.

`ProcessFiles` returns `*ProcessResult` containing:
- `OutputFiles []string` — paths of generated `.tf` files written to disk
- `SkippedFiles []SkippedFile` — files that were skipped, each with `Stem` (the YAML file stem) and `Reason` (why it was skipped, e.g., retired status, missing layer)

## E2E Tests

End-to-end tests live in `e2e/` and exercise the full migration pipeline against real Azure resources. They are gated behind the `e2e` build tag and skipped when `ARM_SUBSCRIPTION_ID` is not set.

**Running locally:**
```bash
ARM_CLIENT_ID=... ARM_TENANT_ID=... ARM_SUBSCRIPTION_ID=... ARM_USE_OIDC=true \
  go test -tags=e2e -v -timeout=30m -count=1 ./e2e/...
```

**CI:** Runs via `.github/workflows/e2e.yml` (separate from unit tests). Triggered by `workflow_dispatch` or PRs touching `pkg/`, `cmd/`, `e2e/`, or `go.mod`. Requires GitHub environment `e2e` with OIDC secrets.

**Structure:**
- `e2e/testproject/layers/` — Static Terraform project with 3 layers (shared, app, networking)
- `e2e/helpers_test.go` — Test helpers (tofu init/apply/plan/destroy via terraform-exec, engine.ProcessFiles wrapper, blob container lifecycle)
- `e2e/e2e_test.go` — Test functions covering move, keyed move, rename, remove+import, condition skip, upload/download

**Environment variables:**
- `ARM_CLIENT_ID`, `ARM_TENANT_ID`, `ARM_SUBSCRIPTION_ID` — Azure auth
- `ARM_USE_OIDC=true` — OIDC authentication (GitHub Actions / ADO Pipeline)
- `E2E_LOCATION` — Azure region (default: `westeurope`)
- `E2E_STORAGE_ACCOUNT_NAME` — Pre-existing storage account for upload/download tests (service principal needs "Storage Blob Data Contributor" role). If not set, `TestE2E_UploadDownload` is skipped.

**Test isolation:** Each test generates a unique resource prefix (`tfe2e` + 4 random hex chars) and uses `t.Cleanup()` to destroy all resources even on failure. Local backend — no shared state. The upload/download test creates an ephemeral blob container per run (named after the unique prefix) and deletes it on cleanup.

## Project Structure

Key source files for understanding the codebase:

- `pkg/migration/schema.go` — YAML types and operation definitions
- `pkg/migration/validation.go` — validation rules
- `pkg/template/funcs.go` — available template functions
- `pkg/template/template.go` — template evaluation logic
- `pkg/engine/engine.go` — orchestration pipeline (with metadata wiring)
- `pkg/engine/consolidate.go` — module-level removed block consolidation
- `pkg/engine/keymatcher.go` — key pattern matching (exact, prefix, catch-all)
- `pkg/engine/resolver.go` — import ID resolution and state lookups
- `pkg/engine/tracker.go` — cross-operation key tracking and completeness checking
- `pkg/generator/` — HCL block rendering (import, moved, removed)
- `pkg/generator/metadata.go` — migration metadata type, render/parse, address extraction, condition inference/merge
- `pkg/generator/writer.go` — file output with metadata embedding and condition inference
- `pkg/state/reader.go` — OpenTofu state reading (TofuStateReader with built-in caching and auto-init)
- `pkg/auth/` — Azure credential management (HashiCorp go-azure-sdk wrapper for azcore.TokenCredential)
- `pkg/auth/credential.go` — TokenCredential wrapper bridging HashiCorp SDK auth to azcore
- `pkg/upload/backend.go` — backend config discovery (HCL parsing + init arg merging)
- `pkg/upload/uploader.go` — Azure Blob Storage operations (BlobUploader interface)
- `pkg/upload/upload.go` — upload orchestration (Manager, version cleanup, overwrite protection guard)
- `pkg/conditions/evaluate.go` — shared condition evaluation for upload guard and download
- `pkg/download/download.go` — download orchestration with condition evaluation
- `pkg/tofu/runner.go` — OpenTofu plan execution (via terraform-exec) and migration target scanning
- `cmd/generate.go` — CLI entry point for generate command (with `--upload` flag)
- `cmd/upload.go` — CLI entry point for standalone upload command
- `cmd/download.go` — CLI entry point for download command
- `cmd/plan.go` — CLI entry point for targeted plan command
- `e2e/e2e_test.go` — E2E test functions (build tag: e2e)
- `e2e/helpers_test.go` — E2E test helpers (terraform-exec wrappers, engine runner)
- `e2e/fast_e2e_same_layer_test.go` — Fast E2E tests for same-layer moves and source/destination prefix (build tag: e2e_fast)
- `e2e/testproject/` — Static Terraform project with 3 layers for e2e tests

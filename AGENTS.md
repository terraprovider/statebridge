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
schema_version: "2"  # optional
condition:           # optional: skip file if checks fail
  resources_exist:
    - layer: "<layer path>"
      addresses:
        - "<resource address>"
  resources_not_exist:
    - layer: "<layer path>"
      addresses:
        - "<resource address>"
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

### Common Field: `address_prefix`

All operation types support an optional `address_prefix` field that is prepended (with a dot `.` separator) to all resource addresses in the operation. Useful for factoring out common module paths:

```yaml
- type: move
  address_prefix: "module.identity_governance"
  resources:
    - address: "azuread_access_package.all"
    # Resolved address: module.identity_governance.azuread_access_package.all
```

### Operation: `move`

Moves resources between two layers. Generates `removed` in source + `import` in destination.

Required fields: `source_layer`, `destination_layer`, `resources` (non-empty list)
Each resource requires: `address`
Optional fields: `description`, `address_prefix`, per-resource `destination_address`, `keys`, `import_id`

**Simple move (single or non-for_each resource):**

```yaml
- type: move
  description: "Move web server to app layer"
  source_layer: "./layers/compute"
  destination_layer: "./layers/app"
  resources:
    - address: "aws_instance.web"
      import_id: "i-0abc123def456"  # omit to auto-resolve from source state
```

**Keyed move (for_each resource with key mapping):**

```yaml
- type: move
  source_layer: "./layers/old"
  destination_layer: "./layers/new"
  resources:
    - address: "azuread_access_package_catalog.all"
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

**`destination_address`** — Override when the destination base address differs from source:

```yaml
resources:
  - address: "module.old.resource.all"
    destination_address: "module.new.resource.all"
    keys:
      key1: key1
```

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

Required fields: `layer`, `addresses` (non-empty list)
Optional fields: `destroy` (default: false)

```yaml
- type: remove
  description: "Stop managing deprecated resources"
  layer: "./layers/iam"
  destroy: false
  addresses:
    - "aws_iam_role.deprecated"
    - "aws_iam_policy.old_policy"
```

### Operation: `import`

Imports existing resources into state. Generates `import` blocks.

Required fields: `layer`, `imports` (non-empty list)
Each import requires: `address`, `import_id`
Optional per-import: `provider`

```yaml
- type: import
  description: "Import existing databases"
  layer: "./layers/database"
  imports:
    - address: "aws_db_instance.primary"
      import_id: "my-database-identifier"
      provider: "aws.useast1"
    - address: "aws_db_instance.replica"
      import_id: "my-replica-identifier"
```

---

## Go Template Reference

Templates can be used in `keys` map values and `import_id` fields. They are evaluated once per matched resource instance.

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
    - address: "<resource address>"
```

If the user provides a specific import ID, add `import_id` to the resource. Otherwise omit it (auto-resolved from state).

### "Move multiple resources between layers"

```yaml
- type: move
  source_layer: "<source>"
  destination_layer: "<destination>"
  address_prefix: "<common module path>"     # if resources share a prefix
  resources:
    - address: "<resource 1>"
    - address: "<resource 2>"
    - address: "<resource 3>"
```

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
  addresses:
    - "<resource address>"
```

If the user says "delete" or "destroy", set `destroy: true`. If they say "remove from state" or "stop managing", use the default `destroy: false`.

### "Import X into layer A"

```yaml
- type: import
  layer: "<layer A path>"
  imports:
    - address: "<resource address>"
      import_id: "<cloud resource ID>"
```

The user must provide the import ID (ARN, resource ID, etc.) or you must ask for it.

### "Re-key for_each resources" / "Rename instance keys"

Use a keyed move with exact key mappings:

```yaml
- type: move
  source_layer: "<layer path>"
  destination_layer: "<layer path>"          # can be the same layer
  resources:
    - address: "<resource>.all"
      keys:
        old_key_1: new_key_1
        old_key_2: new_key_2
```

### "Move all instances of a for_each resource"

Without a `keys` map, all instances are moved with the same keys:

```yaml
- type: move
  source_layer: "<source>"
  destination_layer: "<destination>"
  resources:
    - address: "<resource_type>.<name>"
```

### "Split resource by key prefix" / "Route different for_each keys to different layers"

Use multiple move operations with prefix patterns:

```yaml
operations:
  - type: move
    source_layer: "<source layer>"
    destination_layer: "<layer A>"
    resources:
      - address: "<resource>.all"
        keys:
          "<prefix_a>_*": '{{ .Key | trimPrefix "<prefix_a>_" }}'
  - type: move
    source_layer: "<source layer>"
    destination_layer: "<layer B>"
    resources:
      - address: "<resource>.all"
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
      - address: "<resource address>"
```

### "Run in CI where backends aren't initialized" / "Auto-init layers"

Use the `--backend-config` CLI flag on `generate`, `upload`, or `download` to pass backend configuration to `tofu init`. This mirrors the `tofu init -backend-config=...` syntax:

```bash
# Generate with backend config (auto-inits layers on state read failure)
tfmigrate generate --backend-config=bucket=my-state-bucket --backend-config=key=terraform.tfstate migrations/

# Upload with backend config
tfmigrate upload --backend-config=storage_account_name=myacct ./layers/compute

# Download with backend config
tfmigrate download --backend-config=storage_account_name=myacct

# Point to a backend config file
tfmigrate generate --backend-config=backend.hcl migrations/
```

---

## Validation Rules

When generating YAML, ensure:

1. `description` at the top level is always present
2. `operations` list is non-empty
3. Every operation has a `type` field
4. `move` requires `source_layer`, `destination_layer`, and non-empty `resources` list
5. Each resource requires `address`
6. `keys` map entries: `*` only at the end of a pattern (e.g., `"prefix_*"`)
7. `rename` requires `layer` and non-empty `renames` list; each entry requires `from` and `to`
8. `remove` requires `layer` and non-empty `addresses` list
9. `import` requires `layer` and non-empty `imports` list; each entry requires `address` and `import_id`
10. Template expressions (`{{ }}`) are only valid in `keys` map values and `import_id` fields
11. Layer paths are relative to where `tfmigrate generate` is run
12. When `keys` is present, all state keys must be covered (completeness check)
13. A key matching multiple operations is an overlap error
14. `condition` is optional; if present, each resource check requires `layer` (non-empty) and `addresses` (non-empty list of non-empty strings)
15. `resources_exist`: all listed addresses must exist in the specified layer's state for the migration to proceed
16. `resources_not_exist`: none of the listed addresses may exist in the specified layer's state

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
tfmigrate generate --dry-run migrations/

# Generate the HCL files
tfmigrate generate migrations/

# Generate with backend config for auto-init
tfmigrate generate --backend-config=storage_account_name=myacct migrations/

# Generate and upload to Azure Blob Storage in one step
tfmigrate generate --upload --backend-config=storage_account_name=myacct migrations/
```

### CI Workflow

The full lifecycle for applying migrations in CI:

```bash
# 1. Generate and upload (from repo root, once)
tfmigrate generate --upload --backend-config=storage_account_name=myacct migrations/

# 2. Per layer: download applicable migrations
cd layers/compute
tfmigrate download --backend-config=storage_account_name=myacct

# 3. Plan to verify (targeted to migration resources by default)
tfmigrate plan

# 4. Apply
tfmigrate apply
```

## Uploading Migrations to Azure Blob Storage

Generated migration files can be persisted to Azure Blob Storage using either the `--upload` flag on `generate` or the standalone `upload` command.

### Generate and Upload

```bash
tfmigrate generate --upload --backend-config=storage_account_name=myacct migrations/
```

Runs the full pipeline, writes files to disk, then uploads each generated `.tf` file to `migrations/<filename>` in the Azure Blob Storage container configured in that layer's backend. Cannot be combined with `--dry-run`. The `--backend-config` flag is used both for auto-init during state reads and for backend config discovery during upload.

### Standalone Upload

```bash
tfmigrate upload [layer-dirs...] [flags]
```

Uploads pre-generated `migration.*.tf` files from layer directories. Useful when generation and upload are separate CI steps.

**Flags:**

| Flag | Description |
|------|-------------|
| `--backend-config` | Backend configuration passed to tofu init, as `key=value` or path to a file (repeatable) |

**Examples:**

```bash
# Upload from specific layers
tfmigrate upload ./layers/compute ./layers/networking

# Override backend config
tfmigrate upload --backend-config=storage_account_name=myacct ./layers/compute

# Use a backend config file
tfmigrate upload --backend-config=backend.hcl ./layers/compute
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

## Downloading Migrations from Azure Blob Storage

```bash
tfmigrate download [flags]
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

## Plan and Apply Commands

```bash
tfmigrate plan [flags] [-- extra-tofu-args...]
tfmigrate apply [flags] [-- extra-tofu-args...]
```

Run targeted `tofu plan` or `tofu apply -auto-approve` scoped to resources in migration file metadata. Must run from a layer directory containing `migration.*.tf` files.

**Flags (both commands):**

| Flag | Description |
|------|-------------|
| `--no-target` | Run without `-target` flags (full plan/apply) |
| `--tofu-path` | Override tofu binary path |

**Flow:**

1. Scans cwd for `migration.*.tf` files
2. Parses metadata from each to extract resource addresses
3. Runs `tofu plan` or `tofu apply -auto-approve` with `-target=<addr>` for each address
4. Extra args after `--` are passed through to tofu

## Migration Metadata

Generated `.tf` files include a structured JSON metadata block as comments:

```hcl
# Generated by tfmigrate - do not edit manually
#
# tfmigrate:metadata:begin
# {"conditions":{"resources_exist":[{"layer":".","addresses":["azurerm_vm.web"]}]},"resources":["azurerm_vm.web","azurerm_vnet.main"]}
# tfmigrate:metadata:end

import {
  ...
}
```

**Fields:**
- `conditions` — auto-inferred from block types (`resources_exist` for removed/moved, `resources_not_exist` for import/moved), merged with any explicit YAML conditions. The owning layer is represented as `"."` for portability.
- `resources` — all resource addresses touched by blocks in the file (used for `-target` flags)

The metadata is produced by the generator during `ProcessFiles()` and embedded by the `Writer` at render time. The `download`, `plan`, and `apply` commands parse it using `generator.ParseMetadataComment()`.

## Project Structure

Key source files for understanding the codebase:

- `pkg/migration/schema.go` — YAML types and operation definitions
- `pkg/migration/validation.go` — validation rules
- `pkg/template/funcs.go` — available template functions
- `pkg/template/template.go` — template evaluation logic
- `pkg/engine/engine.go` — orchestration pipeline (with metadata wiring)
- `pkg/engine/keymatcher.go` — key pattern matching (exact, prefix, catch-all)
- `pkg/engine/resolver.go` — import ID resolution and state lookups
- `pkg/engine/tracker.go` — cross-operation key tracking and completeness checking
- `pkg/generator/` — HCL block rendering (import, moved, removed)
- `pkg/generator/metadata.go` — migration metadata type, render/parse, address extraction
- `pkg/generator/writer.go` — file output with metadata embedding
- `pkg/auth/` — Azure credential management (azcore, azidentity, hashicorp graph)
- `pkg/upload/backend.go` — backend config discovery (HCL parsing + init arg merging)
- `pkg/upload/uploader.go` — Azure Blob Storage operations (BlobUploader interface)
- `pkg/upload/upload.go` — upload orchestration (Manager, version cleanup)
- `pkg/download/download.go` — download orchestration with condition evaluation
- `pkg/tofu/runner.go` — OpenTofu command execution and migration target scanning
- `cmd/generate.go` — CLI entry point for generate command (with `--upload` flag)
- `cmd/upload.go` — CLI entry point for standalone upload command
- `cmd/download.go` — CLI entry point for download command
- `cmd/plan.go` — CLI entry point for targeted plan command
- `cmd/apply.go` — CLI entry point for targeted apply command

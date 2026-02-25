# tfmigrate

A declarative code generator for OpenTofu state migrations. Define resource moves, renames, imports, and removals in YAML, and tfmigrate generates the corresponding HCL code (`import`, `moved`, `removed` blocks) in your layer directories.

## Installation

```bash
go install github.com/redtenant/tfmigrate@latest
```

Or build from source:

```bash
git clone https://github.com/redtenant/tfmigrate.git
cd tfmigrate
go build -o tfmigrate .
```

## Quick Start

1. Create a migration YAML file:

```yaml
# migrations/001_move_web_server.yaml
description: "Move web server from shared compute to dedicated app layer"
operations:
  - type: move
    source_layer: "./layers/compute"
    destination_layer: "./layers/app"
    resources:
      - address: "aws_instance.web"
        import_id: "i-0abc123def456"
```

2. Generate the HCL:

```bash
tfmigrate generate migrations/001_move_web_server.yaml
```

3. This produces two files (one per layer, named with a content hash):

**`./layers/compute/migration.001_move.a1b2c3d4.tf`**
```hcl
removed {
  from = aws_instance.web

  lifecycle {
    destroy = false
  }
}
```

**`./layers/app/migration.001_move.e5f6a7b8.tf`**
```hcl
import {
  to = aws_instance.web
  id = "i-0abc123def456"
}
```

4. Run `tofu plan` in each layer to verify, then `tofu apply`.

## CLI Usage

```
tfmigrate generate [migration-files-or-dirs...] [flags]
```

### Arguments

Pass one or more file paths or directories. Directories are scanned for `.yaml`/`.yml` files and processed in sorted filename order.

```bash
# Single file
tfmigrate generate migrations/001_move.yaml

# Entire directory (files sorted by name)
tfmigrate generate migrations/

# Mix of files and directories
tfmigrate generate migrations/001_move.yaml other_migrations/
```

### Flags

| Flag | Description |
|------|-------------|
| `--dry-run` | Print generated HCL to stdout without writing files |
| `--tofu-path <path>` | Override path to the `tofu` binary (default: auto-detect from PATH) |
| `--upload` | Upload generated files to Azure Blob Storage after generation |
| `--backend-config` | Backend configuration passed to tofu init, as `key=value` or path to a file (repeatable) |
| `--force` | Force upload even if existing migrations are still active (overwrite protection bypass; only relevant with `--upload`) |

### Dry Run

Preview what would be generated without writing any files:

```bash
tfmigrate generate --dry-run migrations/
```

### Upload to Azure Blob Storage

Generated migration files can be uploaded to Azure Blob Storage, using the backend configuration from each layer's Terraform files and/or `--backend-config` flags.

#### Generate and upload in one step

```bash
tfmigrate generate --upload --backend-config=storage_account_name=myacct migrations/
```

This runs the full generation pipeline and then uploads each generated `.tf` file to the `migrations/` directory in the Azure Blob Storage container configured in that layer's backend. The `--upload` flag cannot be combined with `--dry-run`. The `--backend-config` flag is used both for auto-init during state reads and for backend config discovery during upload.

#### Upload pre-generated files

Use the standalone `upload` command to upload migration files that were already generated to disk:

```bash
tfmigrate upload ./layers/compute ./layers/networking
```

Each layer directory is scanned for `migration.*.tf` files and uploaded to the storage container discovered from the layer's backend configuration.

##### Upload Flags

| Flag | Description |
|------|-------------|
| `--backend-config` | Backend configuration passed to tofu init, as `key=value` or path to a file (repeatable) |
| `--force` | Force upload even if existing migrations are still active (overwrite protection bypass) |
| `--tofu-path <path>` | Override path to the `tofu` binary for upload guard state evaluation (default: auto-detect from PATH) |

```bash
# Override backend config values
tfmigrate upload --backend-config=storage_account_name=myacct ./layers/compute

# Use a backend config file
tfmigrate upload --backend-config=backend.hcl ./layers/compute
```

#### Backend Configuration Discovery

The upload target (storage account and container) is resolved per layer by:

1. Parsing `.tf` files in the layer directory for a `backend "azurerm"` block
2. Extracting `key=value` pairs from `--backend-config` CLI flags. File paths are also supported: `--backend-config=path/to/file.hcl` reads key=value pairs from the file (HCL or plain text format)
3. Merging: `--backend-config` flags override inline HCL values

Required backend fields: `storage_account_name`, `container_name`.

#### Version Cleanup

Before uploading, the tool checks for existing blobs matching the same migration stem (e.g., `migrations/migration.001_move.*.tf`). Old versions with a different content hash are automatically deleted. A message is printed to stderr for each removal:

```
Removed old version: migrations/migration.001_move.oldold00.tf
Uploaded: migrations/migration.001_move.newnew99.tf
```

The storage account is expected to have blob versioning enabled, so deleted versions remain recoverable through Azure's versioning.

#### Upload Guard (Overwrite Protection)

When uploading, tfmigrate checks whether existing migration blobs are still "active" (their metadata conditions still pass against the layer's state). If an existing blob is still needed — for example, because a cross-layer migration was only partially applied — the upload is refused to prevent overwriting it:

```
Error: refusing to overwrite "migrations/migration.001_move.a1b2c3d4.tf": migration is still active in layer "./layers/app" (conditions pass); use --force to override
```

This protects against a common CI failure mode: a pipeline partially applies migrations across layers (e.g., L10 applied, L30 fails, L50 pending), then re-runs `generate --upload` which would otherwise overwrite the still-needed import blocks.

The guard requires the `tofu` binary to read layer state. If `tofu` is not available, the guard is silently disabled and upload proceeds without protection. Use `--force` to explicitly bypass the guard when intentional overwrite is needed.

#### Authentication

Upload and download commands authenticate using Azure SDK credentials configured through environment variables. The following are supported via the `pkg/auth` package:

- `ARM_CLIENT_ID`, `ARM_TENANT_ID`, `ARM_CLIENT_SECRET` (service principal)
- `ARM_USE_CLI` (Azure CLI credential)
- `ARM_USE_MSI` (managed identity)
- `ARM_USE_OIDC` (OIDC federation — GitHub Actions, ADO Pipeline, generic)
- `ARM_OIDC_TOKEN` (direct OIDC assertion token)
- `ARM_OIDC_REQUEST_URL` / `ACTIONS_ID_TOKEN_REQUEST_URL` (OIDC token request URL)
- `ARM_OIDC_REQUEST_TOKEN` / `ACTIONS_ID_TOKEN_REQUEST_TOKEN` (OIDC request auth token)

### Download from Azure Blob Storage

Download applicable migration files from the layer's blob storage container to the current directory. Conditions embedded in migration metadata are evaluated against the layer's state, and only migrations that need to be applied are written.

```bash
cd layers/compute && tfmigrate download
```

This command must be run from within a layer directory containing backend configuration.

#### Download Flags

| Flag | Description |
|------|-------------|
| `--backend-config` | Backend configuration passed to tofu init, as `key=value` or path to a file (repeatable) |
| `--tofu-path <path>` | Override path to the `tofu` binary |
| `--dry-run` | Print what would be downloaded without writing files |

```bash
# Override backend config
tfmigrate download --backend-config=storage_account_name=myacct

# Use a backend config file
tfmigrate download --backend-config=backend.hcl

# Preview what would be downloaded
tfmigrate download --dry-run
```

#### Download Flow

1. Discovers backend config from `.tf` files in the current directory (+ `--backend-config` overrides)
2. Lists all `migrations/migration.*.tf` blobs in the layer's storage container
3. For each migration:
   - Downloads the blob content
   - Parses embedded metadata (conditions and resource addresses)
   - Initializes the backend if `--backend-config` was provided
   - Evaluates conditions against the layer's current state
   - Writes the file to the current directory only if conditions are met
4. Skipped migrations print an informational message to stderr

### Plan

Run targeted `tofu plan` scoped to resources in downloaded migration files.

```bash
# Targeted plan (default — only resources in migration metadata)
tfmigrate plan

# Save plan and detect changes via exit code
tfmigrate plan --out=tfplan --detailed-exitcode

# Full plan without targeting
tfmigrate plan --no-target

# Pass variables
tfmigrate plan --var="env=prod" --var-file=prod.tfvars

# Disable state locking
tfmigrate plan --lock=false
```

#### Plan Flags

| Flag | Description |
|------|-------------|
| `--no-target` | Run without `-target` flags (full plan) |
| `--tofu-path <path>` | Override path to the `tofu` binary |
| `--detailed-exitcode` | Return exit code 2 when plan has changes |
| `--out <path>` | Save the plan to a file |
| `--var <key=value>` | Set a variable (repeatable) |
| `--var-file <path>` | Variable file path (repeatable) |
| `--lock` | Lock the state file (default: true) |
| `--lock-timeout <duration>` | Duration to retry a state lock (e.g. 30s) |

By default, resource addresses are extracted from migration file metadata and passed as `-target` flags. This limits the blast radius to only resources touched by the migrations.

If no migration files are found, the command prints a message and exits with code 0.

To apply changes, save the plan and use tofu directly:

```bash
tfmigrate plan --out=tfplan --detailed-exitcode
# exit 0 = no changes, exit 2 = changes detected
if [ $? -eq 2 ]; then
  tofu apply tfplan
fi
```

### CI Workflow

The full workflow for applying migrations in CI:

```bash
# 1. Generate and upload (from repo root, once)
tfmigrate generate --upload --backend-config=storage_account_name=myacct migrations/

# 2. Per layer: download applicable migrations
cd layers/compute
tfmigrate download --backend-config=storage_account_name=myacct

# 3. Plan and apply
tfmigrate plan --out=tfplan --detailed-exitcode
if [ $? -eq 2 ]; then
  tofu apply tfplan
fi
```

#### Resilient Multi-File Processing

When processing multiple migration YAML files, tfmigrate is resilient to individual file failures. If one YAML file fails — for example, because its source resource no longer exists in state after a partial pipeline run — it is skipped with an informational message to stderr, and remaining files continue to be processed:

```
Skipping "migrations/001_move.yaml": operation[0] (move): no resources matching "aws_instance.gone" found in state
```

This allows unrelated migrations to be generated even when some migrations reference resources that have already been moved. Parse errors and YAML validation errors remain fatal. If all files are skipped, the command returns an error.

#### Data Source Exclusion

Data sources (`data.*` resources) are automatically excluded from all migration operations. They are auto-computed by Terraform/OpenTofu and never need import or removed blocks. If a resource address matches only data sources in state, the generation will report that no managed resources were found.

#### Module-Level Consolidation

When all managed resources within a module are being moved out, tfmigrate automatically consolidates the individual `removed` blocks into a single module-level removal:

```hcl
# Instead of individual removed blocks for each resource:
#   removed { from = module.foo.aws_instance.web }
#   removed { from = module.foo.aws_s3_bucket.data }
# A single consolidated block is generated:
removed {
  from = module.foo
  lifecycle { destroy = false }
}
```

This works at any nesting depth. If all resources under `module.foo.module.bar` are moved but `module.foo` has other resources remaining, only `module.foo.module.bar` is consolidated. If the entire parent module is moved, consolidation rolls up to the parent level.

### Migration Metadata

Generated `.tf` files include an embedded metadata block used by `download` and `plan` commands:

```hcl
# Generated by tfmigrate - do not edit manually
#
# tfmigrate:metadata:begin
# {"conditions":{"resources_exist":[{"layer":".","addresses":["aws_instance.web"]}]},"resources":["aws_instance.web","aws_instance.api"]}
# tfmigrate:metadata:end

import {
  ...
}
```

The metadata contains:
- **conditions**: Auto-inferred conditions from block types (e.g., `resources_exist` for removed/moved blocks, `resources_not_exist` for import/moved blocks), merged with any explicit conditions from the migration YAML. Layer paths are relativized to `"."` for the owning layer.
- **resources**: All resource addresses touched by blocks in the file (used for `-target` flags)

### Output File Naming

Each migration YAML file produces a separate `.tf` file per layer, with a content-addressed filename:

```
<layer>/migration.<yaml_stem>.<sha256_8hex>.tf
```

For example, `migrations/001_move_web.yaml` generating blocks for two layers produces:
- `./layers/compute/migration.001_move_web.a1b2c3d4.tf`
- `./layers/app/migration.001_move_web.e5f6a7b8.tf`

The 8-character SHA-256 hash is computed from the rendered HCL content, ensuring:
- Filenames change when content changes (cache-busting)
- Identical content always produces the same filename (deterministic)
- Block ordering within each file is stable (sorted by type, then by address)

## Migration File Format

Migration files are YAML documents with a description and a list of operations:

```yaml
description: "Human-readable description of this migration"
schema_version: "2"  # optional
operations:
  - type: <move|rename|remove|import>
    # ... operation-specific fields
```

### Common Fields

All operation types support an optional `address_prefix` field that is prepended (with a dot separator) to all resource addresses in the operation:

```yaml
- type: move
  address_prefix: "module.identity_governance"
  resources:
    - address: "azuread_access_package.all"
    # Full address: module.identity_governance.azuread_access_package.all
```

### Operation Types

#### `move` — Cross-Layer Resource Move

Moves resources between OpenTofu layers. Generates `removed` blocks in the source layer and `import` blocks in the destination layer.

```yaml
- type: move
  description: "Move resources to app layer"
  source_layer: "./layers/compute"
  destination_layer: "./layers/app"
  address_prefix: "module.main"              # optional
  resources:
    - address: "aws_instance.web"
      import_id: "i-0abc123def456"           # optional: auto-resolved from state if omitted
    - address: "aws_instance.api"
```

When `import_id` is omitted, tfmigrate reads the source layer's state and extracts the resource's `id` attribute automatically.

**`destination_address`** — When the destination base address differs from the source:

```yaml
resources:
  - address: "module.old.resource.all"
    destination_address: "module.new.resource.all"
    keys:
      key1: key1
```

**Module-Level Move** — Move an entire module (all managed resources under it) to a new layer:

```yaml
resources:
  - address: "module.foo"
  - address: "module.foo"
    destination_address: "module.bar"   # optional: remap module prefix
```

When a module address is specified (e.g., `module.foo`), tfmigrate discovers all managed resources under that module from the source layer's state and generates import + removed blocks for each. The removed blocks are automatically consolidated into a single `removed { from = module.foo }`. Module moves do not support `keys` or `import_id` (import IDs are auto-resolved from state). If `destination_address` is provided, it must also be a module address. This works with `address_prefix` and at any nesting depth (nested sub-modules are included).

**`all_resources`** — Move all managed resources from the source layer to the destination layer:

```yaml
- type: move
  description: "Move entire layer"
  source_layer: "./layers/old"
  destination_layer: "./layers/new"
  all_resources: true
```

When `all_resources: true` is set, tfmigrate discovers all managed resources from the source layer's state and generates removed + import blocks for each. Data sources are excluded. Module-level consolidation applies automatically to the removed blocks.

Optional `resources` entries can be specified alongside `all_resources` to override destination addresses for specific resources (e.g., renaming during a bulk move):

```yaml
- type: move
  source_layer: "./layers/old"
  destination_layer: "./layers/new"
  all_resources: true
  resources:
    - address: "aws_instance.web"
      destination_address: "aws_instance.api"   # rename this one
```

All other resources keep their addresses unchanged. Override constraints: `destination_address` is required, `keys` and `import_id` are not allowed, module addresses cannot be used as overrides, and `address_prefix` cannot be combined with `all_resources`.

**`omit`** — Exclude specific resources from import during an `all_resources` move:

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

Omitted resources get `removed` blocks in the source layer (with `destroy = false` by default) but no `import` blocks in the destination layer. This is useful for resources that cannot be imported and need to be recreated. The `destroy` field can be set to `true` per entry if the resource should also be destroyed.

#### `rename` — In-Layer Rename

Renames resources or modules within a single layer. Generates `moved` blocks.

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

#### `remove` — Remove from State

Removes resources from state tracking. By default, the underlying infrastructure is preserved (`destroy = false`).

```yaml
- type: remove
  description: "Stop managing deprecated IAM resources"
  layer: "./layers/iam"
  destroy: false                             # default; set to true to also destroy
  addresses:
    - "aws_iam_role.deprecated"
    - "aws_iam_policy.old_policy"
```

#### `import` — Import Existing Resources

Imports existing cloud resources into OpenTofu state. Generates `import` blocks.

```yaml
- type: import
  description: "Import existing databases"
  layer: "./layers/database"
  imports:
    - address: "aws_db_instance.primary"
      import_id: "my-database-identifier"
      provider: "aws.useast1"                # optional provider alias
    - address: "aws_db_instance.replica"
      import_id: "my-replica-identifier"
```

## Keyed Moves

For `for_each` resources, use the `keys` map to specify how individual state keys are routed to destination keys.

```yaml
- type: move
  source_layer: "./layers/old"
  destination_layer: "./layers/new"
  resources:
    - address: "azuread_access_package_catalog.all"
      keys:
        mrt_customer: customer_approval                  # exact key rename
        mrt_outbound_provisioning: resource_tenant_access
        mrt_privileged_access: privileged_access
        mrt_vaw: vaw
```

### Key Pattern Types

| Pattern | Meaning | Example |
|---|---|---|
| `exact_key` | Matches exactly that for_each key | `mrt_customer: customer_approval` |
| `prefix_*` | Matches all keys starting with `prefix_` | `"mrt_customer_*": '{{ .Key \| trimPrefix "mrt_customer_" }}'` |
| `*` | Catch-all: matches all remaining unmatched keys | `"*": '{{ .Key }}'` |

Values can be literal strings or Go template expressions. Match priority: exact > longest prefix > catch-all.

### Completeness Rules

- When `keys` is present, **all state keys** must be matched. Unmatched keys cause an error.
- Overlapping key claims across operations cause an error.
- The same source resource can appear in multiple move operations with different destination layers. Keys are tracked across operations.

### Cross-Operation Key Splitting

```yaml
operations:
  - type: move
    source_layer: "./layers/shared"
    destination_layer: "./layers/engineering"
    resources:
      - address: "aws_resource.assignments"
        keys:
          "eng_*": '{{ .Key | trimPrefix "eng_" }}'
  - type: move
    source_layer: "./layers/shared"
    destination_layer: "./layers/finance"
    resources:
      - address: "aws_resource.assignments"
        keys:
          "fin_*": '{{ .Key | trimPrefix "fin_" }}'
```

### Without `keys` Map

When `keys` is omitted:
- **Single resource**: one `removed` + one `import` block
- **For_each resource**: expands all instances with the same keys

## Conditions

Conditions control whether a generated migration file is applied at download time. They are evaluated against the layer's current Terraform state.

### Auto-Inferred Conditions

By default, conditions are automatically inferred from the block types in each generated `.tf` file:

| Block type | Inferred condition | Rationale |
|------------|-------------------|-----------|
| `removed`  | `resources_exist` for `from` address | Skip if resource already gone |
| `import`   | `resources_not_exist` for `to` address | Skip if resource already imported |
| `moved`    | `resources_exist` for `from` AND `resources_not_exist` for `to` | Skip if rename already applied |

This makes all migrations idempotent by default — safe to re-run even after partial completion. For cross-layer moves (which decompose into `removed` + `import` blocks), each layer's file gets the correct condition automatically.

### Explicit Conditions (Optional Override)

Migration files also support an optional `condition` block that is merged (additively) with inferred conditions. Use this for cross-layer checks or custom logic that cannot be derived from the operations:

```yaml
description: "Move web server to app layer"
condition:
  resources_exist:
    - layer: "./layers/compute"
      addresses:
        - "aws_instance.web"
  resources_not_exist:
    - layer: "./layers/app"
      addresses:
        - "aws_instance.web"
operations:
  - type: move
    source_layer: "./layers/compute"
    destination_layer: "./layers/app"
    resources:
      - address: "aws_instance.web"
```

### Condition Types

| Type | Behavior |
|------|----------|
| `resources_exist` | ALL listed addresses must be found in the layer's state |
| `resources_not_exist` | NONE of the listed addresses must be found in the layer's state |

All condition checks are ANDed — every check must pass for the migration to proceed.

### Address Matching

- A base address (e.g., `aws_instance.web`) matches if **any** for_each instance exists in state
- A fully-qualified address (e.g., `aws_instance.web["key"]`) matches only that specific instance

## Go Template Reference

Templates in key values and `import_id` fields have access to:

| Field | Type | Description |
|-------|------|-------------|
| `.Address` | `string` | Full source address (e.g., `aws_s3_bucket.data["key-1"]`) |
| `.Type` | `string` | Resource type (e.g., `aws_s3_bucket`) |
| `.Name` | `string` | Resource name (e.g., `data`) |
| `.Index` | `any` | Raw for_each key or count index |
| `.Key` | `string` | String representation of `.Index` |
| `.Attributes` | `map` | All resource attributes from state |

### Template Functions

| Function | Usage | Description |
|----------|-------|-------------|
| `replace` | `{{ .Key \| replace "-" "_" }}` | Replace all occurrences |
| `replaceN` | `{{ .Key \| replaceN "-" "_" 1 }}` | Replace first N |
| `trimPrefix` | `{{ .Key \| trimPrefix "prefix-" }}` | Remove prefix |
| `trimSuffix` | `{{ .Key \| trimSuffix "-suffix" }}` | Remove suffix |
| `trimSpace` | `{{ .Key \| trimSpace }}` | Strip whitespace |
| `lower` | `{{ .Key \| lower }}` | Lowercase |
| `upper` | `{{ .Key \| upper }}` | Uppercase |
| `split` | `{{ .Key \| split "-" }}` | Split into list |
| `join` | `{{ .Key \| split "-" \| join "_" }}` | Join list |
| `at` | `{{ .Key \| split "/" \| at 1 }}` | Index into list (pipe-compatible) |
| `hasPrefix` | `{{ if .Key \| hasPrefix "prod" }}...{{ end }}` | Test prefix |
| `hasSuffix` | `{{ if .Key \| hasSuffix "-prod" }}...{{ end }}` | Test suffix |
| `contains` | `{{ if .Key \| contains "x" }}...{{ end }}` | Test substring |
| `attr` | `{{ attr .Attributes "tags" "Name" }}` | Nested map lookup |
| `default` | `{{ .Key \| default "fallback" }}` | Fallback for empty |
| `quote` | `{{ .Key \| quote }}` | Wrap in double quotes |
| `printf` | `{{ printf "%s-%s" .Type .Name }}` | Format string |
| `regexReplace` | `{{ .Key \| regexReplace "[^a-z]+" "_" }}` | Regex replacement |
| `sanitizeKey` | `{{ .Key \| sanitizeKey }}` | Lowercase + non-alphanumeric → `_` |
| `formatKey` | `{{ formatKey "%s_%s" .Attributes.a .Attributes.b }}` | Format + sanitize |

## Real-World Example

This example moves 4 resource types between layers with exact key renames, prefix patterns, and `address_prefix`:

```yaml
description: "Identity Governance - Restructuring"
operations:
  - type: move
    source_layer: ./blueprints/41-workplace
    destination_layer: ./blueprints/61-identity-governance
    address_prefix: module.identity_governance
    resources:
      - address: azuread_access_package_catalog.all
        keys:
          mrt_customer: customer_approval
          mrt_outbound_provisioning: resource_tenant_access
          mrt_privileged_access: privileged_access
          mrt_vaw: vaw

      - address: azuread_access_package.all
        keys:
          "mrt_customer_*": 'customer_approval_{{ .Key | trimPrefix "mrt_customer_" }}'
          "mrt_privileged_access_*": 'privileged_access_{{ .Key | trimPrefix "mrt_privileged_access_" }}'
          "mrt_outbound_provisioning_*": 'resource_tenant_access_{{ .Key | trimPrefix "mrt_outbound_provisioning_" }}'
          vaw_access: vaw_access

      - address: azuread_access_package_resource_package_association.all
        keys:
          "mrt_customer_*": 'customer_approval_{{ .Key | trimPrefix "mrt_customer_" | split "_entra_group_" | at 0 }}_AadGroup_{{ .Attributes.catalog_resource_association_id | split "/" | at 1 }}'
          "mrt_privileged_access_*": 'privileged_access_{{ .Key | trimPrefix "mrt_privileged_access_" | split "_entra_group_" | at 0 }}_AadGroup_{{ .Attributes.catalog_resource_association_id | split "/" | at 1 }}'
          "mrt_outbound_provisioning_*": 'resource_tenant_access_{{ .Key | trimPrefix "mrt_outbound_provisioning_" | split "_entra_group_" | at 0 }}_AadGroup_{{ .Attributes.catalog_resource_association_id | split "/" | at 1 }}'
```

## Architecture

```
pkg/
  migration/   - YAML schema, parsing, and validation
  state/       - OpenTofu state reading via terraform-exec
  template/    - Go template evaluation with custom functions
  generator/   - HCL block rendering, file output, and migration metadata
  engine/      - Pipeline orchestration, key matching, wildcard tracking
  conditions/  - Shared condition evaluation for upload guard and download
  auth/        - Azure credential management (azcore, azidentity)
  upload/      - Azure Blob Storage upload with overwrite protection guard
  download/    - Download orchestration with condition evaluation
  tofu/        - OpenTofu command execution and migration target scanning
```

## Requirements

- Go 1.25+ (for building)
- OpenTofu (`tofu`) in PATH (for state auto-resolution)

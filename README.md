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
      - from: "aws_instance.web"
        import_id: "i-0abc123def456"
```

2. Generate the HCL:

```bash
tfmigrate generate migrations/001_move_web_server.yaml
```

3. This produces two files (one per layer, named with a content hash):

**`./layers/compute/migration.001_move_web_server.a1b2c3d4.tf`**
```hcl
removed {
  from = aws_instance.web

  lifecycle {
    destroy = false
  }
}
```

**`./layers/app/migration.001_move_web_server.e5f6a7b8.tf`**
```hcl
import {
  to = aws_instance.web
  id = "i-0abc123def456"
}
```

4. Run `tofu plan` in each layer to verify, then `tofu apply`.

## What Can It Do?

### Move resources between layers

```yaml
- type: move
  source_layer: "./layers/compute"
  destination_layer: "./layers/app"
  resources:
    - from: "aws_instance.web"                     # import_id auto-resolved from state
    - from: "aws_instance.api"
      import_id: "i-0abc123def456"                 # or provide explicitly
```

Generates `removed` blocks in the source layer and `import` blocks in the destination.

### Move or rename within the same layer

When `source_layer` and `destination_layer` are the same, generates `moved` blocks instead:

```yaml
- type: move
  source_layer: "./layers/app"
  destination_layer: "./layers/app"
  source_prefix: "module.v1"
  destination_prefix: "module.v2"
  resources:
    - from: "aws_instance.web"
```

Use `source_prefix` / `destination_prefix` for independent control over each side's address prefix (move operations only). `address_prefix` remains available as a shorthand that applies to both sides.

### Rename resources within a layer

```yaml
- type: rename
  layer: "./layers/networking"
  renames:
    - from: "aws_subnet.old"
      to: "aws_subnet.new"
```

Generates `moved` blocks.

### Remove resources from state

```yaml
- type: remove
  layer: "./layers/iam"
  entries:
    - address: "aws_iam_role.deprecated"           # keeps infrastructure by default
    - address: "aws_iam_policy.old_policy"
      destroy: true                                # or destroy it
```

### Import existing resources

```yaml
- type: import
  layer: "./layers/database"
  imports:
    - address: "aws_db_instance.primary"
      id: "my-database-identifier"
```

### Import from another resource's state (source-based imports)

Derive import IDs from an existing resource's state attributes. Useful when splitting a resource into sub-resources (e.g., splitting `azuread_application` into `azuread_application_registration` + `azuread_api_access`):

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

With attribute expansion — each element of a list attribute produces a separate import block:

```yaml
- type: import
  layer: "./layers/identity"
  imports:
    - address: "azuread_api_access.all"
      id: '{{ .Attributes.id }}/apiAccess/{{ .Item.resource_app_id }}'
      key: '{{ .Key }}_{{ .Item.resource_app_id }}'
      source:
        layer: "./layers/identity"
        address: "azuread_application.all"
        expand: "required_resource_access"
```

### Move an entire module or layer

```yaml
# Move all resources under a module
- type: move
  source_layer: "./layers/old"
  destination_layer: "./layers/new"
  resources:
    - from: "module.foo"

# Or move everything from one layer to another
- type: move
  source_layer: "./layers/old"
  destination_layer: "./layers/new"
  all_resources: true
```

## Basic CLI Usage

```bash
# Generate from a single file
tfmigrate generate migrations/001_move.yaml

# Generate from a directory (files sorted by name)
tfmigrate generate migrations/

# Preview without writing files
tfmigrate generate --dry-run migrations/
```

| Flag | Description |
|------|-------------|
| `--dry-run` | Print generated HCL to stdout without writing files |
| `--tofu-path <path>` | Override path to the `tofu` binary |
| `--strict` | Treat missing layer directories as hard errors |

## Idempotent by Default

Conditions are automatically inferred from block types, making all migrations safe to re-run:

| Block type | Auto condition | Rationale |
|------------|---------------|-----------|
| `removed`  | `resources_exist` for source | Skip if already gone |
| `import`   | `resources_not_exist` for target | Skip if already imported |
| `moved`    | Both of the above | Skip if already renamed |

## CI Workflow

```bash
# 1. Generate and upload (from repo root)
tfmigrate generate --upload --backend-config=storage_account_name=myacct migrations/

# 2. Per layer: download applicable migrations
cd layers/compute && tfmigrate download --backend-config=storage_account_name=myacct

# 3. Plan and apply
tfmigrate plan --out=tfplan --detailed-exitcode
if [ $? -eq 2 ]; then
  tofu apply tfplan
fi
```

## Documentation

| Document | Description |
|----------|-------------|
| [CLI Reference](docs/cli-reference.md) | Full command and flag reference for `generate`, `upload`, `download`, `plan`, and `prune` |
| [Migration File Format](docs/migration-format.md) | Complete YAML schema — all operation types, fields, and validation rules |
| [Keyed Moves](docs/keyed-moves.md) | Moving `for_each` resources with key remapping, prefix patterns, and cross-operation splitting |
| [Conditions](docs/conditions.md) | Auto-inferred and explicit conditions, `layer_exists`/`layer_not_exists`, address matching |
| [Go Templates](docs/templates.md) | Template context variables and all available functions for key transformations and import IDs |
| [Azure Blob Storage](docs/azure-storage.md) | Upload, download, pruning, backend discovery, upload guard, and authentication |
| [CI Workflow](docs/ci-workflow.md) | Full CI integration guide, resilient processing, module consolidation, and strict mode |

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

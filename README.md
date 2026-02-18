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

3. This produces two files:

**`./layers/compute/migrations.tf`**
```hcl
removed {
  from = aws_instance.web

  lifecycle {
    destroy = false
  }
}
```

**`./layers/app/migrations.tf`**
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
| `--output-filename <name>` | Override output filename (default: `migrations.tf`) |
| `--tofu-path <path>` | Override path to the `tofu` binary (default: auto-detect from PATH) |

### Dry Run

Preview what would be generated without writing any files:

```bash
tfmigrate generate --dry-run migrations/
```

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
  generator/   - HCL block rendering and file output
  engine/      - Pipeline orchestration, key matching, wildcard tracking
```

## Requirements

- Go 1.25+ (for building)
- OpenTofu (`tofu`) in PATH (for state auto-resolution)

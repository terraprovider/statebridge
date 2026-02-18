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
    source:
      layer: "./layers/compute"
      address: "aws_instance.web"
    destination:
      layer: "./layers/app"
      address: "aws_instance.web"
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

# Multiple files (processed in argument order)
tfmigrate generate migrations/001_move.yaml migrations/002_rename.yaml

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
schema_version: "1"  # optional, for forward compatibility
operations:
  - type: <move|rename|remove|import>
    # ... operation-specific fields
```

### Operation Types

#### `move` — Cross-Layer Resource Move

Moves a resource from one OpenTofu layer (root module) to another. Generates a `removed` block in the source layer and an `import` block in the destination layer.

```yaml
- type: move
  description: "Move web instance to app layer"
  source:
    layer: "./layers/compute"
    address: "aws_instance.web"
  destination:
    layer: "./layers/app"
    address: "aws_instance.web"
  import_id: "i-0abc123def456"  # optional: auto-resolved from state if omitted
```

When `import_id` is omitted, tfmigrate reads the source layer's state (`tofu show -json`) and extracts the resource's `id` attribute automatically.

#### `rename` — In-Layer Rename

Renames a resource or module within a single layer. Generates a `moved` block.

```yaml
- type: rename
  description: "Rename VPC module"
  layer: "./layers/networking"
  from: "module.old_vpc"
  to: "module.new_vpc"
```

#### `remove` — Remove from State

Removes a resource from state tracking. By default, the underlying infrastructure is preserved (`destroy = false`).

```yaml
- type: remove
  description: "Stop managing deprecated IAM role"
  layer: "./layers/iam"
  address: "aws_iam_role.deprecated"
  destroy: false  # default; set to true to also destroy the resource
```

#### `import` — Import Existing Resource

Imports an existing cloud resource into OpenTofu state. Generates an `import` block.

```yaml
- type: import
  description: "Import existing RDS instance"
  layer: "./layers/database"
  address: "aws_db_instance.primary"
  import_id: "my-database-identifier"
  provider: "aws.useast1"  # optional provider alias
```

## Advanced Usage

### Wildcard Expansion with `[*]`

When a source address ends with `[*]`, tfmigrate expands it against the source layer's state to enumerate all `for_each` or `count` instances. Combined with Go templates, this enables bulk moves with key transformations.

```yaml
description: "Re-key S3 buckets from composite keys to bucket names"
operations:
  - type: move
    source:
      layer: "./layers/data"
      address: "aws_s3_bucket.data[*]"
    destination:
      layer: "./layers/storage"
      address: 'aws_s3_bucket.data["{{ .Attributes.bucket }}"]'
    import_id: "{{ .Attributes.id }}"
```

If the source state contains:

- `aws_s3_bucket.data["composite-abc-us-east-1"]` with `bucket = "my-bucket-abc"`
- `aws_s3_bucket.data["composite-xyz-eu-west-1"]` with `bucket = "my-bucket-xyz"`

This generates:

**Source layer (`removed` blocks):**
```hcl
removed {
  from = aws_s3_bucket.data["composite-abc-us-east-1"]
  lifecycle { destroy = false }
}

removed {
  from = aws_s3_bucket.data["composite-xyz-eu-west-1"]
  lifecycle { destroy = false }
}
```

**Destination layer (`import` blocks):**
```hcl
import {
  to = aws_s3_bucket.data["my-bucket-abc"]
  id = "composite-abc-us-east-1"
}

import {
  to = aws_s3_bucket.data["my-bucket-xyz"]
  id = "composite-xyz-eu-west-1"
}
```

### Go Template Context

Templates in `address` and `import_id` fields receive a context with the following fields:

| Field | Type | Description |
|-------|------|-------------|
| `.Address` | `string` | Full source address (e.g., `aws_s3_bucket.data["key-1"]`) |
| `.Type` | `string` | Resource type (e.g., `aws_s3_bucket`) |
| `.Name` | `string` | Resource name (e.g., `data`) |
| `.Index` | `any` | Raw for_each key (string) or count index (int) |
| `.Key` | `string` | String representation of `.Index` |
| `.Attributes` | `map` | All resource attributes from state |

### Template Functions

The following custom functions are available in templates. All string functions accept the input as the last argument for pipe compatibility.

| Function | Usage | Description |
|----------|-------|-------------|
| `replace` | `{{ .Key \| replace "-" "_" }}` | Replace all occurrences |
| `replaceN` | `{{ .Key \| replaceN "-" "_" 1 }}` | Replace first N occurrences |
| `trimPrefix` | `{{ .Key \| trimPrefix "prefix-" }}` | Remove prefix |
| `trimSuffix` | `{{ .Key \| trimSuffix "-suffix" }}` | Remove suffix |
| `trimSpace` | `{{ .Key \| trimSpace }}` | Remove leading/trailing whitespace |
| `lower` | `{{ .Key \| lower }}` | Lowercase |
| `upper` | `{{ .Key \| upper }}` | Uppercase |
| `split` | `{{ .Key \| split "-" }}` | Split into list |
| `join` | `{{ .Key \| split "-" \| join "_" }}` | Join list with separator |
| `hasPrefix` | `{{ if .Key \| hasPrefix "prod" }}...{{ end }}` | Test prefix |
| `hasSuffix` | `{{ if .Key \| hasSuffix "-prod" }}...{{ end }}` | Test suffix |
| `contains` | `{{ if .Key \| contains "special" }}...{{ end }}` | Test substring |
| `attr` | `{{ attr .Attributes "tags" "Name" }}` | Nested map lookup |
| `default` | `{{ .Key \| default "fallback" }}` | Fallback for empty values |
| `quote` | `{{ .Key \| quote }}` | Wrap in double quotes |
| `printf` | `{{ printf "%s-%s" .Type .Name }}` | Formatted string |

### Complex Key Transformation Examples

**Replace hyphens with underscores in keys:**
```yaml
- type: move
  source:
    layer: "./layers/old"
    address: "aws_security_group.rules[*]"
  destination:
    layer: "./layers/new"
    address: 'aws_security_group.rules["{{ .Key | replace "-" "_" }}"]'
```

**Re-key using a nested tag value:**
```yaml
- type: move
  source:
    layer: "./layers/old"
    address: "aws_instance.servers[*]"
  destination:
    layer: "./layers/new"
    address: 'aws_instance.servers["{{ attr .Attributes "tags" "Name" }}"]'
```

**Conditional key transformation:**
```yaml
- type: move
  source:
    layer: "./layers/old"
    address: "aws_s3_bucket.buckets[*]"
  destination:
    layer: "./layers/new"
    address: 'aws_s3_bucket.buckets["{{ if .Key | hasPrefix "prod" }}production-{{ .Attributes.bucket }}{{ else }}{{ .Attributes.bucket }}{{ end }}"]'
```

**Use ARN as import ID instead of default `id` attribute:**
```yaml
- type: move
  source:
    layer: "./layers/old"
    address: "aws_iam_role.roles[*]"
  destination:
    layer: "./layers/new"
    address: 'aws_iam_role.roles["{{ .Attributes.name }}"]'
  import_id: "{{ .Attributes.arn }}"
```

### Multiple Operations in One File

A single migration file can contain multiple operations. When multiple operations target the same layer, their blocks are aggregated into a single output file.

```yaml
description: "Restructure networking layer"
operations:
  - type: rename
    description: "Rename VPC module"
    layer: "./layers/networking"
    from: "module.old_vpc"
    to: "module.new_vpc"

  - type: remove
    description: "Remove legacy security group"
    layer: "./layers/networking"
    address: "aws_security_group.legacy"

  - type: import
    description: "Import new route table"
    layer: "./layers/networking"
    address: "aws_route_table.new"
    import_id: "rtb-0abc123"
```

This produces a single `./layers/networking/migrations.tf`:

```hcl
# Generated by tfmigrate - do not edit manually

# Rename VPC module
moved {
  from = module.old_vpc
  to   = module.new_vpc
}

# Remove legacy security group
removed {
  from = aws_security_group.legacy

  lifecycle {
    destroy = false
  }
}

# Import new route table
import {
  to = aws_route_table.new
  id = "rtb-0abc123"
}
```

### Ordered Migration Sequences

Name your migration files with a numeric prefix to control processing order:

```
migrations/
  001_move_compute_resources.yaml
  002_rename_networking_modules.yaml
  003_import_new_database.yaml
  004_cleanup_legacy_resources.yaml
```

```bash
tfmigrate generate migrations/
```

Files are sorted lexicographically and processed in order. This ensures dependent migrations are applied in the correct sequence.

## Architecture

tfmigrate is built with a modular package architecture:

```
pkg/
  migration/   - YAML schema, parsing, and validation
  state/       - OpenTofu state reading via terraform-exec
  template/    - Go template evaluation with custom functions
  generator/   - HCL block rendering and file output
  engine/      - Pipeline orchestration
```

Each package has a well-defined responsibility and can be extended independently. The engine ties everything together: parse YAML, read state, resolve import IDs, expand wildcards, generate HCL blocks, and write output files.

## Requirements

- Go 1.25+ (for building)
- OpenTofu (`tofu`) in PATH (for state auto-resolution; not needed if all `import_id` values are explicit)

## License

See [LICENSE](LICENSE) for details.

# CLI Reference

Complete reference for all tfmigrate commands and flags.

## `generate`

Generate HCL migration files from YAML definitions.

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
| `--strict` | Treat missing layer directories as hard errors instead of auto-skipping |

### Dry Run

Preview what would be generated without writing any files:

```bash
tfmigrate generate --dry-run migrations/
```

By default, migration files referencing non-existent layer directories (e.g., `source_layer` that was deleted after a migration was applied) are automatically skipped with a message. Use `--strict` to make missing layers a hard error.

---

## `upload`

Upload pre-generated migration files to Azure Blob Storage.

```
tfmigrate upload [layer-dirs...] [flags]
```

Each layer directory is scanned for `migration.*.tf` files and uploaded to the storage container discovered from the layer's backend configuration.

### Flags

| Flag | Description |
|------|-------------|
| `--backend-config` | Backend configuration passed to tofu init, as `key=value` or path to a file (repeatable) |
| `--force` | Force upload even if existing migrations are still active (overwrite protection bypass) |
| `--tofu-path <path>` | Override path to the `tofu` binary for upload guard state evaluation (default: auto-detect from PATH) |

```bash
# Upload from specific layers
tfmigrate upload ./layers/compute ./layers/networking

# Override backend config values
tfmigrate upload --backend-config=storage_account_name=myacct ./layers/compute

# Use a backend config file
tfmigrate upload --backend-config=backend.hcl ./layers/compute
```

See [Azure Blob Storage](azure-storage.md) for details on backend discovery, upload guard, and authentication.

---

## `download`

Download applicable migration files from the layer's blob storage container to the current directory.

```
tfmigrate download [flags]
```

This command must be run from within a layer directory containing backend configuration. Conditions embedded in migration metadata are evaluated against the layer's state, and only migrations that need to be applied are written.

### Flags

| Flag | Description |
|------|-------------|
| `--backend-config` | Backend configuration passed to tofu init, as `key=value` or path to a file (repeatable) |
| `--tofu-path <path>` | Override path to the `tofu` binary |
| `--dry-run` | Print what would be downloaded without writing files |

```bash
cd layers/compute

# Download applicable migrations
tfmigrate download

# Override backend config
tfmigrate download --backend-config=storage_account_name=myacct

# Preview what would be downloaded
tfmigrate download --dry-run
```

### Download Flow

1. Discovers backend config from `.tf` files in the current directory (+ `--backend-config` overrides)
2. Lists all `migrations/migration.*.tf` blobs in the layer's storage container
3. For each migration:
   - Downloads the blob content
   - Parses embedded metadata (conditions and resource addresses)
   - Initializes the backend if `--backend-config` was provided
   - Evaluates conditions against the layer's current state
   - Writes the file to the current directory only if conditions are met
4. Skipped migrations print an informational message to stderr

See [Azure Blob Storage](azure-storage.md) for details on backend discovery and authentication.

---

## `plan`

Run targeted `tofu plan` scoped to resources in downloaded migration files. Must be run from a layer directory containing `migration.*.tf` files.

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

### Flags

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

### Applying Changes

To apply changes, save the plan and use tofu directly:

```bash
tfmigrate plan --out=tfplan --detailed-exitcode
# exit 0 = no changes, exit 2 = changes detected
if [ $? -eq 2 ]; then
  tofu apply tfplan
fi
```

---

## `prune`

Removes completed migration blobs from Azure Blob Storage.

```bash
# Dry run: see what would be pruned
tfmigrate prune --dry-run ./layers/compute ./layers/networking

# Prune completed migrations (evaluates embedded conditions)
tfmigrate prune ./layers/compute ./layers/networking

# Force delete all migration blobs
tfmigrate prune --force ./layers/compute
```

### Flags

| Flag | Description |
|------|-------------|
| `--dry-run` | List what would be pruned without deleting |
| `--backend-config` | Backend configuration for tofu init (repeatable) |
| `--tofu-path <path>` | Path to the tofu binary (default: auto-detect) |
| `--force` | Delete all migration blobs without evaluating conditions |

---

## Output File Naming

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

## Migration Metadata

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

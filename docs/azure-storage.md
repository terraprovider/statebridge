# Azure Blob Storage

tfmigrate can persist generated migration files to Azure Blob Storage and download them per-layer during CI runs. This enables a workflow where migrations are generated centrally and applied per-layer in separate pipeline steps.

## Upload

### Generate and Upload in One Step

```bash
tfmigrate generate --upload --backend-config=storage_account_name=myacct migrations/
```

This runs the full generation pipeline, writes files to disk, then uploads each generated `.tf` file to `migrations/<filename>` in the Azure Blob Storage container configured in that layer's backend. Cannot be combined with `--dry-run`.

### Standalone Upload

Upload pre-generated `migration.*.tf` files from layer directories:

```bash
tfmigrate upload ./layers/compute ./layers/networking
```

| Flag | Description |
|------|-------------|
| `--backend-config` | Backend configuration passed to tofu init, as `key=value` or path to a file (repeatable) |
| `--force` | Force upload even if existing migrations are still active (overwrite protection bypass) |
| `--tofu-path <path>` | Override path to the `tofu` binary (default: auto-detect from PATH) |

```bash
# Override backend config values
tfmigrate upload --backend-config=storage_account_name=myacct ./layers/compute

# Use a backend config file
tfmigrate upload --backend-config=backend.hcl ./layers/compute
```

## Backend Configuration Discovery

The upload target (storage account and container) is resolved per layer by:

1. Parsing `.tf` files in the layer directory for a `backend "azurerm"` block
2. Extracting `key=value` pairs from `--backend-config` CLI flags. File paths are also supported: `--backend-config=path/to/file.hcl` reads key=value pairs from the file (HCL or plain text format)
3. Merging: `--backend-config` flags override inline HCL values

Required backend fields: `storage_account_name`, `container_name`.

## Version Cleanup

Before uploading, the tool checks for existing blobs matching the same migration stem (e.g., `migrations/migration.001_move.*.tf`). Old versions with a different content hash are automatically deleted:

```
Removed old version: migrations/migration.001_move.oldold00.tf
Uploaded: migrations/migration.001_move.newnew99.tf
```

The storage account is expected to have blob versioning enabled, so deleted versions remain recoverable through Azure's versioning.

## Upload Guard (Overwrite Protection)

When uploading, tfmigrate checks whether existing migration blobs are still "active" (their metadata conditions still pass against the layer's state). If an existing blob is still needed — for example, because a cross-layer migration was only partially applied — the upload is refused:

```
Error: refusing to overwrite "migrations/migration.001_move.a1b2c3d4.tf": migration is still active in layer "./layers/app" (conditions pass); use --force to override
```

This protects against a common CI failure mode: a pipeline partially applies migrations across layers (e.g., L10 applied, L30 fails, L50 pending), then re-runs `generate --upload` which would otherwise overwrite the still-needed import blocks.

- The guard requires the `tofu` binary to read layer state — since `tofu` is a hard requirement for all commands, the guard is always active
- Use `--force` to explicitly bypass the guard

## Auto-Pruning Stale Blobs

When using `generate --upload`, migration blobs for retired files (`status: retired`) and files whose source layers no longer exist are automatically pruned from blob storage. This keeps storage clean without manual intervention.

Auto-pruning only applies to layers that active migrations are uploading to. For complete cleanup across all layers, use the `prune` command.

## Download

Download applicable migration files from the layer's blob storage container:

```bash
cd layers/compute && tfmigrate download
```

Must be run from within a layer directory containing backend configuration.

| Flag | Description |
|------|-------------|
| `--backend-config` | Backend configuration passed to tofu init, as `key=value` or path to a file (repeatable) |
| `--tofu-path <path>` | Override path to the `tofu` binary |
| `--dry-run` | Print what would be downloaded without writing files |

### Download Flow

1. Discovers backend config from `.tf` files in the current directory (+ `--backend-config` overrides)
2. Lists all `migrations/migration.*.tf` blobs in the layer's storage container
3. Cleans up all existing `migration.*.tf` files in the target directory (blob storage is source of truth)
4. Downloads each blob and parses embedded metadata
5. Evaluates auto-inferred + explicit conditions: for `layer == "."`, reads state and checks; for cross-layer conditions, warns and treats as met
6. Writes only applicable files; skipped files print a message to stderr

## Prune

Remove completed migration blobs from Azure Blob Storage:

```bash
# Dry run: see what would be pruned
tfmigrate prune --dry-run ./layers/compute ./layers/networking

# Prune completed migrations (evaluates embedded conditions)
tfmigrate prune ./layers/compute ./layers/networking

# Force delete all migration blobs
tfmigrate prune --force ./layers/compute
```

| Flag | Description |
|------|-------------|
| `--dry-run` | List what would be pruned without deleting |
| `--backend-config` | Backend configuration for tofu init (repeatable) |
| `--tofu-path <path>` | Path to the tofu binary (default: auto-detect) |
| `--force` | Delete all migration blobs without evaluating conditions |

## Authentication

Upload, download, and prune commands authenticate using Azure SDK credentials configured through environment variables:

| Variable(s) | Method |
|-------------|--------|
| `ARM_CLIENT_ID`, `ARM_TENANT_ID`, `ARM_CLIENT_SECRET` | Service principal |
| `ARM_USE_CLI` | Azure CLI credential |
| `ARM_USE_MSI` | Managed identity |
| `ARM_USE_OIDC` | OIDC federation (GitHub Actions, ADO Pipeline, generic) |
| `ARM_OIDC_TOKEN` | Direct OIDC assertion token |
| `ARM_OIDC_REQUEST_URL` / `ACTIONS_ID_TOKEN_REQUEST_URL` | OIDC token request URL |
| `ARM_OIDC_REQUEST_TOKEN` / `ACTIONS_ID_TOKEN_REQUEST_TOKEN` | OIDC request auth token |

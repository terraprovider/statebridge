# Azure Blob Storage

statebridge can persist generated migration files to Azure Blob Storage and download them per-layer during CI runs. This enables a workflow where migrations are generated centrally and applied per-layer in separate pipeline steps.

## Upload

### Generate and Upload in One Step

```bash
statebridge generate --upload --backend-config=storage_account_name=myacct migrations/
```

This runs the full generation pipeline, writes files to disk, then uploads each generated `.tf` file to `migrations/<filename>` in the Azure Blob Storage container configured in that layer's backend. Cannot be combined with `--dry-run`.

### Standalone Upload

Upload pre-generated `migration.*.tf` files from layer directories:

```bash
statebridge upload ./layers/compute ./layers/networking
```

| Flag | Description |
|------|-------------|
| `--backend-config` | Backend configuration passed to tofu init, as `key=value` or path to a file (repeatable) |
| `--force` | Force upload even if existing migrations are still active (overwrite protection bypass) |
| `--tofu-path <path>` | Override path to the `tofu` binary (default: auto-detect from PATH) |

```bash
# Override backend config values
statebridge upload --backend-config=storage_account_name=myacct ./layers/compute

# Use a backend config file
statebridge upload --backend-config=backend.hcl ./layers/compute
```

## Backend Configuration Discovery

The upload target (storage account and container) is resolved per layer by:

1. Parsing `.tf` files in the layer directory for a `backend "azurerm"` block
2. Extracting `key=value` pairs from `--backend-config` CLI flags. File paths are also supported: `--backend-config=path/to/file.hcl` reads key=value pairs from the file (HCL or plain text format)
3. Merging: `--backend-config` flags override inline HCL values

Required backend fields: `storage_account_name`, `container_name`.

Any recognized authentication attributes present in the backend configuration (see [Per-Layer Credential Overrides](#per-layer-credential-overrides) below) are discovered and merged the same way, and are kept separate from the storage coordinates above.

## Shared-Container Scoping

By default each layer is assumed to have its own storage container, so its `migrations/` prefix holds only its own blobs. If **multiple layers share a single storage account and container** (distinguished only by their backend `key`), all layers' migration blobs sit next to each other under the same `migrations/` prefix.

To keep each layer acting only on its own blobs, statebridge embeds the owning layer's backend coordinates (`storage_account_name`, `container_name`, `key`) in each generated file's metadata as a `source_layer` descriptor (see the "Migration Metadata" section in `AGENTS.md`). Every blob-listing operation is then scoped:

- **`download`** skips blobs whose `source_layer` provably belongs to a different layer.
- **`upload`** guard and old-version cleanup never block on or delete another layer's blobs.
- **`prune`** (and auto-prune on `--upload`) never delete another layer's blobs; `--force` stays layer-scoped.

Scoping is fully backward compatible: files generated before this feature carry no `source_layer` and are treated exactly as before (unscoped). When a layer's own `key` cannot be determined at download time, statebridge falls back to condition-based applicability and prints a warning.

For scoping to work, each layer's backend block must declare a distinct `key`. The `key` is now parsed from the `backend "azurerm"` block and `--backend-config=key=...` flags.

## Version Cleanup

Before uploading, the tool checks for existing blobs matching the same migration stem (e.g., `migrations/migration.001_move.*.tf`). Old versions with a different content hash are automatically deleted:

```
Removed old version: migrations/migration.001_move.oldold00.tf
Uploaded: migrations/migration.001_move.newnew99.tf
```

The storage account is expected to have blob versioning enabled, so deleted versions remain recoverable through Azure's versioning.

## Upload Guard (Overwrite Protection)

When uploading, statebridge checks whether existing migration blobs are still "active" (their metadata conditions still pass against the layer's state). If an existing blob is still needed — for example, because a cross-layer migration was only partially applied — the upload is refused:

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
cd layers/compute && statebridge download
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
5. Scopes by `source_layer`: skips blobs owned by a different layer (shared container); legacy blobs and indeterminate-key cases fall through to condition evaluation
6. Evaluates auto-inferred + explicit conditions: for `layer == "."`, reads state and checks; for cross-layer conditions, warns and treats as met
7. Writes only applicable files; skipped files print a message to stderr

## Prune

Remove completed migration blobs from Azure Blob Storage:

```bash
# Dry run: see what would be pruned
statebridge prune --dry-run ./layers/compute ./layers/networking

# Prune completed migrations (evaluates embedded conditions)
statebridge prune ./layers/compute ./layers/networking

# Force delete all migration blobs
statebridge prune --force ./layers/compute
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

These environment variables provide the baseline credential configuration for every layer.

### Per-Layer Credential Overrides

A layer's backend configuration (its inline `backend "azurerm"` block, and/or `--backend-config` CLI flags/files) can additionally supply any of the same authentication attributes that OpenTofu's own azurerm backend accepts. When present, these values are merged on top of the environment-sourced configuration: environment variables are read first, and a value supplied via backend configuration takes precedence on conflict. This mirrors how OpenTofu itself resolves azurerm backend authentication, and lets an individual layer's state storage live in a different Entra tenant/service principal than the one the rest of the pipeline runs under.

Recognized backend-config authentication attributes:

| Key | Purpose |
|-----|---------|
| `client_id` | Service principal (application) ID |
| `tenant_id` | Entra tenant (directory) ID |
| `client_secret` | Service principal client secret |
| `client_certificate_path` | Path to a PFX/PKCS12 client certificate |
| `client_certificate_password` | Password for the client certificate file |
| `use_cli` | Authenticate via the Azure CLI (`az login` session) |
| `use_msi` | Authenticate via Managed Service Identity |
| `use_oidc` | Authenticate via OpenID Connect (OIDC) federation |
| `oidc_token` | Direct OIDC assertion token |
| `oidc_request_url` | OIDC token request URL |
| `oidc_request_token` | OIDC request auth token |
| `ado_service_connection_id` | ADO Pipeline service connection ID for OIDC |

Example: a layer whose state lives in a storage account under a different tenant than the pipeline's default credentials:

```hcl
terraform {
  backend "azurerm" {
    storage_account_name = "otherTenantAcct"
    container_name        = "tfstate"
    key                   = "compute.tfstate"
    client_id             = "11111111-1111-1111-1111-111111111111"
    tenant_id             = "22222222-2222-2222-2222-222222222222"
    use_oidc              = true
  }
}
```

Or supplied via `--backend-config`, keeping secrets out of committed HCL:

```bash
statebridge download \
  --backend-config=client_id=11111111-1111-1111-1111-111111111111 \
  --backend-config=tenant_id=22222222-2222-2222-2222-222222222222 \
  --backend-config=use_oidc=true
```

As with the other backend-config values, `--backend-config` flags/files take precedence over the inline HCL block, and (as noted above) the fully merged set of layer values takes precedence over environment variables. As OpenTofu itself recommends, prefer environment variables or a git-ignored `--backend-config` file over committing secrets like `client_secret` directly in a `.tf` backend block.

| `ARM_OIDC_REQUEST_TOKEN` / `ACTIONS_ID_TOKEN_REQUEST_TOKEN` | OIDC request auth token |

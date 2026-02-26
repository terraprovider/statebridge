# CI Workflow

Guide for integrating tfmigrate into continuous integration pipelines.

## Full Workflow

The typical CI workflow for applying migrations:

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

### Step 1: Generate and Upload

Run once from the repository root. This reads the migration YAML files, resolves import IDs from state, generates HCL files, and uploads them to each layer's blob storage container.

The `--backend-config` flag is used both for auto-initializing layers during state reads and for discovering the upload target.

### Step 2: Download Per Layer

Each layer downloads only the migrations that apply to it. Conditions embedded in the migration metadata are evaluated against the layer's current state — migrations that have already been applied are skipped.

### Step 3: Plan and Apply

`tfmigrate plan` runs a targeted `tofu plan` scoped to only the resources touched by the downloaded migrations. The `--detailed-exitcode` flag returns exit code 2 when changes are detected, allowing conditional apply.

## Backend Configuration

In CI environments where backends aren't pre-initialized, use `--backend-config` to pass backend configuration:

```bash
# Key=value pairs
tfmigrate generate --backend-config=storage_account_name=myacct --backend-config=key=terraform.tfstate migrations/

# Or point to a backend config file
tfmigrate generate --backend-config=backend.hcl migrations/
```

This mirrors the `tofu init -backend-config=...` syntax. Layers are auto-initialized on first state read if needed.

## Strict Mode

Use `--strict` to treat missing layer directories as hard errors instead of auto-skipping:

```bash
tfmigrate generate --strict migrations/
```

By default, missing layers are silently skipped — useful when a migration references a layer that was deleted after being applied. Strict mode is recommended for CI to catch configuration errors early.

## Resilient Multi-File Processing

When processing multiple migration YAML files, tfmigrate is resilient to individual file failures. If one YAML file fails — for example, because its source resource no longer exists in state after a partial pipeline run — it is skipped with an informational message:

```
Skipping "migrations/001_move.yaml": operation[0] (move): no resources matching "aws_instance.gone" found in state
```

This allows unrelated migrations to be generated even when some migrations reference resources that have already been moved. Parse errors and YAML validation errors remain fatal. If all files are skipped, the command returns an error.

## Data Source Exclusion

Data sources (`data.*` resources) are automatically excluded from all migration operations. They are auto-computed by Terraform/OpenTofu and never need import or removed blocks. If a resource address matches only data sources in state, the generation will report that no managed resources were found.

## Module-Level Consolidation

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

This works at any nesting depth. If all resources under `module.foo.module.bar` are moved but `module.foo` has other resources remaining, only `module.foo.module.bar` is consolidated.

## Upload Guard in CI

The upload guard protects against a common CI failure mode: a pipeline partially applies migrations across layers (e.g., L10 applied, L30 fails, L50 pending), then re-runs `generate --upload` which would overwrite still-needed import blocks.

When existing blobs are still active, the upload is refused. Use `--force` to bypass this protection when intentional overwrite is needed.

See [Azure Blob Storage](azure-storage.md) for full details on upload, download, and pruning.

## Pruning Completed Migrations

After migrations have been fully applied across all layers, clean up the blob storage:

```bash
# Dry run first
tfmigrate prune --dry-run ./layers/compute ./layers/networking

# Then prune
tfmigrate prune ./layers/compute ./layers/networking
```

Auto-pruning also happens during `generate --upload` for retired files and missing source layers.

# pkg/upload

The upload package handles persisting generated migration `.tf` files to Azure Blob Storage and pruning stale blobs.

## Source Files

| File | Purpose |
|------|---------|
| `upload.go` | `Manager` orchestration: version cleanup, upload, overwrite protection guard, prune. Guard/cleanup/prune are layer-scoped via `source_layer` metadata (`BlobContentOwnedByOtherLayer`) so shared-container blobs of other layers are left untouched. Resolves and caches a per-layer credential via `ResolveCredential` before creating each layer's uploader |
| `uploader.go` | `BlobUploader` interface and Azure Blob Storage implementation |
| `backend.go` | Backend config discovery: parses HCL `backend "azurerm"` blocks (including `key` and `auth.CredentialKeys` authentication attributes) and merges `--backend-config` flags |
| `credential.go` | `ResolveCredential`: builds the per-layer Azure credential by merging `BackendConfig.Credentials` on top of the shared environment-sourced base credential (via `auth.WithBackendConfig`); returns the base credential unchanged when a layer has no credential values |

## Test Files

Tests are split by topic. All files share the `mockBlobUploader` type defined in `upload_test.go`.

| File | Tests |
|------|-------|
| `upload_test.go` | Mock type (`mockBlobUploader`) + core upload tests: YAML stem extraction, old version cleanup, rendered upload, disk upload, uploader caching, cleanup-and-upload flow |
| `upload_guard_test.go` | Overwrite protection guard: triggered, not triggered, force bypass, no existing blobs, no guard checker, same hash skipped, eval error, integration test |
| `upload_prune_test.go` | Prune operations: active blobs kept, expired blobs pruned, force prune, dry-run prune |
| `backend_test.go` | Backend config parsing and merging, including credential attribute extraction/merging |
| `credential_test.go` | `ResolveCredential` behaviour + `Manager` per-layer credential resolution/caching/error propagation |

## Running Tests

```bash
go test ./pkg/upload/... -count=1
```

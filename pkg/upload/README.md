# pkg/upload

The upload package handles persisting generated migration `.tf` files to Azure Blob Storage and pruning stale blobs.

## Source Files

| File | Purpose |
|------|---------|
| `upload.go` | `Manager` orchestration: version cleanup, upload, overwrite protection guard, prune. Guard/cleanup/prune are layer-scoped via `source_layer` metadata (`BlobContentOwnedByOtherLayer`) so shared-container blobs of other layers are left untouched |
| `uploader.go` | `BlobUploader` interface and Azure Blob Storage implementation |
| `backend.go` | Backend config discovery: parses HCL `backend "azurerm"` blocks (including `key`) and merges `--backend-config` flags |

## Test Files

Tests are split by topic. All files share the `mockBlobUploader` type defined in `upload_test.go`.

| File | Tests |
|------|-------|
| `upload_test.go` | Mock type (`mockBlobUploader`) + core upload tests: YAML stem extraction, old version cleanup, rendered upload, disk upload, uploader caching, cleanup-and-upload flow |
| `upload_guard_test.go` | Overwrite protection guard: triggered, not triggered, force bypass, no existing blobs, no guard checker, same hash skipped, eval error, integration test |
| `upload_prune_test.go` | Prune operations: active blobs kept, expired blobs pruned, force prune, dry-run prune |
| `backend_test.go` | Backend config parsing and merging |

## Running Tests

```bash
go test ./pkg/upload/... -count=1
```

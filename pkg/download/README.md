# pkg/download

The download package handles downloading migration files from Azure Blob Storage with condition-based filtering.

## Source Files

| File | Purpose |
|------|---------|
| `download.go` | `Manager`: lists blobs, parses metadata, scopes by `source_layer` (skips other layers' blobs in a shared container), evaluates conditions, downloads applicable migration files to the layer directory. Resolves the layer's credential via `upload.ResolveCredential` before creating the uploader, so a layer's backend-config credential values (if any) take precedence over the shared base credential |

## Test Files

| File | Tests |
|------|-------|
| `download_test.go` | Download tests: condition evaluation, blob filtering, metadata parsing, cross-layer condition handling |

## Running Tests

```bash
go test ./pkg/download/... -count=1
```

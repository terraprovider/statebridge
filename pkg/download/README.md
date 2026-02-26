# pkg/download

The download package handles downloading migration files from Azure Blob Storage with condition-based filtering.

## Source Files

| File | Purpose |
|------|---------|
| `download.go` | `Manager`: lists blobs, parses metadata, evaluates conditions, downloads applicable migration files to the layer directory |

## Test Files

| File | Tests |
|------|-------|
| `download_test.go` | Download tests: condition evaluation, blob filtering, metadata parsing, cross-layer condition handling |

## Running Tests

```bash
go test ./pkg/download/... -count=1
```

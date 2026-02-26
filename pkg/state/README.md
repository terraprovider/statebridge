# pkg/state

The state package provides OpenTofu state reading with caching and auto-init support.

## Source Files

| File | Purpose |
|------|---------|
| `reader.go` | `TofuStateReader`: reads OpenTofu state via `tofu show -json`, with built-in caching and auto-init on failure |
| `types.go` | State helper types and functions: `ResourceExists`, `ResourceAttributes`, module address matching |

## Test Files

| File | Tests |
|------|-------|
| `reader_test.go` | State reader tests with mock tofu binary |
| `types_test.go` | `ResourceExists` tests including module address matching and for_each instances |

## Running Tests

```bash
go test ./pkg/state/... -count=1
```

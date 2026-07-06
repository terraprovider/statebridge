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
| `reader_test.go` | `FlattenState`/`LookupResource` tests using synthetic state (no tofu binary needed) |
| `reader_init_test.go` | `TestRunInit_DoesNotLeakStateJSONAfterward`: regression test (real `tofu` binary, skipped if not on PATH) asserting the auto-init retry path doesn't leak the full state JSON to the writers configured for `tofu init`'s own output |
| `types_test.go` | `ResourceExists` tests including module address matching and for_each instances |

## Running Tests

```bash
go test ./pkg/state/... -count=1
```

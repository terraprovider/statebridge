# pkg/state

The state package provides OpenTofu state reading with caching and auto-init support.

## Source Files

| File | Purpose |
|------|---------|
| `reader.go` | `TofuStateReader`: reads OpenTofu state via `tofu show -json`, with built-in caching, auto-init on failure, and a `tofu state pull` fallback when `show -json` still fails (e.g. resource schema version mismatch after a provider upgrade) |
| `rawstate.go` | `parseRawState`: converts raw v4 state from `tofu state pull` into `*tfjson.State` without provider schemas; attribute values are returned as stored (not schema-upgraded) |
| `types.go` | State helper types and functions: `ResourceExists`, `ResourceAttributes`, module address matching |

## Test Files

| File | Tests |
|------|-------|
| `reader_test.go` | `FlattenState`/`LookupResource` tests using synthetic state (no tofu binary needed) |
| `reader_init_test.go` | `TestRunInit_DoesNotLeakStateJSONAfterward`: regression test (real `tofu` binary, skipped if not on PATH) asserting the auto-init retry path doesn't leak the full state JSON to the writers configured for `tofu init`'s own output |
| `rawstate_test.go` | `parseRawState` tests with synthetic raw state, plus `TestReadState_FallsBackToRawStateOnSchemaVersionMismatch` (real `tofu` binary, skipped if not on PATH) reproducing the `show -json` schema version mismatch |
| `types_test.go` | `ResourceExists` tests including module address matching and for_each instances |

## Running Tests

```bash
go test ./pkg/state/... -count=1
```

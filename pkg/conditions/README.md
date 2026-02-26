# pkg/conditions

The conditions package provides shared condition evaluation logic used by upload guard, download, and prune commands.

## Source Files

| File | Purpose |
|------|---------|
| `evaluate.go` | `EvaluateConditions`: checks `resources_exist`, `resources_not_exist`, `layer_exists`, `layer_not_exists` against state |

## Test Files

| File | Tests |
|------|-------|
| `evaluate_test.go` | Condition evaluation tests: resource existence, module addresses, layer directory checks, cross-layer conditions |

## Running Tests

```bash
go test ./pkg/conditions/... -count=1
```

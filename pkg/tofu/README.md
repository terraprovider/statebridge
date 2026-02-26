# pkg/tofu

The tofu package wraps OpenTofu CLI execution for targeted plans and migration file scanning.

## Source Files

| File | Purpose |
|------|---------|
| `runner.go` | `Runner`: scans `migration.*.tf` files for target addresses, executes `tofu plan` with `-target` flags via terraform-exec |

## Test Files

| File | Tests |
|------|-------|
| `runner_test.go` | Runner tests: target scanning, plan execution, flag handling |

## Running Tests

```bash
go test ./pkg/tofu/... -count=1
```

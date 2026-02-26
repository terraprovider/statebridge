# pkg/template

The template package provides Go template evaluation for key mappings and import ID expressions.

## Source Files

| File | Purpose |
|------|---------|
| `template.go` | Template evaluation logic: context variables (`.Address`, `.Key`, `.Attributes`, etc.) and execution |
| `funcs.go` | Custom template functions: string manipulation (`replace`, `trimPrefix`, `split`, `join`), regex, `sanitizeKey`, `formatKey`, `attr`, etc. |

## Test Files

| File | Tests |
|------|-------|
| `template_test.go` | Template evaluation tests covering all functions, context variables, and edge cases |

## Running Tests

```bash
go test ./pkg/template/... -count=1
```

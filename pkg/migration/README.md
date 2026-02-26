# pkg/migration

The migration package handles YAML migration file parsing, schema types, and validation.

## Source Files

| File | Purpose |
|------|---------|
| `schema.go` | Go types for the YAML migration schema: `MigrationFile`, `Operation`, `Resource`, `Condition`, etc. |
| `parser.go` | YAML parsing with strict unmarshalling |
| `validation.go` | Validation rules: required fields, mutual exclusivity, address format, template placement |

## Test Files

| File | Tests |
|------|-------|
| `parser_test.go` | YAML parsing tests including edge cases and error handling |
| `validation_test.go` | Comprehensive table-driven validation tests covering all rules |

## Running Tests

```bash
go test ./pkg/migration/... -count=1
```

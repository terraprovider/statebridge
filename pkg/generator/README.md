# pkg/generator

The generator package renders HCL blocks (`import`, `moved`, `removed`) and writes them to `.tf` files with embedded metadata.

## Source Files

| File | Purpose |
|------|---------|
| `generator.go` | Core block types (`ImportBlock`, `MovedBlock`, `RemovedBlock`) and rendering logic |
| `import_block.go` | `import` block HCL rendering |
| `moved_block.go` | `moved` block HCL rendering |
| `removed_block.go` | `removed` block HCL rendering |
| `metadata.go` | Migration metadata type (version, source_layer, conditions, resources), JSON render/parse, address extraction, condition inference/merge, source-layer `Matches`/`OwnedByOther` scoping helpers |
| `writer.go` | File output: deterministic block sorting, content-addressed filenames, metadata embedding, source-layer resolver stamping |

## Test Files

| File | Tests |
|------|-------|
| `generator_test.go` | Block rendering tests for import, moved, and removed blocks |
| `metadata_test.go` | Metadata serialization, parsing, condition inference, address extraction |

## Running Tests

```bash
go test ./pkg/generator/... -count=1
```

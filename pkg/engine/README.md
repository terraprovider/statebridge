# pkg/engine

The engine package is the orchestration core of tfmigrate. It reads migration YAML files, resolves resource addresses against OpenTofu state, and delegates HCL block generation to the `generator` package.

## Source Files

| File | Purpose |
|------|---------|
| `engine.go` | `ProcessFiles` pipeline: parse YAML → evaluate conditions → resolve state → generate HCL blocks → write output |
| `consolidate.go` | Module-level `removed` block consolidation (collapses individual removals into a single `module.X` removal) |
| `keymatcher.go` | Key pattern matching for `for_each` resources: exact, prefix (`prefix_*`), and catch-all (`*`) patterns |
| `resolver.go` | Import ID resolution and state lookups; interfaces with `pkg/state` to discover managed resources |
| `tracker.go` | Cross-operation key tracking and completeness checking (ensures all `for_each` keys are covered) |

## Test Files

Tests are split by topic for readability. All files are in `package engine` and share helpers from `engine_helpers_test.go`.

| File | Tests |
|------|-------|
| `engine_helpers_test.go` | Shared test helpers: `findLayerFile`, `readLayerFile` |
| `engine_move_test.go` | Simple move operations: single resource, for_each (all keys), multi-operation same layer, destination address override |
| `engine_keyed_move_test.go` | Keyed moves: explicit key mapping, address prefix, split across operations, incomplete coverage error |
| `engine_module_move_test.go` | Module-level moves: basic, with destination address, address prefix, nested modules, for_each modules, no-resources error, nested rename |
| `engine_all_resources_test.go` | `all_resources: true` moves: basic, with override, for_each, module consolidation, empty state, omit, omit+destroy, omit+override, import ID override |
| `engine_rename_test.go` | Rename operations: basic, multiple renames, with address prefix |
| `engine_remove_test.go` | Remove operations: basic, multiple addresses, destroy overrides |
| `engine_import_test.go` | Import operations: basic, operation-level provider |
| `engine_conditions_test.go` | Condition evaluation: resources_exist, resources_not_exist, state errors, partial/full skip, layer_exists, layer_not_exists |
| `engine_lifecycle_test.go` | Lifecycle & misc: validation errors, dry-run, status:retired, layer auto-skip, auto-skip (rename/remove/import/mixed), strict mode |
| `engine_same_layer_move_test.go` | Same-layer moves: simple, identity skip, for_each, keyed, prefix patterns, module rename, module identity no-op, merge_duplicates error, source/destination prefix, same-layer with prefixes |
| `consolidate_test.go` | Unit tests for module consolidation logic |
| `keymatcher_test.go` | Unit tests for key pattern matching |
| `resolver_test.go` | Unit tests for state resolution |
| `tracker_test.go` | Unit tests for key tracking |

## Running Tests

```bash
go test ./pkg/engine/... -count=1
```

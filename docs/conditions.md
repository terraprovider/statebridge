# Conditions

Conditions control whether a generated migration file is applied at download time. They are evaluated against the layer's current Terraform state.

## Auto-Inferred Conditions

By default, conditions are automatically inferred from the block types in each generated `.tf` file:

| Block type | Inferred condition | Rationale |
|------------|-------------------|-----------|
| `removed`  | `resources_exist` for `from` address | Skip if resource already gone |
| `import`   | `resources_not_exist` for `to` address | Skip if resource already imported |
| `moved`    | `resources_exist` for `from` AND `resources_not_exist` for `to` | Skip if rename already applied |

This makes all migrations **idempotent by default** — safe to re-run even after partial completion. For cross-layer moves (which decompose into `removed` + `import` blocks), each layer's file gets the correct condition automatically.

## Explicit Conditions

Migration files support an optional `condition` block that is merged (additively) with inferred conditions. Use this for cross-layer checks or custom logic that cannot be derived from the operations:

```yaml
description: "Move web server to app layer"
condition:
  resources_exist:
    - layer: "./layers/compute"
      addresses:
        - "aws_instance.web"
  resources_not_exist:
    - layer: "./layers/app"
      addresses:
        - "aws_instance.web"
operations:
  - type: move
    source_layer: "./layers/compute"
    destination_layer: "./layers/app"
    resources:
      - from: "aws_instance.web"
```

## Condition Types

| Type | Behavior |
|------|----------|
| `resources_exist` | ALL listed addresses must be found in the layer's state |
| `resources_not_exist` | NONE of the listed addresses must be found in the layer's state |

All condition checks are ANDed — every check must pass for the migration to proceed.

### `layer_exists` / `layer_not_exists`

Check directory existence without reading state (much cheaper than `resources_exist`):

```yaml
condition:
  layer_exists:
    - "./layers/source"              # all paths must exist on disk
  layer_not_exists:
    - "./layers/deprecated"          # none of the paths may exist
```

Useful for skipping migrations after a source layer has been intentionally deleted.

## Address Matching

- A base address (e.g., `aws_instance.web`) matches if **any** for_each instance exists in state
- A fully-qualified address (e.g., `aws_instance.web["key"]`) matches only that specific instance

## Condition Evaluation

Conditions are evaluated at two points:

1. **Download time** (`statebridge download`): Conditions embedded in migration metadata are evaluated against the layer's state. Only migrations whose conditions pass are written to disk.
2. **Upload guard** (`statebridge upload` / `generate --upload`): Existing blobs are checked — if their conditions still pass, the migration is considered "active" and overwriting is refused (unless `--force` is used).

For `layer == "."`, the layer's own state is read and checked. For cross-layer conditions, the download command warns and treats them as met (state of other layers is not available during per-layer download).

# Keyed Moves

For `for_each` resources, use the `keys` map to specify how individual state keys are routed to destination keys. This enables fine-grained control over key renaming and splitting resources across layers.

## Basic Usage

```yaml
- type: move
  source_layer: "./layers/old"
  destination_layer: "./layers/new"
  resources:
    - from: "azuread_access_package_catalog.all"
      keys:
        mrt_customer: customer_approval                  # exact key rename
        mrt_outbound_provisioning: resource_tenant_access
        mrt_privileged_access: privileged_access
        mrt_vaw: vaw
```

## Key Pattern Types

| Pattern | Meaning | Example |
|---|---|---|
| `exact_key` | Matches exactly that for_each key | `mrt_customer: customer_approval` |
| `prefix_*` | Matches all keys starting with `prefix_` | `"mrt_customer_*": '{{ .Key \| trimPrefix "mrt_customer_" }}'` |
| `*` | Catch-all: matches all remaining unmatched keys | `"*": '{{ .Key }}'` |

Values can be literal strings or [Go template expressions](templates.md). Match priority: exact > longest prefix > catch-all.

## Completeness Rules

- When `keys` is present, **all state keys** must be matched. Unmatched keys cause an error.
- Overlapping key claims across operations cause an error.
- The same source resource can appear in multiple move operations with different destination layers. Keys are tracked across operations.

## Cross-Operation Key Splitting

Split a `for_each` resource by key prefix, routing different keys to different destination layers:

```yaml
operations:
  - type: move
    source_layer: "./layers/shared"
    destination_layer: "./layers/engineering"
    resources:
      - from: "aws_resource.assignments"
        keys:
          "eng_*": '{{ .Key | trimPrefix "eng_" }}'
  - type: move
    source_layer: "./layers/shared"
    destination_layer: "./layers/finance"
    resources:
      - from: "aws_resource.assignments"
        keys:
          "fin_*": '{{ .Key | trimPrefix "fin_" }}'
```

All keys in the source state must be collectively covered by the operations.

## Without `keys` Map

When `keys` is omitted:
- **Single resource**: one `removed` + one `import` block
- **For_each resource**: expands all instances with the same keys

## Merging Duplicate Destination Addresses

When multiple source resources with keyed moves produce the same destination address (same `to` resource + same destination key), this normally results in an error because Terraform rejects duplicate `import` blocks.

The `merge_duplicates: true` flag on a resource enables destination-side deduplication:

```yaml
- type: move
  source_layer: "./layers/old"
  destination_layer: "./layers/new"
  resources:
    - from: "azurerm_role_management_policy.permanent_active"
      to: "azurerm_role_management_policy.all"
      merge_duplicates: true
      keys:
        key_a: shared_key
        key_b: unique_active
    - from: "azurerm_role_management_policy.permanent_eligible"
      to: "azurerm_role_management_policy.all"
      merge_duplicates: true
      keys:
        key_x: shared_key
        key_y: unique_eligible
```

In this example, both resources map a key to `shared_key` on the same destination resource. Without `merge_duplicates`, this would produce two import blocks for `azurerm_role_management_policy.all["shared_key"]` — a Terraform error.

With `merge_duplicates: true`:
- The **first** import block for a destination address wins
- Subsequent duplicates with **matching import IDs** are silently skipped
- If import IDs **differ**, an error is raised (the merge is ambiguous)
- Both resources involved in the collision must have `merge_duplicates: true`

### Constraints

- `merge_duplicates` requires `keys` to be present (keyed move only)
- Not valid on module-level moves
- Not valid on `all_resources` overrides
- For cross-layer moves, import IDs must match (error if they differ)
- For same-layer moves, duplicates targeting the same destination are always compatible (first `moved` block wins, when `use_moved_blocks: true` which is the default)
- Scoped to a single migration YAML file (not cross-file)

## Same-Layer Keyed Moves

When `source_layer` and `destination_layer` are the same, keyed moves generate `moved` blocks by default instead of `removed` + `import` blocks. This is useful for re-keying `for_each` resources in place. Set `use_moved_blocks: false` on the operation or individual resources to force `removed` + `import` instead (see [Same-Layer Moves](migration-format.md#same-layer-moves)).

```yaml
- type: move
  source_layer: "./layers/app"
  destination_layer: "./layers/app"
  resources:
    - from: "aws_instance.web"
      keys:
        old_key_1: new_key_1
        old_key_2: new_key_2
```

Generates:
```hcl
moved {
  from = aws_instance.web["old_key_1"]
  to   = aws_instance.web["new_key_1"]
}
moved {
  from = aws_instance.web["old_key_2"]
  to   = aws_instance.web["new_key_2"]
}
```

Identity moves (where the source and destination address are the same, e.g., `old_key: old_key`) are silently skipped.

You can also use `source_prefix` and `destination_prefix` to change the module path during a same-layer keyed move:

```yaml
- type: move
  source_layer: "./layers/app"
  destination_layer: "./layers/app"
  source_prefix: "module.v1"
  destination_prefix: "module.v2"
  resources:
    - from: "aws_instance.web"
      keys:
        key1: key1
```

Generates: `moved { from = module.v1.aws_instance.web["key1"]; to = module.v2.aws_instance.web["key1"] }`

## Real-World Example

This example moves 3 resource types between layers with exact key renames, prefix patterns, and `address_prefix`:

```yaml
description: "Identity Governance - Restructuring"
operations:
  - type: move
    source_layer: ./blueprints/41-workplace
    destination_layer: ./blueprints/61-identity-governance
    address_prefix: module.identity_governance
    resources:
      - from: azuread_access_package_catalog.all
        keys:
          mrt_customer: customer_approval
          mrt_outbound_provisioning: resource_tenant_access
          mrt_privileged_access: privileged_access
          mrt_vaw: vaw

      - from: azuread_access_package.all
        keys:
          "mrt_customer_*": 'customer_approval_{{ .Key | trimPrefix "mrt_customer_" }}'
          "mrt_privileged_access_*": 'privileged_access_{{ .Key | trimPrefix "mrt_privileged_access_" }}'
          "mrt_outbound_provisioning_*": 'resource_tenant_access_{{ .Key | trimPrefix "mrt_outbound_provisioning_" }}'
          vaw_access: vaw_access

      - from: azuread_access_package_resource_package_association.all
        keys:
          "mrt_customer_*": 'customer_approval_{{ .Key | trimPrefix "mrt_customer_" | split "_entra_group_" | at 0 }}_AadGroup_{{ .Attributes.catalog_resource_association_id | split "/" | at 1 }}'
          "mrt_privileged_access_*": 'privileged_access_{{ .Key | trimPrefix "mrt_privileged_access_" | split "_entra_group_" | at 0 }}_AadGroup_{{ .Attributes.catalog_resource_association_id | split "/" | at 1 }}'
          "mrt_outbound_provisioning_*": 'resource_tenant_access_{{ .Key | trimPrefix "mrt_outbound_provisioning_" | split "_entra_group_" | at 0 }}_AadGroup_{{ .Attributes.catalog_resource_association_id | split "/" | at 1 }}'
```

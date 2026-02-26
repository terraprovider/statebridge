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

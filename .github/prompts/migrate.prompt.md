---
description: "Generate a tfmigrate YAML migration file from a natural language description of resource moves, renames, imports, or removals"
agent: "agent"
argument-hint: "Describe the migration (e.g., Move azurerm_resource_group.main from ./layers/shared to ./layers/app)"
---

Generate a tfmigrate migration YAML file based on the user's description.

Follow the YAML schema and translation patterns documented in [AGENTS.md](../../AGENTS.md).

Before generating, determine:
1. **Operation type** — move, rename, remove, or import
2. **Layer paths** — which Terraform root modules are involved
3. **Resource addresses** — full Terraform addresses (e.g., `module.vpc.aws_subnet.main`)
4. **Key mapping** — for for_each resources, whether keys change
5. **Import IDs** — whether known or auto-resolved from state

Place the file in `migrations/` with the next available numeric prefix (e.g., `migrations/004_description.yaml`).

Always include a `description` field. Only add explicit `condition` blocks when cross-layer checks are needed — auto-inferred conditions handle idempotency by default.

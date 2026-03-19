# e2e — End-to-End Tests

End-to-end tests exercise the full statebridge pipeline against real or local OpenTofu resources.

## Test Suites

### Fast E2E Tests (`e2e_fast` build tag)

Use local-only providers (`null`, `random`, `terraform_data`) with a local backend — no cloud credentials needed. Run in CI on every PR.

```bash
go test -tags=e2e_fast -v -timeout=10m -count=1 ./e2e/...
```

| File | Tests |
|------|-------|
| `fast_e2e_move_test.go` | Move operations: simple move, module move, all_resources with overrides/omit, key pattern move, keyed move, address rename, address prefix, for_each without keys, split for_each to multiple layers |
| `fast_e2e_ops_test.go` | Other operations: rename, remove+import, import with template ID |
| `fast_e2e_conditions_test.go` | Condition handling: condition skip, layer_exists/layer_not_exists, retired status, idempotency, multi-file migration |
| `fast_e2e_same_layer_test.go` | Same-layer moves and prefix features: same-layer rename, keyed move, module move, source/destination prefix (cross-layer and same-layer) |
| `fast_e2e_storage_test.go` | Storage integration: upload/download round-trip, upload guard, download condition skip, prune |
| `fast_helpers_test.go` | Shared helpers: `setupTestProject`, `processFiles`, `tofuInit`, `tofuApply`, `tofuPlan`, `assertFileContains`, mock storage helpers |

### Full E2E Tests (`e2e` build tag)

Require real Azure resources and credentials. Gated behind `ARM_SUBSCRIPTION_ID`.

```bash
ARM_CLIENT_ID=... ARM_TENANT_ID=... ARM_SUBSCRIPTION_ID=... ARM_USE_OIDC=true \
  go test -tags=e2e -v -timeout=30m -count=1 ./e2e/...
```

| File | Tests |
|------|-------|
| `e2e_test.go` | Full pipeline tests against Azure: move, keyed move, rename, remove+import, condition skip, upload/download |
| `helpers_test.go` | Azure test helpers: resource provisioning, blob container lifecycle, credential setup |

## Test Project

The `fastproject/` directory contains a static Terraform project with 3 layers (`shared`, `app`, `networking`) using only local providers. The `testproject/` directory contains an Azure-based project for full E2E tests.

---
description: "Use when writing or modifying Go test files in tfmigrate. Covers table-driven test patterns, test helpers from internal/testutil/, and build tag conventions."
applyTo: "**/*_test.go"
---

# Go Test Conventions

## Table-driven tests
Always use table-driven tests with `t.Run()` subtests:

```go
tests := []struct {
    name string
    // ... fields
}{
    {name: "descriptive case name", ...},
}
for _, tt := range tests {
    t.Run(tt.name, func(t *testing.T) {
        // test body
    })
}
```

## Test helpers (`internal/testutil/`)
Use the project's test helpers — no external test frameworks (no testify, no gomega):

- `testutil.SetupLayers(t, "app", "shared")` — create temp layer directories
- `testutil.WriteMigration(t, dir, "001_test.yaml", yaml)` — write migration fixture
- `testutil.ReadLayerFile(t, outputFiles, layerPath)` — read generated output
- `testutil.AssertContains(t, content, "import {")` — substring check
- `testutil.AssertNotContains(t, content, "removed {")` — negative check
- `testutil.AssertBlockCount(t, content, "import {", 3)` — count HCL blocks
- `testutil.RequireOutputCount(t, outputFiles, 2)` — verify file count

## Mock state
Use `testutil.BuildState()` to create mock Terraform state for engine tests. See `internal/testutil/state.go`.

## Build tags
- No tag → unit tests (default `go test ./...`)
- `//go:build e2e_fast` → fast E2E (Azure auth needed, no real infra)
- `//go:build e2e` → full E2E (Azure auth + OpenTofu + real resources)

## Error assertions
Use `t.Fatalf` for setup failures, `t.Errorf` for assertion failures. Always call `t.Helper()` in helper functions.

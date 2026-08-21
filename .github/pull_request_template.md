## What this changes

## Why

## Checklist

- [ ] `make fmt-check vet test` passes
- [ ] `make race` passes if the change touches concurrency
- [ ] New behaviour has a test that fails without the change
- [ ] A new operation is registered in `internal/ops` with its risk class and scope
- [ ] No MCP tool gained an `apply`, `confirm` or `force` parameter
- [ ] Documentation updated if the JSON contract, exit codes or safety model changed

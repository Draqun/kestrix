# Proposal: remove-legacy-model-shim

## Why

`internal/model/model.go` is a transitional type-alias shim (`type Project = domain.Project`, `type Task = domain.Task`, etc.) introduced during the hexagonal architecture refactoring. Although its package comment states that new code should import `internal/domain` directly, 53 Go files across `cmd/` and `internal/` still import `kestrix/internal/model`, and 44 of those files import both `internal/model` and `internal/domain` simultaneously. Keeping two package names for the same domain entities creates cognitive overhead and weakens hexagonal boundary enforcement.

## What Changes

- Replace all `kestrix/internal/model` imports and `model.*` type/constant references across `cmd/` and `internal/` with `kestrix/internal/domain` and `domain.*`.
- Delete `internal/model/model.go` and remove the `internal/model` package directory.
- Extend the architectural boundary test in `internal/domain/portscheck/portscheck_test.go` to fail if any package in the module imports `kestrix/internal/model`.
- Update `docs/architecture.md` and `docs/be.md` to reference `internal/domain` exclusively.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `hexagonal-ports`: removes the transitional `internal/model` alias shim and enforces `internal/domain` as the single canonical package for domain entities and ports.

## Non-goals

- Changing JSON serialization tags, database schemas, or HCL syntax of any domain struct (`Project`, `Task`, `Repo`, `Variable`).
- Renaming exported types within `internal/domain`.

## Impact

- Updates imports and type qualifiers in 53 Go files across `cmd/kestrix/` and `internal/`.
- Deletes `internal/model/model.go`.
- Updates `internal/domain/portscheck/portscheck_test.go`, `docs/architecture.md`, and `docs/be.md`.

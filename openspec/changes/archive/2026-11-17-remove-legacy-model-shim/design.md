# Design: remove-legacy-model-shim

## Context

During the hexagonal refactor, core pipeline structs (`Repo`, `ExecuteSpec`, `Task`, `Project`, `ProjectFile`, `Variable`, `AcquiredSource`) were moved into `internal/domain/types.go`, while `internal/model/model.go` was left behind as type aliases (`type Project = domain.Project`). This left 53 files referencing `model.*` and 44 files importing both packages.

```
Before:
  cmd/*, internal/web, internal/service, internal/store, internal/parser, internal/dag
       |                                        |
       +---> internal/model (type aliases) -----+---> internal/domain (canonical types & ports)

After:
  cmd/*, internal/web, internal/service, internal/store, internal/parser, internal/dag
       |
       +--------------------------------------------> internal/domain (canonical types & ports)
                                                      (enforced by internal/domain/portscheck)
```

## Goals / Non-Goals

**Goals:**
- Eliminate `internal/model` completely so `internal/domain` is the sole source of domain types and ports.
- Prevent regression via `internal/domain/portscheck/portscheck_test.go`.

**Non-Goals:**
- Modifying domain struct fields, methods, or JSON tags.

## Security Considerations

- Purely a compile-time package consolidation; no runtime behavior, authentication, secret redaction, or runner isolation logic changes.
- Enforcing a single domain package via `portscheck_test.go` ensures future code cannot bypass domain-level invariants via a parallel model package.

## Decisions

### D1: Direct mechanical replacement across all packages in one atomic change
- **Choice:** Rewrite `model.` qualifiers to `domain.` across `cmd/` and `internal/` and delete `internal/model/model.go` in a single atomic change.
- **Alternatives considered:** Gradual file-by-file migration. Rejected because type aliases allow silent re-introduction of `internal/model` imports until the package is deleted.

### D2: Enforce removal in `portscheck_test.go`
- **Choice:** Add an assertion in `internal/domain/portscheck/portscheck_test.go` checking `go list ./...` imports so that `kestrix/internal/model` is forbidden everywhere.
- **Alternatives considered:** Relying on package deletion alone. Adding the check documents the architectural rule and prevents recreating `internal/model`.

## Risks / Trade-offs

- **[Risk]** Local variable shadowing where `domain` is used as a local identifier in tests or helpers.
  → *Mitigation:* Rename any conflicting local variable to `dom` or `domainName` during migration and verify with `go test ./...`.

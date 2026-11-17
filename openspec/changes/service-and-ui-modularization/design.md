# Design: service-and-ui-modularization

## Context

`internal/service/service.go` grew to 1,572 lines as features (SSRF protection, run cancellation, remote runner job resumption, manual approvals, and webhook dispatching) accumulated in one file. Similarly, `ui/src/pages/Pipelines.jsx` grew to 882 lines handling both the runs list/filtering and the detailed run execution inspector.

```
internal/service/
  ├── project_service.go      (ProjectService CRUD + HCL validation)
  ├── repo_url_validation.go  (ValidateRepoURL + SSRF/IPv4 blocks)
  ├── run_service.go          (RunService orchestration, cancel, remote resume)
  ├── webhook_service.go      (WebhookService event classification + trigger)
  └── var_store.go            (BuildVarStore + shared helpers)

ui/src/
  ├── pages/Pipelines.jsx                     (page state, SSE wiring, layout)
  └── components/pipelines/
        ├── RunDetailHeader.jsx               (run metadata, cancel/rerun actions)
        └── TaskLogPanel.jsx                  (task status list, manual gate, log output)
```

## Goals / Non-Goals

**Goals:**
- Keep every file in `internal/service/` and `ui/src/pages/Pipelines.jsx` focused on a single cohesive concern (<500 lines per file).
- Preserve 100% statement coverage in `internal/service` and passing Vitest suite in `ui/`.

**Non-Goals:**
- Introducing new packages or changing public APIs.
- Altering React state semantics or Rules of Hooks compliance.

## Security Considerations

- `repo_url_validation.go` isolates SSRF and private IPv4/loopback/metadata blocking logic into an dedicated, easily auditable file without changing any validation rules.
- No changes to authentication, secret redaction, or runner execution boundaries.

## Decisions

### D1: Keep a single `service` Go package split across domain-aligned files
- **Choice:** Split `internal/service/service.go` into `project_service.go`, `repo_url_validation.go`, `run_service.go`, `webhook_service.go`, and `var_store.go` inside package `service`.
- **Alternatives considered:** Sub-packages (`internal/service/run`, `internal/service/project`). Rejected because `WebhookService` coordinates with `RunService` and `BuildVarStore`, and keeping package `service` avoids breaking callers in `cmd/` and `internal/web/`.

### D2: Extract stateless/presentational subcomponents from `Pipelines.jsx`
- **Choice:** Extract run header actions and task log/manual-approval panel into `ui/src/components/pipelines/`, passing callbacks via props and keeping hooks at the top level of each component.
- **Alternatives considered:** Global Redux/Zustand store. Rejected as unnecessary complexity for page-scoped state.

## Risks / Trade-offs

- **[Risk]** Merge conflicts with in-flight branches touching `internal/service/service.go` or `ui/src/pages/Pipelines.jsx`.
  → *Mitigation:* Perform the split as a pure structural refactoring commit with zero behavioral changes.

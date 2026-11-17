# Proposal: service-and-ui-modularization

## Why

Two critical source files have grown into monolithic files that mix distinct responsibilities:

1. `internal/service/service.go` (1,572 lines) combines `ProjectService`, SSRF/IPv4/URL validation helpers, `RunService` (async execution, cancellation, remote runner resumption, manual task approval), `WebhookService`, and `BuildVarStore` in a single file.
2. `ui/src/pages/Pipelines.jsx` (882 lines) combines project/run filtering, SSE event subscriptions, DAG visualization, terminal log viewer, manual gate approvals, run cancellation, and single-task re-runs in a single React component file.

Splitting these files into cohesive units improves maintainability, code review clarity, and navigation without altering runtime behavior.

## What Changes

- Split `internal/service/service.go` into focused files within `internal/service/`:
  - `project_service.go` (`ProjectService` CRUD + validation)
  - `repo_url_validation.go` (`ValidateRepoURL`, private IPv4/host checks)
  - `run_service.go` (`RunService` start, cancel, manual approval, remote task completion)
  - `webhook_service.go` (`WebhookService` push/MR processing and dispatch)
  - `var_store.go` (`BuildVarStore` and shared helpers)
- Extract cohesive subcomponents from `ui/src/pages/Pipelines.jsx` (such as run detail header/actions and task log viewer panel) into `ui/src/components/pipelines/` while preserving all existing UI behavior and Vitest coverage.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `service-layer`: requires cohesive per-concern file organization in `internal/service` and modular UI subcomponents for pipeline run inspection while maintaining test coverage gates.

## Non-goals

- Changing any exported Go function/method signatures in `internal/service`.
- Redesigning the visual layout, user workflow, or API calls of the Pipelines page.
- Splitting `internal/service` into multiple separate Go packages (which would require cross-package wiring changes).

## Impact

- Replaces `internal/service/service.go` with focused files in `internal/service/` and corresponding test files.
- Refactors `ui/src/pages/Pipelines.jsx` and adds subcomponents under `ui/src/components/pipelines/`.

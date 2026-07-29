## Why

The application has a 1731-line god file (`server.go`), triple-duplicated var-store construction (~120 lines), two parallel `TaskResult` types, an 11-site `*PGStore` type-assertion bypass of the `Store` interface, and a `webhookLog` data race. The hexagonal refactor establishes ports (interfaces) and adapters (implementations) so each layer is testable in isolation and dependencies point inward toward the domain.

## What Changes

- Extract **domain ports** (interfaces) in a new `internal/domain` package: `ProjectStore`, `RunStore`, `PipelineRunner`, `WebhookLogSink`
- Extract **service layer** in `internal/service`: `ProjectService`, `RunService`, `WebhookService` — business logic without HTTP
- Split `server.go` into thin **HTTP adapters** (`internal/web/handlers/*.go`), one file per handler group
- Merge `runner.TaskResult` + `store.TaskResult` into one `domain.TaskResult`
- Replace `store.PipelineSnapshot` reuse with `model.Project` + JSON tags
- Split `store/pgstore.go` into `projectstore` + `runstore`
- Add **error helper** (`writeError`) and **pagination package** (`internal/web/pagination`)
- Protect `webhookLog` with a mutex
- Apply **TDD** throughout — write tests first (red), implement to pass (green), then refactor
- Add **CI coverage gate** — pipeline fails if total coverage drops below 80%; per-package thresholds for `domain` (≥90%) and `service` (≥90%)

## Capabilities

### New Capabilities
- `hexagonal-ports`: domain interfaces (ProjectStore, RunStore, PipelineRunner, WebhookLogSink)
- `service-layer`: business logic extracted from handlers into services
- `http-adapter-split`: server.go decomposed into per-group handler files

### Modified Capabilities
*(none — existing specs unchanged behaviorally)*

## Non-goals
- No new features; behavior must remain identical
- No database schema changes
- No frontend changes (this is backend only)
- No migration framework (separate change)

## Impact

- **New packages**: `internal/domain`, `internal/service`, `internal/web/pagination`
- **Heavily modified**: `internal/web/server.go` (shrinks from ~1731 to ~200 lines), `internal/store/pgstore.go` (splits in two), `internal/runner/runner.go` (extract `Executor` interface)
- **Deleted**: `store.PipelineSnapshot`, `store.SnapshotFromProject`, `runner.TaskResult` (replaced by `domain.TaskResult`)
- **CI**: `scripts/test.sh` and `.gitlab-ci.yml` updated with coverage threshold enforcement
- **Risk**: High — touches every HTTP handler; mitigated by TDD green test suite (302+ tests) serving as regression safety net
- **TDD**: Every phase writes tests first (red → green → refactor); existing tests serve as regression baseline
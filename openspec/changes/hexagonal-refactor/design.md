## Context

Current architecture is layered but leaky: `internal/web/server.go` (1731 lines, 25 handlers) holds business logic, HTTP transport, goroutine orchestration, and persistence wiring. 11 `*PGStore` type assertions bypass the `Store` interface. Two parallel `TaskResult` types require field-by-field conversion. `store.PipelineSnapshot` duplicates `model.Project`. Var-store construction is triplicated (~120 lines). `webhookLog` has no mutex.

## Architecture (Target)

```
cmd/kestrix (composition root)
    │
    ▼
internal/domain (ports: interfaces + shared types)
    │  ProjectStore · RunStore · PipelineRunner · WebhookLogSink
    │  TaskResult · RunSummary · Project (from model)
    ▼
internal/service (use cases: business logic, no HTTP)
    │  ProjectService · RunService · WebhookService
    ▼
internal/web (HTTP adapters: thin handlers, JSON encode/decode only)
    ├── handlers/project.go    handlers/run.go    handlers/pipeline.go
    ├── handlers/webhook.go    handlers/system.go
    ├── pagination/             webhook_verify.go   generator.go
    ▼
internal/store (adapters: PGProjectStore, PGRunStore, FileProjectStore)
internal/runner (adapters: LocalExecutor, DockerExecutor, GitLabRunnerExecutor)
```

Dependency rule: **arrows point inward**. `domain` imports nothing. `service` imports `domain`. `web`, `store`, `runner` import `service` + `domain`. `cmd/kestrix` wires concrete adapters into service constructors.

## Goals / Non-Goals

**Goals:**
- `domain` package has zero internal dependencies — pure ports
- `service` package holds all business logic — no `net/http` imports
- `web/handlers` files are thin (≤50 lines each), no business logic
- Single `domain.TaskResult` type — no more field-by-field conversion
- `Store` interface replaced by `domain.ProjectStore` + `domain.RunStore` — no type assertions
- `webhookLog` protected by mutex
- All existing 302 tests still pass; behavior unchanged
- **TDD**: every phase follows red → green → refactor; tests written before implementation
- **Coverage**: ≥80% total, ≥90% for `domain`, ≥90% for `service`, ≥85% per `web/handlers` file
- **Race detection**: `go test -race` passes on every phase boundary

**Non-Goals:**
- No frontend changes
- No DB schema changes or migration tooling
- No new features
- No cancellation of running processes (separate change)

## Decisions

| # | Decision | Rationale |
|---|----------|-----------|
| 1 | `internal/domain` holds interfaces + shared types | Hexagonal "ports" — pure abstractions, no deps |
| 2 | `internal/service` holds business logic | Use-case layer; testable without HTTP or DB |
| 3 | `Store` interface splits into `ProjectStore` + `RunStore` | FileStore genuinely can't do runs; the split makes this honest instead of hidden behind type assertions |
| 4 | `domain.TaskResult` merges `runner.TaskResult` + `store.TaskResult` | One struct, JSON tags; eliminates 4 conversion sites |
| 5 | `store.PipelineSnapshot` → reuse `model.Project` with JSON tags | Eliminates parallel model; `SnapshotFromProject` becomes a no-op |
| 6 | `runner.Executor` interface with `Local`/`Docker`/`GitLabRunner` implementations | Eliminates ~150 lines duplicated task-loop body |
| 7 | `web/pagination` package extracted from `server.go` | Reusable, testable in isolation |
| 8 | `buildVarStore()` helper in `service` | Eliminates 3× duplication (~120 lines) |
| 9 | `webhookLog` → `sync.RWMutex` protected | Fixes data race |
| 10 | Handler files split by domain: `project.go`, `run.go`, `pipeline.go`, `webhook.go`, `system.go` | Each ≤200 lines; one responsibility per file |
| 11 | TDD cycle per phase: write tests → implement → refactor | Tests written first ensure the contract is correct before the adapter code; prevents "tests that chase the implementation" |
| 12 | CI coverage gate: `go test -coverprofile` + threshold check in `scripts/test.sh` | Prevents coverage regression; enforces accountability at merge time |
| 13 | Hand-rolled mocks for ports (no `gomock` dependency) | `domain` includes `mocks_test.go` with simple struct mocks implementing `ProjectStore`, `RunStore`, `PipelineRunner`; keeps zero-dep philosophy |
| 14 | Compile-time port assertions: `var _ domain.ProjectStore = (*PGProjectStore)(nil)` | Catches interface drift at build time, not at runtime |
| 15 | Race detection in CI: `go test -race -count=1` | Catches the `webhookLog` data race and any new concurrency bugs introduced during refactor |

## Phasing

The refactor is split into **5 phases** (see tasks.md), each independently buildable + testable:

1. **Phase 1 — Domain types**: Extract `domain.TaskResult`, eliminate `store.PipelineSnapshot`. Non-behavioral; pure type moves. TDD: round-trip JSON tests written first.
2. **Phase 2 — Ports + Store split**: Define `domain.ProjectStore` + `domain.RunStore`. Split `pgstore.go`. Remove type assertions. TDD: compile-time assertions + mock implementations written first.
3. **Phase 3 — Service layer**: Extract `ProjectService`, `RunService`, `WebhookService`, `buildVarStore`. Handlers become thin. TDD: service unit tests with mocks written first.
4. **Phase 4 — Runner interface + cleanup**: Extract `Executor` interface, deduplicate task-loop. Extract `pagination` package, `writeError` helper, `webhookLog` mutex. TDD: executor tests + race detection.
5. **Phase 5 — CI pipeline**: Add coverage threshold enforcement to `scripts/test.sh` and `.gitlab-ci.yml`. Race detection in CI.

Each phase: `go build && go test ./... -count=1` must pass before starting the next. Phases 1–4 additionally require `go test -race -count=1`.

## TDD Approach

Each phase follows the **Red → Green → Refactor** cycle:

1. **Red**: Write failing tests that express the desired behavior. For Phase 1, this is a JSON round-trip test for `domain.TaskResult`. For Phase 2, it's a compile-time interface assertion test. For Phase 3, it's service unit tests with mock stores that assert the business logic contract.
2. **Green**: Write the minimum code to make tests pass. No extra abstractions, no speculative interfaces.
3. **Refactor**: Improve the code (extract helpers, rename, simplify) while keeping tests green.

**Mocking strategy**: `internal/domain` includes `mocks_test.go` with hand-rolled struct mocks implementing each port interface. Each mock records calls and returns configurable values. This keeps the `domain` package self-testing with zero external dependencies — no `gomock`, no `testify/mock`, just `struct` + `interface`.

**Handler tests**: Use `httptest.NewRecorder` + real services backed by in-memory or temp-dir stores. Verifies the full HTTP→service→store stack without PostgreSQL.

**Regression safety net**: The existing 302 tests in `internal/web/*_test.go` are the regression baseline. They must pass unchanged after every phase. If a test's imports change (e.g. `store.TaskResult` → `domain.TaskResult`), that is mechanical churn, not a behavioral change.

## Coverage Strategy

**Per-package targets:**

| Package | Target | Rationale |
|---------|--------|-----------|
| `internal/domain` | ≥90% | Pure types + interfaces; easy to cover fully |
| `internal/service` | ≥90% | Business logic; mock-based tests make branches trivial to hit |
| `internal/web/handlers` | ≥85% | HTTP adapters; some error paths need specific store states |
| `internal/store` | ≥80% | DB code can't be unit-tested without PG; FileStore paths covered |
| `internal/runner` | ≥80% | OS/exec integration; skip Docker/GitLabRunner in `-short` |
| **Total** | **≥80%** | Enforced by CI pipeline |

**CI enforcement (Phase 5):**
- `scripts/test.sh`: runs `go test ./... -coverprofile=coverage.out -covermode=atomic -race -count=1`
- Parses `go tool cover -func=coverage.out` output
- Exits non-zero if total < 80% or any package below its target
- `.gitlab-ci.yml` `test` stage calls `scripts/test.sh`
- Coverage report uploaded as artifact for GitLab MR coverage display

## Risks / Trade-offs

- **Big diff** → Mitigated by 5-phase split; each phase is a separate commit, reviewable independently
- **Test churn** → Tests import `store`/`runner` types directly; will need import updates. Mechanical, not behavioral. TDD reduces churn: new tests are written against the target architecture, not the old one.
- **Service layer may seem over-engineered for MVP** → Trade-off accepted: the god-file cost (1731 lines, triplicated logic, data race) exceeds the abstraction cost
- **`model.Project` JSON tags** may change API response shape → Mitigated by matching existing `PipelineSnapshot` field names in tags; verified by e2e tests
- **TDD slows initial implementation** → Trade-off accepted: the safety net prevents regressions during a large refactor; tests also serve as living documentation of the new architecture
- **Coverage thresholds may rise over time** → Thresholds are floors, not ceilings; they can be raised in a separate change but never lowered without explicit decision
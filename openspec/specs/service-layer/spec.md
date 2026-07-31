# service-layer Specification

## Purpose
TBD - created by archiving change hexagonal-refactor. Update Purpose after archive.
## Requirements
### Requirement: Service layer holds business logic
The `internal/service` package SHALL contain all business logic (run orchestration, webhook processing, project CRUD validation). It SHALL NOT import `net/http`.

#### Scenario: Service has no HTTP dependency
- **WHEN** checking `internal/service` imports
- **THEN** `net/http` is not imported

### Requirement: buildVarStore helper eliminates duplication
A single `buildVarStore(proj, cfg) *variables.Store` helper SHALL be used by all run-related handlers.

#### Scenario: Var-store constructed once
- **WHEN** `handleRunProject`, `handleRunTask`, or `handleApproveManualTask` needs a variable store
- **THEN** they call `service.buildVarStore` instead of inlining ~40 lines of construction

### Requirement: ProjectService handles CRUD validation
`ProjectService` SHALL validate project config (name required, repo required) before delegating to `ProjectStore`.

#### Scenario: Missing name rejected
- **WHEN** creating a project without a name
- **THEN** `ProjectService` returns an error before the store is called

### Requirement: RunService orchestrates async execution
`RunService` SHALL create the run record, launch the background goroutine, and call `FinishRun` on completion. The goroutine SHALL have panic recovery.

#### Scenario: Run completes and is finalized
- **WHEN** `RunService.Start(name, changed)` is called
- **THEN** a run record is created, the runner executes tasks, and `FinishRun` is called with the final status

#### Scenario: Panic in goroutine is recovered
- **WHEN** the runner panics during execution
- **THEN** the panic is recovered and the run is marked failed

### Requirement: Service unit tests use mock stores
The `internal/service` package tests SHALL use hand-rolled mock implementations of `domain.ProjectStore`, `domain.RunStore`, and `domain.PipelineRunner`. Tests SHALL NOT connect to PostgreSQL or touch the filesystem.

#### Scenario: ProjectService tested with mock ProjectStore
- **WHEN** testing `ProjectService.Create`
- **THEN** a `MockProjectStore` records the `Upsert` call and returns nil, without any file or DB access

#### Scenario: RunService tested with mock RunStore and PipelineRunner
- **WHEN** testing `RunService.Start`
- **THEN** a `MockRunStore` records `CreateRun` + `FinishRun` calls and a `MockPipelineRunner` returns predetermined task results, without executing real scripts

### Requirement: Service package coverage ≥90%
The `internal/service` package SHALL maintain ≥90% statement coverage as measured by `go test -coverprofile`.

#### Scenario: Coverage gate enforced
- **WHEN** running `go test ./internal/service/ -coverprofile=coverage.out`
- **THEN** `go tool cover -func=coverage.out` reports ≥90% for the total package


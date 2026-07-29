## ADDED Requirements

### Requirement: Domain package has zero external dependencies
The `internal/domain` package SHALL NOT import any other `internal/*` package. It SHALL contain only interfaces and shared types.

#### Scenario: Domain is a leaf
- **WHEN** running `go list -deps gitlab.com/lunaris-bot/metaci/internal/domain`
- **THEN** no `internal/web`, `internal/store`, `internal/runner`, or `internal/service` appears

### Requirement: ProjectStore port covers project CRUD
The `domain.ProjectStore` interface SHALL expose `Load`, `Save`, `Get`, `FindByGitLabID`, `Upsert`, `Delete`, `List`.

#### Scenario: FileStore implements ProjectStore
- **WHEN** a `FileStore` is used as `domain.ProjectStore`
- **THEN** all project operations succeed without type assertions

### Requirement: RunStore port covers run lifecycle
The `domain.RunStore` interface SHALL expose `CreateRun`, `FinishRun`, `AppendTaskResult`, `FlushRunOutput`, `CancelRun`, `ApplyTaskResult`, `GetRun`, `CountRuns`, `ListRuns`, `ListAllRuns`, `CountRunsByStatus`.

#### Scenario: PGStore implements RunStore
- **WHEN** a `PGStore` is used as `domain.RunStore`
- **THEN** all run operations succeed without type assertions

### Requirement: PipelineRunner port covers execution
The `domain.PipelineRunner` interface SHALL expose `Run(ctx, project, changedSet, opts) ([]TaskResult, error)`.

#### Scenario: Local executor implements PipelineRunner
- **WHEN** the local runner mode is selected
- **THEN** `LocalExecutor` satisfies `domain.PipelineRunner`

### Requirement: Single TaskResult type
The `domain.TaskResult` struct SHALL be the only `TaskResult` type. `runner.TaskResult` and `store.TaskResult` SHALL be removed.

#### Scenario: No field-by-field conversion
- **WHEN** a handler receives task results from the runner
- **THEN** it passes them directly to the store without field-by-field copying

### Requirement: PipelineSnapshot eliminated
`store.PipelineSnapshot` and related snapshot types SHALL be removed. `model.Project` SHALL be persisted directly via JSON tags.

#### Scenario: Run stores model.Project
- **WHEN** creating a run
- **THEN** `pipeline_def` JSONB column contains `model.Project` serialized with JSON tags

### Requirement: Compile-time port assertions
Each adapter SHALL satisfy its port via a compile-time assertion: `var _ domain.ProjectStore = (*PGProjectStore)(nil)` and equivalent for every adapter.

#### Scenario: Interface drift caught at build time
- **WHEN** a port interface gains a new method and an adapter is not updated
- **THEN** `go build` fails on the adapter package

### Requirement: Port mocks for testing
The `internal/domain` package SHALL include mock implementations of `ProjectStore`, `RunStore`, `PipelineRunner`, and `WebhookLogSink` for use in `service` tests. Mocks SHALL be hand-rolled structs with no external dependencies.

#### Scenario: MockProjectStore available
- **WHEN** a service test needs a `domain.ProjectStore`
- **THEN** `domain.MockProjectStore` is available with configurable return values and call recording

### Requirement: Domain package coverage ≥90%
The `internal/domain` package SHALL maintain ≥90% statement coverage.

#### Scenario: Coverage gate enforced
- **WHEN** running `go test ./internal/domain/ -coverprofile=coverage.out`
- **THEN** `go tool cover -func=coverage.out` reports ≥90% for the total package
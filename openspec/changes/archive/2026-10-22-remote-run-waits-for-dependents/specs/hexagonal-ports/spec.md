## MODIFIED Requirements

### Requirement: RunStore port covers run lifecycle
The `domain.RunStore` interface SHALL expose `CreateRun`, `FinishRun`, `FinishRunIfDone`, `AppendTaskResult`, `FlushRunOutput`, `CancelRun`, `ApplyTaskResult`, `GetRun`, `CountRuns`, `ListRuns`, `ListAllRuns`, `CountRunsByStatus`. `FinishRunIfDone` SHALL finish a run only when its recorded results leave nothing pending or running, and SHALL leave a run that is already terminal alone.

#### Scenario: PGStore implements RunStore
- **WHEN** a `PGStore` is used as `domain.RunStore`
- **THEN** all run operations succeed without type assertions

#### Scenario: A run with a task in flight is not finished
- **WHEN** `FinishRunIfDone` is called for a run whose results still hold a pending or running task
- **THEN** the run keeps its `running` status

## MODIFIED Requirements

### Requirement: RunService orchestrates async execution
`RunService` SHALL create the run record, launch the background goroutine, and finish the run on completion: an in-process run calls `FinishRun`, a remote run calls `FinishRunIfDone`, which finishes it only when no task is pending or running. The goroutine SHALL have panic recovery.

#### Scenario: Run completes and is finalized
- **WHEN** `RunService.Start(name, changed)` is called
- **THEN** a run record is created, the runner executes tasks, and `FinishRun` is called with the final status

#### Scenario: Panic in goroutine is recovered
- **WHEN** the runner panics during execution
- **THEN** the panic is recovered and the run is marked failed

#### Scenario: A remote run is finished only when its dependents are decided
- **WHEN** a runner reports a task of a remote run completed
- **THEN** the service resumes the run and calls `FinishRunIfDone`
- **AND** the run is finished only when no task is pending or running

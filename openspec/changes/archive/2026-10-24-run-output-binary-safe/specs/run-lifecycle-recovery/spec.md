## ADDED Requirements

### Requirement: A failed write of a run's outcome is logged

When Kestrix cannot store a run's final state, the output it flushes after a
task, or a task's result, it SHALL log a warning that names the run and the
error, with secret values redacted. It SHALL NOT discard the failure silently.

#### Scenario: The final record cannot be written
- **WHEN** storing the outcome of a finished in-process run fails
- **THEN** Kestrix SHALL log a warning with the run id and the error

#### Scenario: A secret does not reach the log
- **WHEN** the error of such a write contains a resolved secret value
- **THEN** the logged warning SHALL NOT contain it

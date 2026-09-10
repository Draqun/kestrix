## ADDED Requirements

### Requirement: Project deletion guards against an active run

Deleting a project whose run is currently executing SHALL be refused with
`409 Conflict` naming the run(s) still active, rather than deleting the
project and letting the run's underlying process continue against rows that
no longer exist. "Currently executing" means a run with a live execution
goroutine; a run waiting for manual approval has none and SHALL NOT block
the delete. A project with no active run SHALL delete exactly as before,
cascading its runs and their task results.

#### Scenario: Delete refused while a run is active

- **WHEN** a client sends `DELETE /api/projects/{name}` and a run for that project is currently executing
- **THEN** the server SHALL respond `409 Conflict` naming the active run's ID
- **AND** the project, its runs, and their task results SHALL remain unchanged

#### Scenario: Delete succeeds once no run is active

- **WHEN** a client sends `DELETE /api/projects/{name}` and no run for that project is currently executing
- **THEN** the server SHALL delete the project
- **AND** its runs and their task results SHALL cascade-delete as before

#### Scenario: Cancel then delete

- **WHEN** an active run is cancelled and its process has terminated
- **AND** a client then sends `DELETE /api/projects/{name}`
- **THEN** the server SHALL delete the project, since no run is active any longer

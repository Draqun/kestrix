## ADDED Requirements

### Requirement: Manual tasks in a remote run

In a remote-mode run started without `include_manual`, Kestrix SHALL record
each triggered manual task as `manual` when the run starts, before it creates
the run's first job, and SHALL NOT dispatch it until it is approved. While a
task is `manual`, the run SHALL stay `running` and the tasks that depend on it
SHALL wait. If a dependency of a `manual` task fails, is canceled or is
skipped, Kestrix SHALL record that task `skipped` with the error
`dependency failed`. In a run started with `include_manual`, a manual task
SHALL be an ordinary task. Kestrix SHALL record the sources a remote run
started from, so that approving one of its tasks can re-open them.

#### Scenario: A manual task waits for approval
- **WHEN** `deploy` is manual and depends on `build`, and a run starts without `include_manual`
- **THEN** `deploy` SHALL be `manual` from the moment the run starts
- **AND** after the runner reports `build` passed, the run SHALL still be `running` and no job SHALL exist for `deploy`

#### Scenario: Approval dispatches the waiting task
- **WHEN** an operator approves `deploy` in that run
- **THEN** a job for `deploy` SHALL be available to a runner
- **AND** the run SHALL stay `running`

#### Scenario: A failed dependency skips the gate
- **WHEN** the runner reports `build` failed in that run
- **THEN** `deploy` SHALL be recorded `skipped` with the error `dependency failed`
- **AND** the run SHALL become `failed`

#### Scenario: include_manual runs the manual task
- **WHEN** a run starts with `include_manual` and the runner reports `build` passed
- **THEN** a job for `deploy` SHALL be available without approval

#### Scenario: A run of manual tasks only waits
- **WHEN** every triggered task is manual and the run starts without `include_manual`
- **THEN** each of them SHALL be `manual`
- **AND** the run SHALL be `running`

#### Scenario: Approval can open the run's sources
- **WHEN** a remote run starts from a recorded primary source
- **THEN** the run SHALL record that source

## MODIFIED Requirements

### Requirement: Concurrent run limit

Kestrix SHALL execute at most `max_concurrent_runs` runs at a time (default `2`; a non-positive or non-integer configured value SHALL fail startup naming the setting). A run accepted while every slot is busy SHALL be recorded with status `queued` and SHALL start automatically, in acceptance order, when a slot frees. Whole-pipeline runs, single-task re-runs, and approved manual tasks each occupy one slot while executing. Acceptance SHALL be reported before a slot is acquired: every route that starts a run SHALL respond with the run's id and its accepted status (`running` or `queued`) without waiting for the run to start or finish, and the run's outcome SHALL be read from the run record afterwards.

#### Scenario: Burst above the limit
- **WHEN** the limit is `2` and three runs are triggered at once
- **THEN** two report `running`, one reports `queued`, and the queued run starts when either of the first two finishes

#### Scenario: Queued run is visible
- **WHEN** a run is `queued`
- **THEN** it appears in `GET /api/runs`, `GET /api/projects/{name}/runs`, the dashboard counters, and the UI with a distinct `queued` state

#### Scenario: Invalid limit
- **WHEN** the configured limit is `0` or `abc`
- **THEN** startup fails with an error naming `runner.max_concurrent_runs`

#### Scenario: Single-task re-run is accepted immediately
- **WHEN** a single-task re-run is requested while every slot is busy
- **THEN** the response SHALL carry the new run's id and the status `queued`
- **AND** the response SHALL NOT wait for the task to execute or carry its output

## MODIFIED Requirements

### Requirement: Concurrent run limit

Kestrix SHALL execute at most `max_concurrent_runs` runs at a time (default `2`; a non-positive or non-integer configured value SHALL fail startup naming the setting). A run accepted while every slot is busy SHALL be recorded with status `queued` and SHALL start automatically, in acceptance order, when a slot frees. Whole-pipeline runs, single-task re-runs, and approved manual tasks each occupy one slot while executing. A `remote`-mode run's tasks SHALL NOT occupy an in-process concurrency slot: their pacing is bounded by registered-runner availability, not `max_concurrent_runs`.

#### Scenario: Burst above the limit
- **WHEN** the limit is `2` and three runs are triggered at once
- **THEN** two report `running`, one reports `queued`, and the queued run starts when either of the first two finishes

#### Scenario: Queued run is visible
- **WHEN** a run is `queued`
- **THEN** it appears in `GET /api/runs`, `GET /api/projects/{name}/runs`, the dashboard counters, and the UI with a distinct `queued` state

#### Scenario: Invalid limit
- **WHEN** the configured limit is `0` or `abc`
- **THEN** startup fails with an error naming `runner.max_concurrent_runs`

#### Scenario: Remote-mode tasks bypass the concurrency slot
- **WHEN** `max_concurrent_runs` in-process slots are all busy and a `remote`-mode run starts
- **THEN** its tasks SHALL still dispatch as jobs immediately
- **AND** SHALL NOT be recorded `queued` waiting for an in-process slot

## MODIFIED Requirements

### Requirement: Exposed metric set

When metrics are enabled, the endpoint SHALL expose at least: the total number
of runs that reached each terminal status (`passed`, `failed`, `canceled`), the
current number of `running` and `queued` runs, a histogram of completed run
durations in seconds, the total number of webhook deliveries by outcome
(accepted, rejected, skipped), the current number of pending remote jobs that no registered runner can take (`kestrix_remote_jobs_unmatched`), and a build-information gauge whose value is `1`
and whose labels carry the same version string reported by `GET /api/version`.
Every metric SHALL carry a `HELP` and a `TYPE` line. Counters SHALL be
monotonic for the life of the process and SHALL restart at zero after a restart.

#### Scenario: Run outcome counted once
- **WHEN** a run reaches a terminal status
- **THEN** the counter for that status SHALL increase by exactly one
- **AND** the run's duration SHALL be observed once in the duration histogram

#### Scenario: Queue depth reflects the scheduler
- **WHEN** runs are queued because every concurrency slot is busy
- **THEN** the queued gauge SHALL equal the number of runs in `queued` state
- **AND** it SHALL return to zero once the queue drains

#### Scenario: Label cardinality is bounded
- **WHEN** metrics carry a project label
- **THEN** the number of distinct project label values SHALL be bounded by a documented cap
- **AND** projects beyond the cap SHALL be aggregated under a single overflow label value rather than dropped

#### Scenario: Unmatched remote jobs are counted
- **WHEN** two pending remote jobs require a tag no registered runner has
- **THEN** `kestrix_remote_jobs_unmatched` SHALL be `2`
- **AND** it SHALL return to `0` once a runner with that tag is registered

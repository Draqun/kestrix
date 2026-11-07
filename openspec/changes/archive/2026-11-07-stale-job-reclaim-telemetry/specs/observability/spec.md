# Spec Delta: observability

## ADDED Requirements

### Requirement: Stale runner reclaim run telemetry
The system SHALL record run completion metrics (`kestrix_runs_total` counter with `status="failed"` and duration histogram) whenever a run is finalized due to all remaining jobs being failed by silent-runner reclaim.

#### Scenario: Silent runner timeout fails run and records metrics
- **GIVEN** an active remote run whose remaining tasks exceed max reclaims
- **WHEN** `ReclaimStaleRemoteJobs` fails the jobs and ends the run
- **THEN** `kestrix_runs_total{status="failed"}` is incremented by 1
- **AND** the run duration is recorded in `kestrix_run_duration_seconds`
- **AND** the terminal run state is broadcast on `EventHub`

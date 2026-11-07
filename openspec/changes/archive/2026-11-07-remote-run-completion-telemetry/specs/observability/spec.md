# Spec Delta: observability

## ADDED Requirements

### Requirement: Remote run terminal completion telemetry
The system SHALL record run completion metrics (`kestrix_runs_total` counter and `kestrix_run_duration_seconds` histogram) whenever a run executing via remote runners reaches a terminal state (`passed`, `failed`, or `canceled`).

#### Scenario: Remote run completion increments metric
- **WHEN** a remote run reaches a terminal state (`passed` or `failed`) upon completion of its final remote job
- **THEN** `kestrix_runs_total{status="<terminal-status>"}` is incremented by exactly one
- **AND** the elapsed run duration is observed in `kestrix_run_duration_seconds`

#### Scenario: Canceled remote run records telemetry
- **WHEN** an active or queued remote run is canceled
- **THEN** `kestrix_runs_total{status="canceled"}` is incremented by exactly one

# Spec: Dashboard Metrics

## Purpose
Provide aggregate system metrics and recent activity data for the dashboard page.

## Requirements

### Requirement: Dashboard API endpoint
The system SHALL expose a `GET /api/dashboard` endpoint returning aggregate system metrics and recent activity.

#### Scenario: Dashboard returns all metrics
- **WHEN** a client calls `GET /api/dashboard`
- **THEN** the response SHALL include:
  - `projects`: total number of registered projects
  - `runs.total`: total number of runs
  - `runs.passed`: runs with status "passed"
  - `runs.failed`: runs with status "failed"
  - `runs.running`: currently running runs
  - `runs.canceled`: canceled runs
  - `runs.pending`: pending runs
  - `recent_runs`: array of the 10 most recent runs with `id`, `project`, `status`, `started_at`, `finished_at`
  - `server.status`: health status string
  - `server.service`: service name

### Requirement: Dashboard degrades gracefully without database
When using FileStore (no PostgreSQL), the dashboard SHALL return available metrics without runs data.

#### Scenario: No runs data with FileStore
- **WHEN** the server uses FileStore
- **THEN** `GET /api/dashboard` SHALL return project count and server status
- **AND** `runs.*` fields SHALL all be 0
- **AND** `recent_runs` SHALL be an empty array

### Requirement: Dashboard renders useful information
The Dashboard page SHALL display real metrics from the API rather than placeholder values.

#### Scenario: Dashboard shows real data
- **WHEN** the Dashboard page loads
- **THEN** it SHALL call `GET /api/dashboard`
- **AND** display project count, run status counts, and recent runs
- **AND** the polling interval SHALL be 30 seconds

#### Scenario: Dashboard without database shows limited data
- **WHEN** the server uses FileStore
- **THEN** the Dashboard SHALL display project count and server status
- **AND** SHALL show a notice that runs data requires PostgreSQL

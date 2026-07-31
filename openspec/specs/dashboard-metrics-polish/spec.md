# dashboard-metrics-polish Specification

## Purpose
TBD - created by archiving change dashboard-metrics-polish. Update Purpose after archive.
## Requirements
### Requirement: Show canceled and skipped counts in stat cards
The dashboard SHALL display stat cards for both canceled and skipped run counts.

#### Scenario: Canceled count visible
- **WHEN** runs with status "canceled" exist
- **THEN** the Canceled stat card shows the correct count

#### Scenario: Skipped count visible
- **WHEN** runs with status "skipped" exist
- **THEN** the Skipped stat card shows the correct count

### Requirement: Show finished timestamp in recent runs table
The recent runs table SHALL display a Finished column showing the `finished_at` timestamp.

#### Scenario: Finished column present
- **WHEN** the recent runs table is rendered
- **THEN** a Finished column is present after the Started column

#### Scenario: Finished value shown
- **WHEN** a run has `finished_at` set
- **THEN** the cell shows the formatted date/time

#### Scenario: No finished value
- **WHEN** a run has no `finished_at`
- **THEN** the cell shows '—'


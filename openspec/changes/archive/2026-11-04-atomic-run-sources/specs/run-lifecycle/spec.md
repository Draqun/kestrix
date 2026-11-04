# Spec Delta: run-lifecycle

## ADDED Requirements

### Requirement: Atomic persistence of initial sources on run creation

When creating a new pipeline run record, Kestrix SHALL persist any provided initial sources atomically in the initial database insertion. The run record SHALL NOT be exposed in a state with empty sources when initial sources were provided at trigger time.

#### Scenario: Run created with initial sources has sources immediately available
- **WHEN** a run is created with initial sources containing a primary repository branch
- **THEN** the persisted run record SHALL contain the initial sources immediately upon creation
- **AND** queries against `runs.sources` (such as `FindActiveRunsByRef`) SHALL match the run without requiring a secondary update

#### Scenario: Run created without initial sources defaults to empty sources
- **WHEN** a run is created without initial sources (nil or empty slice)
- **THEN** the persisted run record SHALL have `sources` set to an empty JSON array `[]`

# Spec: pipeline-run-filters

## ADDED Requirements

### Requirement: Filter runs by status

The pipeline run list SHALL support filtering by run status. The user SHALL be able to select one or more statuses from the set of known run statuses. Only runs whose status matches at least one selected status SHALL be displayed.

#### Scenario: Single status filter

- **WHEN** the user selects status `failed` in the status filter
- **THEN** the list displays only runs with status `failed`
- **AND** runs with other statuses are hidden

#### Scenario: Multiple status filters

- **WHEN** the user selects statuses `passed` and `running`
- **THEN** the list displays only runs with status `passed` or `running`

#### Scenario: No status filter

- **WHEN** no status filter is selected
- **THEN** the list displays runs of all statuses

### Requirement: Filter runs by changed repository

The pipeline run list SHALL support filtering by changed repository. The filter dropdown SHALL be populated with repository names present in the `changed` field of the loaded runs. Selecting a repository SHALL display only runs whose `changed` array includes that repository.

#### Scenario: Filter by a changed repository

- **WHEN** the user selects repository `auth_service` from the change filter
- **THEN** the list displays only runs whose `changed` array contains `auth_service`

#### Scenario: Clear change filter

- **WHEN** the user selects the empty option in the change filter
- **THEN** the list displays runs for all repositories

### Requirement: Reset pagination when filters change

When the user changes any filter value, the current page SHALL reset to the first page so that filtered results are shown from the beginning.

#### Scenario: Filter while on page two

- **GIVEN** the user is on page 2 of the run list
- **WHEN** the user changes the status filter
- **THEN** the list resets to page 1

### Requirement: Display friendly empty-change label

Each run list item SHALL display the text `no changes` when the run's `changed` array is empty. The previous text `Changed: none` SHALL no longer be used.

#### Scenario: Run with no changed repositories

- **GIVEN** a run with an empty `changed` array
- **WHEN** the run is rendered in the list
- **THEN** the secondary text shows `no changes`

#### Scenario: Run with changed repositories

- **GIVEN** a run with `changed` array `["auth_service", "frontend"]`
- **WHEN** the run is rendered in the list
- **THEN** the secondary text shows `Changed: auth_service, frontend`

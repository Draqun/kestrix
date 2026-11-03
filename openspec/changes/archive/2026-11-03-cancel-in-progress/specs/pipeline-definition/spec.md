# Spec Delta: pipeline-definition

## ADDED Requirements

### Requirement: Cancel in progress project attribute

A `project` block in HCL MAY declare an optional `cancel_in_progress` boolean attribute. When omitted, `cancel_in_progress` SHALL default to `false`. When set to `true`, the parsed project aggregate SHALL record `CancelInProgress` as `true`. A non-boolean value for `cancel_in_progress` SHALL fail HCL parsing with an actionable error.

#### Scenario: Declaring cancel_in_progress
- **WHEN** a pipeline file declares `cancel_in_progress = true` in its `project` block
- **THEN** the parsed project SHALL have `CancelInProgress` set to `true`

#### Scenario: Default cancel_in_progress
- **WHEN** a pipeline file omits `cancel_in_progress` in its `project` block
- **THEN** the parsed project SHALL have `CancelInProgress` set to `false`

#### Scenario: Invalid cancel_in_progress type rejected
- **WHEN** a pipeline file sets `cancel_in_progress = "true"` or a non-boolean value
- **THEN** parsing SHALL fail with a type validation error

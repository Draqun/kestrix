# Spec Delta: pipeline-definition

## ADDED Requirements

### Requirement: Task execution timeout declaration
A `ci_task` block MAY declare an optional `timeout` attribute containing a duration string (e.g., `15m`, `30s`, `1h`). The parser SHALL validate that the duration format is valid according to standard time duration syntax. If invalid, parsing SHALL fail with a descriptive error.

#### Scenario: Declare valid task timeout
- **WHEN** a user writes `ci_task "build" { timeout = "10m" script = "make build" }`
- **THEN** the system SHALL parse the task definition with a timeout duration of 10 minutes

#### Scenario: Reject invalid task timeout format
- **WHEN** a user writes `ci_task "build" { timeout = "invalid-time" }`
- **THEN** the parser SHALL reject the definition with a syntax error naming the invalid timeout attribute

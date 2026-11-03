# Spec Delta: remote-runner-execution

## ADDED Requirements

### Requirement: Remote trace append deduplication and event coordination

The system SHALL coordinate trace streaming from remote runners through the service layer, acknowledging idempotent trace retries without re-broadcasting duplicate chunks to real-time subscribers.

#### Scenario: Idempotent trace append retry is not re-broadcast
- **GIVEN** a remote runner has sent a trace chunk at offset `N`
- **WHEN** the runner retries sending the same chunk at offset `N`
- **THEN** the store SHALL acknowledge the retry successfully
- **AND** the event hub SHALL NOT broadcast the duplicate chunk to real-time subscribers

#### Scenario: Claimed remote task emits running transition
- **WHEN** a registered runner claims a pending remote job
- **THEN** the system SHALL broadcast a task event with status `running` for that task to real-time subscribers

## ADDED Requirements

### Requirement: Runner unit coverage must exclude integration-only paths
The runner package SHALL be improved using unit tests only. Coverage accounting SHALL NOT depend on Docker-backed, bash-backed, or gitlab-runner-backed integration execution.

#### Scenario: Coverage target ignores integration tests
- **WHEN** a test requires Docker or another external binary to run
- **THEN** that test SHALL NOT be relied on to justify the unit coverage target
- **AND** the coverage target SHALL be satisfied by unit-testable code paths only

#### Scenario: External execution remains outside the unit metric
- **WHEN** the runner executes Docker, bash, or gitlab-runner commands in production code
- **THEN** those paths MAY remain covered only by integration-style tests
- **AND** they SHALL NOT be treated as the unit-coverage objective

### Requirement: Pure runner helpers SHALL be unit-testable
Pure helper logic in `internal/runner` SHALL be covered by unit tests without external services.

#### Scenario: Order and dependency logic are tested directly
- **WHEN** the runner orders tasks or validates dependencies
- **THEN** unit tests SHALL verify the expected order and error behavior
- **AND** the tests SHALL run without Docker

#### Scenario: Repository path resolution is tested directly
- **WHEN** the runner resolves task repository directories
- **THEN** unit tests SHALL verify success and failure branches for valid paths, escapes, and missing paths
- **AND** the tests SHALL run without Docker

### Requirement: Runner orchestration SHALL be testable with fakes
Task-loop orchestration in the runner SHALL be testable with fake executors or equivalent test seams.

#### Scenario: Manual tasks are skipped by a fake executor test
- **WHEN** a task is marked manual and the run excludes manual tasks
- **THEN** a unit test using a fake executor SHALL verify the task is skipped
- **AND** the test SHALL not require Docker or bash

#### Scenario: Dependency failures are propagated by a fake executor test
- **WHEN** one task fails and another depends on it
- **THEN** a unit test using a fake executor SHALL verify the dependent task is blocked or skipped as designed
- **AND** the test SHALL not require Docker or bash

### Requirement: Mode dispatch SHALL be unit-tested
`PipelineExecutor` mode selection SHALL be covered by unit tests.

#### Scenario: Local mode dispatches to local execution path
- **WHEN** the executor mode is local or empty
- **THEN** unit tests SHALL verify the local dispatch branch is selected

#### Scenario: Unsupported mode returns an error
- **WHEN** the executor mode is unknown
- **THEN** unit tests SHALL verify an error is returned

#### Scenario: GitLab runner mode validates its generator dependency
- **WHEN** the executor mode is `gitlab-runner` and no YAML generator is configured
- **THEN** unit tests SHALL verify the appropriate error is returned

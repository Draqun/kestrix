## MODIFIED Requirements

### Requirement: Runner SHALL have complete unit-test statement coverage
The project SHALL enforce 100.0% statement coverage for `internal/runner` using unit tests alone. Every production statement in the package, including orchestration, adapters, dispatch, callbacks, and error handling, SHALL be executable through deterministic unit tests.

#### Scenario: Unit-only quality gate passes
- **WHEN** the runner unit-coverage target executes without integration or e2e tests
- **THEN** the reported statement coverage SHALL equal 100.0%
- **AND** the quality gate SHALL fail for any lower result

#### Scenario: External adapter behavior is unit-tested
- **WHEN** runner code constructs or dispatches Docker or bash execution
- **THEN** unit tests SHALL exercise that production logic through fakes or equivalent test seams
- **AND** no external binary or service SHALL be required to reach 100.0% statement coverage

### Requirement: Coverage accounting SHALL exclude integration and end-to-end tests
The unit coverage profile SHALL contain execution data only from unit tests. Integration and end-to-end suites SHALL NOT contribute to the unit coverage percentage, regardless of whether they execute the same production statements.

#### Scenario: Integration-capable environment does not affect the metric
- **WHEN** Docker, bash, databases, or external services are available on the test host
- **THEN** the unit coverage target SHALL skip or exclude tests that execute those dependencies
- **AND** the result SHALL remain deterministic across developer and CI environments

#### Scenario: Integration and e2e results remain separate
- **WHEN** integration or end-to-end suites execute
- **THEN** their pass or fail results SHALL be reported independently from unit coverage
- **AND** their execution SHALL NOT modify the unit-only coverage profile

### Requirement: Pure runner behavior SHALL be directly unit-testable
Runner ordering, stage assignment, repository path resolution, manual handling, dependency propagation, callbacks, and mode selection SHALL be testable without external services.

#### Scenario: Ordering and repository logic are tested directly
- **WHEN** runner helpers process task dependencies or repository paths
- **THEN** unit tests SHALL verify success, cycle, missing dependency, escape, missing path, and invalid path behavior

#### Scenario: Orchestration is tested with fakes
- **WHEN** a task passes, fails, is manual, or is blocked by a failed dependency
- **THEN** unit tests SHALL verify results, ordering, skips, and callbacks with a fake executor
- **AND** the tests SHALL not invoke Docker or bash

#### Scenario: All execution modes are dispatched deterministically
- **WHEN** `PipelineExecutor` receives local, empty, Docker, or unsupported mode
- **THEN** unit tests SHALL verify the selected path and all nil or error branches without invoking an external executor

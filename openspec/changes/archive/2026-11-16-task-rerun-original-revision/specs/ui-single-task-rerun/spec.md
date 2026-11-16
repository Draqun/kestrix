# Spec Delta: ui-single-task-rerun

## ADDED Requirements

### Requirement: Re-run executes at original run revision
When an operator triggers a single task re-run from a finished run's detail view, the system SHALL check out and execute the task against the original run's recorded source revision (commit SHA) rather than default-branch HEAD.

#### Scenario: Re-run preserves historical commit SHA
- **WHEN** an operator triggers a re-run on a task from run #40 whose recorded source commit is `abc1234`
- **AND** the target repository branch has newer commits ahead of `abc1234`
- **THEN** the new single-task run checks out `abc1234`
- **AND** executes the task script using the pipeline definition and sources from that exact commit

## ADDED Requirements

### Requirement: Unmatched pending jobs are reported

A pending remote job is unmatched when no non-revoked registered runner has
every tag the job requires. That includes a job with no required tags when no
runner is registered at all. Whether a runner is online SHALL NOT affect this.
For every task whose latest result is pending on an unmatched job, every
response returning the run SHALL include a `waiting_reason`:
- `no registered runner has tags: <tags>` for a job with required tags,
  naming them in sorted order;
- `no runner is registered` when no runner is registered.

The reason SHALL be evaluated when the response is built, so it SHALL
disappear as soon as a matching runner is registered, without any further
action. The run detail view SHALL show the reason as a warning next to the
pending task. Kestrix SHALL log a warning naming the run, the task and the
required tags once for each unmatched job. Kestrix SHALL NOT fail or cancel an
unmatched job on its own.

#### Scenario: No runner has the required tag
- **WHEN** a remote-mode task with `runner_tags = ["gpu"]` is pending and the only registered runner has no tags
- **THEN** the run's response SHALL include `waiting_reason` `no registered runner has tags: gpu` for that task
- **AND** the run detail view SHALL show it as a warning

#### Scenario: No runner registered
- **WHEN** an untagged remote-mode task is pending and no runner is registered
- **THEN** its `waiting_reason` SHALL be `no runner is registered`

#### Scenario: Registering a matching runner clears the reason
- **WHEN** a runner tagged `gpu` is registered while that task is pending
- **THEN** the next response returning the run SHALL NOT include a `waiting_reason` for it

#### Scenario: An offline matching runner is not unmatched
- **WHEN** the only runner with the required tags is registered but offline
- **THEN** the task SHALL NOT carry a `waiting_reason`

#### Scenario: Revoked runners do not count
- **WHEN** the only runner with the required tags has been revoked
- **THEN** the task SHALL carry a `waiting_reason`

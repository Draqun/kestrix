# Spec Delta: run-scheduling

## ADDED Requirements

### Requirement: Automatically cancel superseded runs

When a run starts for a project with `cancel_in_progress = true` and an identified branch or ref, Kestrix SHALL automatically cancel every prior non-terminal run for that same project and branch. The cancellation SHALL record actor `system:cancel-in-progress`. Active in-process tasks SHALL be terminated within the cancellation grace period, unclaimed remote jobs SHALL be marked unclaimable, and queued runs SHALL transition to `canceled` without executing tasks.

#### Scenario: Running run on the same branch is canceled
- **WHEN** a run is `running` on branch `feature-x` for a project with `cancel_in_progress = true` and a new run starts on branch `feature-x`
- **THEN** the earlier run SHALL transition to `canceled` with `canceled_by` set to `system:cancel-in-progress`
- **AND** the new run SHALL continue execution

#### Scenario: Queued run on the same branch is canceled without executing
- **WHEN** a run is `queued` on branch `feature-x` for a project with `cancel_in_progress = true` and a new run starts on branch `feature-x`
- **THEN** the queued run SHALL transition to `canceled` with `canceled_by` set to `system:cancel-in-progress`
- **AND** the slot it waited for SHALL NOT be occupied by that canceled run

#### Scenario: Different branches are not canceled
- **WHEN** a run is `running` on branch `main` and a new run starts on branch `feature-y` for the same project
- **THEN** the run on `main` SHALL NOT be canceled and SHALL continue execution

#### Scenario: Cancel in progress disabled preserves concurrent runs
- **WHEN** a project has `cancel_in_progress = false` or omitted and multiple runs start for the same branch
- **THEN** earlier runs SHALL continue running or remain queued in FIFO order without automatic cancellation

#### Scenario: Remote-mode run marks unclaimed jobs unclaimable
- **WHEN** a `remote`-mode run on branch `feature-z` is superseded by a newer run on branch `feature-z`
- **THEN** its unclaimed pending jobs SHALL be marked unclaimable and the run SHALL transition to `canceled`

#### Scenario: Manual whole-pipeline run cancels prior runs on the same primary branch
- **WHEN** a run is `running` on branch `feature-x` for a project with `cancel_in_progress = true` and a user manually starts a whole-pipeline run on branch `feature-x`
- **THEN** the earlier run SHALL transition to `canceled` with `canceled_by` set to `system:cancel-in-progress`
- **AND** the new manual run SHALL continue execution

#### Scenario: Single-task reruns or triggers without primary ref do not cancel active runs
- **WHEN** a single-task rerun or trigger without primary source ref starts for a project with `cancel_in_progress = true`
- **THEN** existing active runs SHALL NOT be canceled and SHALL continue execution

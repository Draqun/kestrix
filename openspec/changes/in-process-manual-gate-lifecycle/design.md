# Design: in-process-manual-gate-lifecycle

## Context

In `internal/runner/runner.go`, `taskLoop` evaluates task dependencies to determine readiness. While it checks for failed dependencies to trigger skipping, it currently does not treat an upstream task in `manual` status as a blocker for downstream execution. Consequently, tasks dependent on a waiting gate execute prematurely. Furthermore, when an upstream failure occurs, manual gates are not marked `skipped`, causing `runStatus` to see a non-terminal task and keep the run `running` forever.

## Goals / Non-Goals

**Goals:**
- Block execution of any task whose dependencies include a task in `manual` status in `taskLoop`.
- Automatically mark manual gates as `skipped` when an upstream dependency fails, ensuring the run terminates as `failed`.

**Non-Goals:**
- Changing remote runner dispatch (which already uses job state machines correctly).

## Decisions

### D1: Extend dependency readiness check in `taskLoop`
- **Choice:** In `runner.go`, evaluate task dependencies against both completion status and manual gate states. A task is ready only when all dependencies are in `passed` status. If any dependency is `manual`, the dependent task remains unlaunched.
- **Alternatives considered:** Handling this at the DAG level before `taskLoop`. Rejected because task states transition dynamically as approvals occur.

### D2: Propagate skip to manual gates on dependency failure
- **Choice:** When a task fails, immediately inspect all transitive downstream tasks. Any downstream task declared as manual transitions to `skipped` with `dependency <task> failed`.
- **Alternatives considered:** Letting the manual gate remain `manual`. Rejected because a user cannot meaningfully approve a deployment whose build failed.

## Risks / Trade-offs

- **[Risk]** Existing unit tests asserting instant execution of downstream tasks may break.  
  → *Mitigation:* Update test fixtures to simulate manual task approval before asserting downstream execution.

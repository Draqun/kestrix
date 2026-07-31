## Context

In `handleRunProject` (`internal/web/server.go`), after all tasks complete, the code iterates task results. If a task has `Status == "manual"` and no task failed, it overrides the run status from `"passed"` to `"manual"`. This is incorrect: a pipeline should only reflect whether all _executed_ jobs succeeded.

## Goals / Non-Goals

**Goals:**
- Run status remains `"passed"` when all triggered tasks pass, regardless of manual tasks
- Manual job status is preserved at the task level (for FE approval UI)

**Non-Goals:**
- Changing how manual tasks work at the job level
- Changing the cancel/skip logic

## Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Remove `hasManual` check | Delete lines 917-932 block that overrides status | Simple, minimal change. "manual" is a TaskResult status, not a Run status. |

## Risks / Trade-offs

- Existing runs with `status="manual"` remain in the DB. No migration needed — they'll appear correctly in dashboard stats as `passed` once re-saved on next run.

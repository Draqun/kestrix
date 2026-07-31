## Why

A pipeline where all jobs pass but some are manual (untriggered) is currently marked as `"manual"` at the run level. This is misleading — manual is a job attribute, not a pipeline state. Such pipelines should be marked as `"passed"`.

## What Changes

- Remove the override that sets run status to `"manual"` when all tasks pass but one is manual
- Pipelines with only manual jobs left will show as `"passed"`
- `"manual"` remains a valid status at the task/job level

## Capabilities

### New Capabilities

- `fix-pipeline-manual-status`: Pipeline-level status no longer reflects manual job state

### Modified Capabilities

*(none)*

## Impact

- `internal/web/server.go` — remove `hasManual` check (lines 930-932)
- Dashboard stat card for "manual" should be removed (no longer a valid run status)

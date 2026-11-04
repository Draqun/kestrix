# Proposal: atomic run creation with initial sources

## Why

Currently, `CreateRun` inserts a run record into PostgreSQL with an empty JSON array for `sources: '[]'`. Sources are persisted later through `UpdateRunSources`:
1. In `RunService.Start` immediately after `createRun`, but conditionally gated on `proj.CancelInProgress && len(opts.InitialSources) > 0`.
2. In `RunService.OnSourcesAcquired` during local/Docker background execution.
3. In `dispatchRemoteJobs` during remote pipeline dispatch.

This split creates two issues:
1. **Timing window in cancel-in-progress:** Between `createRun` and `UpdateRunSources`, a newly created run exists in the store with empty sources. A concurrent push arriving during this window cannot match the run via `FindActiveRunsByRef`, breaking the "only the latest run survives" invariant.
2. **Scattered source persistence:** Source persistence is duplicated across three execution paths with inconsistent feature gating. If `cancel_in_progress` is disabled, initial sources supplied at trigger time are not persisted until source acquisition finishes in the background.

## What Changes

- Extend `domain.RunStore.CreateRun` (and `PGStore.CreateRun`) to accept optional initial sources atomically in the initial `INSERT INTO runs`.
- Eliminate the separate `UpdateRunSources` call in `RunService.Start`.
- Ensure newly inserted runs immediately have their primary branch/ref indexed and visible to `FindActiveRunsByRef` from the exact moment of insertion.
- Unify source persistence across all run types regardless of whether `cancel_in_progress` is enabled.

## Capabilities

### Modified Capabilities

- `run-lifecycle`: Atomically persist initial sources upon run record insertion.
- `run-scheduling`: Eliminate the non-atomic lookup window for cancel-in-progress discovery.

## Non-goals

- Changing the schema or structure of `domain.AcquiredSource`.
- Changing how background source acquisition updates commit SHAs when cloning repositories.

## Impact

- `internal/domain`: `RunStore.CreateRun` signature or creation options accept initial sources.
- `internal/store`: `PGStore.CreateRun` serializes initial sources directly into the `INSERT` statement.
- `internal/service`: `RunService.Start` passes `opts.InitialSources` to `createRun`; removes redundant gated `UpdateRunSources`.
- `internal/mocks`: Mock implementations updated to record initial sources during `CreateRun`.

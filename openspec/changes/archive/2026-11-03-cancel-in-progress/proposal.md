# Proposal: cancel-in-progress for superseded pipeline runs

## Why

When multiple pushes or merges land on the same branch in rapid succession (e.g., merging three pull requests or pushing consecutive commits to a feature branch), Kestrix currently executes every commit sequentially in FIFO order. Obsolete intermediate commits consume runner execution slots and machine resources even though their results are immediately superseded by the latest commit.

## What Changes

- Projects can declare `cancel_in_progress = true` in their HCL `project` block (default: `false`).
- When a new run starts for a project with `cancel_in_progress = true` on an identified branch or ref:
  - Any prior run for the same project and branch/ref currently in `running` or `queued` status is automatically canceled.
  - Canceled runs record `canceled_by: "system:cancel-in-progress"`.
  - In `local` and `docker` modes, executing tasks are terminated via the existing `RunService.Cancel` mechanism (SIGTERM, then SIGKILL after grace).
  - In `remote` mode, unclaimed jobs are marked unclaimable and the run is finalized as `canceled`.
  - Queued runs transition directly to `canceled` without executing tasks or holding concurrency slots.
- Manual runs and webhook-triggered runs both honor cancel-in-progress when associated with a branch or ref. Single-task re-runs do not auto-cancel whole-pipeline runs.

## Capabilities

### Modified Capabilities

- `pipeline-definition`: Declare optional `cancel_in_progress` boolean attribute on HCL `project` block.
- `run-scheduling`: Automatically cancel active and queued runs superseded by newer runs on the same project and ref.

## Non-goals

- Dynamic concurrency group expressions or arbitrary locking keys (such as `${{ github.workflow }}-${{ github.ref }}`).
- Canceling runs across different branches or different projects.
- Canceling runs when `cancel_in_progress` is false or omitted.
- Automatic cancellation of single-task re-runs.

## Impact

- `internal/domain`: `Project` aggregate gains `CancelInProgress bool`.
- `internal/parser`: HCL parser accepts optional `cancel_in_progress` attribute on `project` blocks.
- `internal/store`: `RunStore` gains lookup for active and queued run IDs by project name and primary ref.
- `internal/service`: `RunService.Start` checks `CancelInProgress`, cancels superseded runs via `Cancel`, and records `canceled_by`.

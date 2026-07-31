## Why

The stat cards show Passed, Failed, Running, Pending but not Canceled. The recent runs table has no column for finish time. Users have no visibility into when runs completed.

## What Changes

- Add a **Canceled** stat card (in the existing grid, alongside Pending/Skipped)
- Add a **Skipped** stat card
- Add a **Finished** column to the recent runs table showing `finished_at`

## Capabilities

### New Capabilities

- `dashboard-metrics-polish`: Canceled/skipped run counts shown in stats; finished timestamp in recent runs table

### Modified Capabilities

*(none)*

## Impact

- `ui/src/pages/Dashboard.jsx` — add stat cards for canceled and skipped, add Finished column

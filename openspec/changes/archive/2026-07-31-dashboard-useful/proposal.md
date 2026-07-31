## Why

Dashboard currently shows placeholder stats (webhook count masquerading as pipeline count, repos always 0) and no meaningful information. A user landing on the dashboard should immediately see the system's health, recent activity, and key metrics.

## What Changes

- Add `GET /api/dashboard` endpoint returning aggregate stats: project count, run counts by status, recent runs, and system status
- Replace Dashboard.jsx stat cards with real data from the new endpoint
- Add a recent runs table showing the last N runs with status, project, and timestamp
- Add a run status breakdown (e.g. pie/bar chart or stat group — passed/failed/running)
- Keep quick-start and architecture cards but move them below the data section

## Capabilities

### New Capabilities
- `dashboard-metrics`: Aggregate endpoint and UI components for dashboard statistics (project count, run status breakdown, recent runs)

### Modified Capabilities
- *(none — first iteration, no existing dashboard spec)*

## Impact

- `internal/web/server.go` — new `handleDashboard` handler
- `internal/store/pgstore.go` — new aggregate query methods (count runs by status, recent runs across projects)
- `ui/src/pages/Dashboard.jsx` — full rewrite of the dashboard page
- `ui/src/api/client.js` — new `getDashboard()` client method

## Non-goals

- No authentication/authorization changes
- No real-time updates (polling is fine for now)
- No historical charts or time-series data

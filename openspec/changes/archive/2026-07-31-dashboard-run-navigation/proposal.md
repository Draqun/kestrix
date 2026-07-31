## Why

The recent runs table on the dashboard shows run data but rows are not clickable. Users must navigate to `/pipelines` and find the run there to see details — a friction point that undermines the dashboard's value as a starting point for daily work.

## What Changes

- Make dashboard recent runs table rows clickable
- Clicking a row navigates to `/pipelines/runs/:projectName/:runId`
- Use the same pattern as the rest of the app (`useNavigate` callback, not `<Link>`)

## Capabilities

### New Capabilities

- `dashboard-run-navigation`: Clicking a recent run row in the dashboard navigates to the run detail page

### Modified Capabilities

*(none)*

## Impact

- `ui/src/pages/Dashboard.jsx` — add `useNavigate`, wire `onClick` on table rows

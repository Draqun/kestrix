## Why

During a recent refactor, a regression was introduced where the frontend fails to display pipeline graphs on the "new run" page (`/pipelines/new-run/:projectName`). Users see an empty or broken graph view instead of the DAG visualization that shows task dependencies and execution order. This blocks the core workflow of reviewing and triggering pipeline runs.

## What Changes

- Fix the pipeline graph rendering on the new-run page
- Ensure DAG data is correctly fetched and passed to the React Flow component
- Verify the API endpoint returns the correct pipeline definition structure
- Restore DAG node/edge visualization (colors, statuses, layout)

## Capabilities

### New Capabilities
- None (this is a bug fix, not a new feature)

### Modified Capabilities
- `pipeline-visualization`: The DAG graph rendering on the new-run page must correctly display nodes, edges, and status colors based on the pipeline definition

## Impact

- **Frontend**: `ui/src/pages/NewRunPage.jsx` (or similar), `ui/src/components/DAGViewer.jsx`, `ui/src/lib/dagLayout.js`
- **API**: `internal/web/server.go` - `/api/projects/{name}/pipeline` and `/api/projects/{name}/definition` endpoints
- **Types**: Shared type definitions for pipeline DAG data structure
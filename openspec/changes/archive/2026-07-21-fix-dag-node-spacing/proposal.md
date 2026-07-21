## Why

DAG node content (label, trigger expression, run status) is cramped together with inconsistent spacing — the status text appears "stuck" to the element above it, especially in the waterfall run view.

## What Changes

- Convert the DAG node's `<Box>` container to a flex column with a consistent `gap`
- Ensure all conditional child Typography elements have `display="block"` so they occupy their own line
- Remove ad-hoc `mt` overrides on individual children in favor of the container gap

## Capabilities

### New Capabilities

None — purely an implementation-level styling fix.

### Modified Capabilities

None — no spec-level behavior changes.

## Impact

- `ui/src/components/DAGViewer.jsx` — single file, styling-only changes

## Why

The pipeline DAG graph on the run detail page (`/pipelines/runs/:projectName/:runId`) no longer renders after the `ui-modernization` refactoring. Users see "No pipeline definition" instead of the interactive DAG visualization, breaking a core feature of the run inspection workflow.

## What Changes

- Fix the run detail DAG rendering so the pipeline graph displays correctly
- If the `DAGViewer` component has a structural issue (e.g., stale state sync between parent `useNodesState` and child `useNodesState`, layout sizing), resolve it without re-duplicating the component
- Ensure the fix does not break the DAG on `PipelineEditor.jsx` (the other consumer of DAGViewer)
- Catch any similar regression patterns introduced by the `ui-modernization` refactoring (e.g., `...props` spread on Box, `useMemo` side-effect pattern)

## Capabilities

### New Capabilities
- none — this is a fix, not a new capability

### Modified Capabilities
- none — the change is purely internal; no spec-level requirement changes

## Impact

- `ui/src/components/DAGViewer.jsx` — may need structural changes
- `ui/src/pages/Pipelines.jsx` — may need data-flow or conditional-rendering fixes
- `ui/src/pages/PipelineEditor.jsx` — verify unchanged behavior

## Non-goals

- No UI redesign or feature additions
- No backend changes (the API already returns `pipeline_def` correctly)

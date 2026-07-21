## Context

The `ui-modernization` refactoring extracted the shared `DAGViewer` component from inline `<ReactFlow>` usage in both `Pipelines.jsx` (run detail page) and `PipelineEditor.jsx`. After the change, the pipeline DAG graph on the run detail page (`/pipelines/runs/:projectName/:runId`) shows "No pipeline definition" instead of the interactive graph.

The API response for a run includes `pipeline_def` (JSONB snapshot), and the backend correctly stores and serves it. The `layoutFromProject()` function and `dagLayout.js` module produce valid React Flow node/edge arrays. The `DAGViewer` component wraps `<ReactFlow>` with a custom `CustomNode` renderer, background, controls, and minimap.

## Goals / Non-Goals

**Goals:**
- Restore the DAG graph display on the run detail page
- Keep the shared `DAGViewer` component (no re-duplication)
- Keep `PipelineEditor.jsx` DAG unchanged

**Non-Goals:**
- No UI redesign or feature additions
- No backend changes

## Decisions

### 1. Root-cause investigation strategy

The most likely causes (ordered by probability):

| # | Hypothesis | Evidence |
|---|------------|----------|
| 1 | `pipeline_def` is missing or `undefined` for run 32 | Check if runs created before the JSONB migration have null `pipeline_def`; also verify the field exists in the JSON response |
| 2 | `useEffect` for DAG doesn't fire because its dependency `detail?.pipeline_def` is the same reference across re-renders | `detail` is set by `setDetail(d)` where `d` is a freshly-parsed JSON object; each fetch creates a new object, so the reference should change |
| 3 | `DAGViewer`'s `useMemo` side-effect pattern causes a render issue in React 19 Strict Mode | `useMemo` should not call `setNodes`/`setEdges`; this violates Rules of Hooks and may cause the sync to behave unexpectedly |
| 4 | `layoutFromProject` returns empty nodes because `repos`/`tasks` field casing doesn't match | The code handles both camelCase and PascalCase keys as a fallback; this should not be an issue |
| 5 | The `CustomNode` component renders but inside a zero-height container | Verify the flex layout chain from `RunDetail` → card → `DAGViewer` → ReactFlow |

Investigation approach: add a `console.log` at the relevant data flow points, then inspect the browser console on the live page.

### 2. Fix approach

Once the root cause is identified, apply a minimal fix:

| Cause | Fix |
|-------|-----|
| `pipeline_def` missing from API response | Fix the Go handler or poll the correct field name |
| `useEffect` doesn't fire | Adjust dependency array or add `detail` itself as a dep |
| `useMemo` side-effect breaks strict mode | Replace `useMemo` with a proper `useEffect` for syncing node/edge state |
| `layoutFromProject` returns empty | Fix field mapping or provide fallback |
| Zero-height container | Fix CSS flex/grow chain |

### 3. Verification

- Navigate to `/pipelines/runs/metaci/32` and confirm the DAG renders
- Check "Back to runs" → re-select the run to confirm it survives navigation cycles
- Verify `PipelineEditor.jsx` still renders its DAG correctly

## Risks / Trade-offs

- [Tight coupling] RunDetail still owns its own `useNodesState` (needless) but DAGViewer also owns one → potential stale state confusion. Mitigation: after fix, either let RunDetail manage all state (pass callbacks down) or let DAGViewer own it entirely (remove RunDetail's hooks).
- [Cache invalidation] If the run is re-fetched (polling), dagNodes re-computes, and DAGViewer may flicker. Mitigation: React Flow handles `fitView` transitions gracefully.

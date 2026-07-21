## 1. Diagnose

- [x] 1.1 Open `/pipelines/runs/metaci/32` in browser devtools and check whether `detail.pipeline_def` is present in the API response (Network tab)
- [x] 1.2 Add `console.log('dagNodes', dagNodes.length, dagNodes)` at the end of the `useEffect` that builds DAG state (line ~144 in Pipelines.jsx), then inspect the browser console on page load
- [x] 1.3 Check if the `useMemo` side-effect pattern in `DAGViewer.jsx` (line 62-65) causes a render loop in React 19 Strict Mode — add a `console.count('sync')` and observe in console
- [x] 1.4 Verify the `layoutFromProject` function is called with valid data and returns non-empty arrays by logging its output

## 2. Fix

- [x] 2.1 Apply the minimal fix based on the identified root cause from step 1 (likely: fix `DAGViewer` sync pattern, adjust `useEffect` deps, or correct the container layout chain)
- [x] 2.2 Run `npm run build` to verify the frontend compiles without errors

## 3. Verify

- [x] 3.1 Navigate to `/pipelines/runs/metaci/32` — confirm the DAG renders and is interactive (pan, zoom, node click)
- [x] 3.2 Navigate to `/pipelines` run list → select a different run — confirm DAG renders for that run too
- [x] 3.3 Check `PipelineEditor.jsx` DAG rendering is unaffected
- [x] 3.4 Remove any temporary `console.log` statements added during diagnosis

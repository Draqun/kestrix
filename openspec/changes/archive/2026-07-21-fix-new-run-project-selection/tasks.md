## 1. Fix NewRun project list

- [x] 1.1 Update `NewRun` component in `Pipelines.jsx` to extract `data.items` from the paginated `listProjects()` response

## 2. Fix Pipeline Editor autocomplete

- [x] 2.1 Update `PipelineEditor.jsx` autocomplete handler to extract `data.items` from the paginated `listProjects()` response

## 3. Fix Pipeline Editor runs list

- [x] 3.1 Update `PipelineEditor.jsx` runs load handler to extract `data.items` from the paginated `listRuns()` response

## 4. Verification

- [x] 4.1 Build UI (`npm run build`) and confirm no build errors
- [x] 4.2 Run UI tests (`node --test`) and confirm all pass
- [x] 4.3 Run Go tests (`go test ./... -short`) and confirm all pass

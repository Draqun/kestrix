## Why

The recent pagination change wrapped API responses in `{items: [...], pagination: {...}}`. Several UI components still expect a flat array and fail to display data, breaking project selection on the New Run page, the Pipeline Editor autocomplete, and the Pipeline Editor run history.

## What Changes

- Fix `NewRun` component in `Pipelines.jsx` to extract `data.items` from the paginated project list response
- Fix `PipelineEditor.jsx` autocomplete to extract `data.items` from the paginated project list response
- Fix `PipelineEditor.jsx` run list to extract `data.items` from the paginated runs response
- Ensure all other callers of `listProjects()` and `listRuns()` handle the new response format correctly

## Capabilities

### New Capabilities
- *(none)*

### Modified Capabilities
- *(none)*

## Impact

- Fixes three UI locations where paginated API responses broke data display
- No API or backend changes required

## Non-goals

- No changes to the API response format
- No changes to the pagination component or its behavior

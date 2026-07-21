## Why

The job output panel on the run detail page (`/pipelines/runs/:projectName/:runId`) has no vertical scrollbar. Long logs overflow the viewport or the card boundary, making them impossible to read without scrolling the entire page.

## What Changes

- Add a scrollable constraint to the active job output and overall run output `<Box component="pre">` elements
- The output area fills the available vertical space inside the card, with `overflow: auto` scrolling

## Capabilities

### New Capabilities
- none — bugfix, no new capability

### Modified Capabilities
- none — no spec-level requirement changes

## Impact

- `ui/src/pages/Pipelines.jsx` — fix the log output containers in RunDetail

## Non-goals

- No changes to log formatting, copy button, or polling behaviour
- No backend changes

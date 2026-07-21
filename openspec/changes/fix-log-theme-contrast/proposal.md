## Why

All log output `<Box component="pre">` elements hardcode `bgcolor: 'grey.900'`, which works on dark mode but renders an unreadable dark-on-dark background on light mode.

## What Changes

- Replace hardcoded `grey.900` with a theme-aware background that adapts to both light and dark palette

## Capabilities

### New Capabilities
- none — bugfix, no new capability

### Modified Capabilities
- none — no spec-level requirement changes

## Impact

- `ui/src/pages/Pipelines.jsx` — 2 log containers (active job output, overall run output)
- `ui/src/pages/PipelineEditor.jsx` — 2 log containers (run result output, selected run output)

## Non-goals

- No changes to log formatting, copy button, or layout
- No backend changes

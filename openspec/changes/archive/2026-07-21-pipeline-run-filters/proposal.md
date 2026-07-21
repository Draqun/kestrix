## Why

The Pipelines view lists every pipeline run in a single scrollable table. As the project grows it becomes hard to locate a specific run. Users need basic filters (by run status and by changed repository) to narrow the list quickly. The current list also shows an awkward label "Changed: none", which should be friendlier.

## What Changes

- Add a filter bar to the `/pipelines` run list with:
  - **Status filter** – multi-select of run statuses (`passed`, `failed`, `running`, `canceled`, `pending`, `manual`, `skipped`).
  - **Change filter** – dropdown of repositories that triggered a run, populated from the `changed` field of loaded runs.
- Apply filters client-side on the already loaded run list.
- Update the run list item label from `Changed: none` to `No changes` when the `changed` array is empty.

## Capabilities

### New Capabilities

- `pipeline-run-filters`: Basic client-side filtering for the pipeline run list by status and changed repository.

### Modified Capabilities

- *(none)*

## Impact

- UI component `ui/src/pages/Pipelines.jsx` (the `RunList` component).
- Minor string change in the run list item rendering.
- No backend API changes required; filters operate on data already returned by `GET /api/runs`.

## Non-goals

- Advanced search, full-text search, or filtering by date range.
- Server-side filtering or new API query parameters.
- Persistent filter preferences (e.g., saved in localStorage or backend).
- Bulk actions on filtered results.

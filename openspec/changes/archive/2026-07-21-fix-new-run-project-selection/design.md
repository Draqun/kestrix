## Context

The paginated project and runs endpoints now return `{items: [...], pagination: {...}}` instead of a flat array. Three UI call sites still use the old flat-array assumption and break:

1. `Pipelines.jsx` `NewRun` — project selection list
2. `PipelineEditor.jsx` — project autocomplete
3. `PipelineEditor.jsx` — runs list in pipeline context

## Goals / Non-Goals

**Goals:**
- Restore project selection on the New Run page
- Restore project autocomplete in Pipeline Editor
- Restore runs display in Pipeline Editor

**Non-Goals:**
- No API or backend changes
- No changes to the Pagination component
- No changes to `Projects.jsx` (already handles new format correctly)

## Decisions

- **Minimal change**: Each fix extracts `.items` from the API response object. This matches the pattern already used in `Projects.jsx`.
- **Single location for API normalization**: Not creating a helper wrapper — keeping changes scoped to each call site to minimise diff size.

## Risks / Trade-offs

- No risks — the code change is trivial (accessing `resp.items` instead of `resp`), all projects list and runs list endpoints consistently return the `{items, pagination}` shape.

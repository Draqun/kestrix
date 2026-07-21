## Why

The DAG engine correctly resolves manual tasks (`manual = true`), but the project resolve API (`POST /api/projects/{name}/resolve`) does not expose the `manual` flag in its response. The spec requires that manual tasks appear in the resolved set annotated as requiring human approval. Adding the flag lets the UI display manual tasks correctly and closes the gap between spec and implementation.

## What Changes

- Add a `manual` boolean field to each triggered task object returned by `POST /api/projects/{name}/resolve`.
- Update the existing `dag-engine` spec scenario for manual tasks to cover the API response.
- Update or add tests verifying the `manual` field is present.

## Capabilities

### New Capabilities

- *(none)*

### Modified Capabilities

- `dag-engine`: clarify that the resolve API response includes a `manual` flag for manual tasks.

## Impact

- `internal/web/server.go` (`handleResolveProject`).
- `internal/web/e2e_test.go` and related tests.
- No breaking changes; only an additive field in the response.

## Non-goals

- Changing the global `POST /api/resolve` response shape (it returns task names as strings; altering it would be breaking).
- Adding new DAG engine features.
- UI changes beyond making the new field available.

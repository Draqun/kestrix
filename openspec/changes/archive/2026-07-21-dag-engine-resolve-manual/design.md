## Context

The DAG engine marks tasks with `manual = true` as triggered when their trigger expression is satisfied. `internal/web/server.go::handleResolveProject` returns a list of triggered task objects containing `name` and `reason`, but omits the `manual` flag. This field is required so that callers can distinguish manual tasks from automatic ones.

## Goals / Non-Goals

**Goals:**
- Expose `manual` from `n.Task.Manual` in the resolved task object returned by the project resolve API.
- Keep the change backward-compatible (adds a new field, does not remove or rename existing fields).
- Update the `dag-engine` spec delta to reflect the API contract.
- Verify with tests.

**Non-Goals:**
- Changing the global `/api/resolve` endpoint response.
- Modifying DAG engine resolution logic.
- UI changes.

## Decisions

1. **Only update the project resolve endpoint**
   - The global `/api/resolve` endpoint returns a flat array of task names (`[]string`). Adding `manual` there would require changing the response type to objects, which is a breaking change.
   - The UI and the main run flow use `resolveProject` (`/api/projects/{name}/resolve`), which already returns objects. Adding a field there is safe.

2. **Read `manual` from `n.Task.Manual`**
   - The `model.Task` struct already has a `Manual bool` field, so no engine changes are needed.

## Risks / Trade-offs

- **Clients that ignore unknown fields** – unaffected.
- **Clients that strictly validate response schemas** – may need to allow the new field. This is acceptable for an additive API change.

## Migration Plan

1. Update `handleResolveProject` to include `manual` in each triggered task object.
2. Update `internal/web/e2e_test.go` to assert the field.
3. Run `make test` and `npm run build`.
4. Archive the change.

## Open Questions

- Should the global `/api/resolve` endpoint also expose manual tasks? If yes, it should be done as a separate, versioned change due to the breaking response shape.

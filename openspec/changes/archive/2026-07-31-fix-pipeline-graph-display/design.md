## Context

During a recent refactor, the frontend fails to display pipeline graphs on the "new run" page (`/pipelines/new-run/:projectName`). The DAG viewer component receives empty data because of a field name mismatch between the API response and the frontend layout function.

**Current State:**
- Backend API (`/api/projects/{name}/definition`) returns `model.Project` with JSON tags in lowercase: `repos`, `tasks`, `name`, `path`, `kind`, etc.
- Frontend `dagLayout.js` expects capitalized field names: `Repos`, `Tasks`, `Name`, `Path`, `Kind`
- The optional chaining `project.Repos?.forEach()` silently fails when `Repos` is undefined, resulting in empty nodes/edges
- The DAGViewer component renders but shows nothing because no nodes/edges are created

**Stakeholders:** Frontend developers, backend developers, users creating pipeline runs

## Goals / Non-Goals

**Goals:**
- Fix the pipeline graph display on the new-run page
- Ensure DAG nodes (repos, tasks) and edges (dependencies, triggers, execution) render correctly
- Restore status colors and manual task indicators

**Non-Goals:**
- Change the backend API structure (keep existing JSON tags)
- Modify the DAG layout algorithm
- Add new features to the graph visualization

## Decisions

### Decision 1: Frontend field name adaptation (preferred)

**Choice:** Transform API response in the frontend to match expected capitalized field names.

**Rationale:** 
- Backward compatible - no backend changes needed
- Single point of fix in the API client layer
- Maintains existing Go struct JSON tags (which follow Go conventions)

**Alternative considered:** Change Go struct JSON tags to capitalized (e.g., `json:"Repos"`). Rejected because it deviates from Go conventions and could break other consumers.

**Implementation:** Add a mapping function in `ui/src/api/client.js` that converts the API response:
```javascript
function normalizeProject(proj) {
  if (!proj) return proj;
  return {
    ...proj,
    Repos: proj.repos,
    Tasks: proj.tasks,
    Name: proj.name,
    DefaultImage: proj.default_image,
    Variables: proj.variables,
  };
}
```

Apply this in `getProjectDefinition()` and `getProjectPipeline()` (if it returns similar structure).

### Decision 2: Add defensive checks in dagLayout

**Choice:** Add console warnings when expected fields are missing to catch future regressions early.

**Rationale:** Silent failures are hard to debug. A simple warning will alert developers immediately.

**Implementation:** In `layoutFromProject`, log a warning if `project.Repos` and `project.Tasks` are both undefined.

## Risks / Trade-offs

| Risk | Mitigation |
|------|------------|
| Frontend transform misses a field | Add unit test for `normalizeProject` covering all fields |
| Other API endpoints return similar structures | Audit other endpoints (`/api/projects`, `/api/projects/{name}`) and apply same normalization |
| Layout function changes in future | Keep normalization in API client layer, not in layout function |

## Migration Plan

1. **Create `normalizeProject` helper** in `ui/src/api/client.js`
2. **Apply normalization** in `getProjectDefinition()` response
3. **Add defensive warning** in `dagLayout.js` for missing fields
4. **Test manually** with a sample pipeline to verify graph renders
5. **Add regression test** (optional): unit test for normalization

## Open Questions

- Are there other API endpoints returning `model.Project` that need the same fix? (e.g., `/api/projects/{name}`, `/api/projects` list)
- Should we also normalize nested fields like `Execute.Script` vs `execute.script`? (Currently the frontend accesses `task.Execute?.Script` but the API returns `execute` - this may be another mismatch)
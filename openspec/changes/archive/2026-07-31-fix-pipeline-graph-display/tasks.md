## 1. Frontend API Response Normalization

- [x] 1.1 Add `normalizeProject` helper function in `ui/src/api/client.js`
  - Transform lowercase API fields to capitalized names expected by `dagLayout.js`
  - Map: `repos`→`Repos`, `tasks`→`Tasks`, `name`→`Name`, `path`→`Path`, `kind`→`Kind`, `default_image`→`DefaultImage`, `variables`→`Variables`
  - Also handle nested fields: `execute.script`→`Execute.Script`, `execute.repo`→`Execute.Repo`, `execute.image`→`Execute.Image`

- [x] 1.2 Apply normalization in `getProjectDefinition()`
  - Call `normalizeProject()` on the response before returning
  - Ensure the returned object has capitalized fields

- [x] 1.3 Apply normalization in `getProjectPipeline()` if it returns similar structure
  - Check if this endpoint is used by NewRun page
  - Also applied to `getProject()` and `listProjects()` which return similar structures

- [x] 1.4 Add unit test for `normalizeProject` in `ui/src/api/client.test.js`
  - Test all field mappings
  - Test null/undefined handling
  - Test nested object transformation

## 2. Defensive Checks in Layout Function

- [x] 2.1 Add warning log in `layoutFromProject` when expected fields missing
  - Log warning if `project.Repos` and `project.Tasks` are both undefined
  - Include the actual keys found in the project object for debugging

- [x] 2.2 Verify `dagLayout.js` handles empty arrays gracefully
  - Ensure `project.Repos?.forEach` and `project.Tasks?.forEach` don't throw
  - Already uses optional chaining, no changes needed

## 3. Verification & Testing

- [x] 3.1 Start dev environment (`make dev-all`)
- [x] 3.2 Navigate to `http://localhost:3000/pipelines/new-run/kestrix` (or demo project)
- [x] 3.3 Verify pipeline graph renders with:
  - Repository nodes (📦 icon, path displayed)
  - Task nodes (⚙️ icon, trigger expression, manual badge)
  - Trigger edges (orange, animated, repo→task)
  - Execution edges (blue, animated, task→repo)
  - Dependency edges (purple, static, task→task)

- [x] 3.4 Test "Resolve" button highlights triggered tasks with colored borders
- [x] 3.5 Test "Run" button initiates pipeline execution
- [x] 3.6 Run frontend lint/build: `cd ui && npm run build` ✓
- [x] 3.7 Run Go tests: `make test` ✓ (445 tests passed)

## 4. Regression Prevention

- [x] 4.1 Check other API endpoints returning `model.Project`:
  - `/api/projects/{name}` (getProject) - now normalizes
  - `/api/projects` (listProjects) - now normalizes items array
  - Apply same normalization if used by graph components

- [x] 4.2 Document the field naming convention in `AGENTS.md` or code comments
  - API returns lowercase (Go convention)
  - Frontend layout expects capitalized (JS convention)
  - Normalization happens in API client layer

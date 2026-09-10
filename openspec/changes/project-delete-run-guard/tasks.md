## 1. Expose active runs from RunService

- [ ] 1.1 Add `func (s *RunService) ActiveRunIDs() []int` returning a mutex-guarded snapshot of `s.active`'s keys; verify a unit test starts two runs with a blocking fake runner, asserts both IDs are present, then completes one and asserts only the other remains.

## 2. Guard project deletion

- [ ] 2.1 In `handleDeleteProject` (`internal/web/handler_project.go`), before calling `projectService.Delete`, call `runService.ActiveRunIDs()`, look up each ID's `ProjectName` via `runStore.GetRun`, and if any equal the target project name, respond `409 Conflict` with a body naming the blocking run ID(s) and skip the delete; verify a handler unit test with a mock `RunService`/`RunStore` covers: no active runs (delete proceeds, `200`), one active run for this project (`409`, delete not called), and an active run for a different project (delete proceeds).
- [ ] 2.2 Verify a test confirms deleting a project after its only active run finishes (or is cancelled) succeeds normally.

## 3. Regression coverage

- [ ] 3.1 Run `go build ./... && go vet ./...` and the existing `internal/web` project-handler test suite; confirm no change to delete behavior for the already-covered no-active-run cases.

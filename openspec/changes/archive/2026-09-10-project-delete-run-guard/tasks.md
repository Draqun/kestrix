## 1. Expose active runs from RunService

- [x] 1.1 Add `func (s *RunService) ActiveRunIDsForProject(name string) []int` returning the sorted, mutex-guarded IDs of the runs in `s.active` that belong to `name`, with both registration sites (`Start` and `trackActive`) recording the project alongside the cancel func; verify a unit test starts two runs with a blocking fake runner, asserts both IDs are present, then completes one and asserts only the other remains, and a second test asserts a `RunTask` run is tracked under its project too.

## 2. Guard project deletion

- [x] 2.1 In `handleDeleteProject` (`internal/web/handler_project.go`), before calling `projectService.Delete`, call `runService.ActiveRunIDsForProject(name)` (project names tracked in-memory in `RunService.active`, no run-row lookup), and if non-empty, respond `409 Conflict` with a body naming the blocking run ID(s) and skip the delete; verify a handler unit test covers: no active runs (delete proceeds, `200`), one active run for this project (`409`, delete not called), and an active run for a different project (delete proceeds).
- [x] 2.2 Verify a test confirms deleting a project after its only active run finishes (or is cancelled) succeeds normally.

## 3. Regression coverage

- [x] 3.1 Run `go build ./... && go vet ./...` and the existing `internal/web` project-handler test suite; confirm no change to delete behavior for the already-covered no-active-run cases.

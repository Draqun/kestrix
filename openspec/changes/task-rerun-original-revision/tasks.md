# Tasks: task-rerun-original-revision

## 1. Backend Service & Handler Refactor

- [ ] 1.1 In `internal/service/service.go`, move run record creation into `StartTask`, making it accept optional pinned sources from a parent run; verify with a unit test.
- [ ] 1.2 In `internal/web/handler_run.go`, parse `from_run_id` parameter, look up the parent run's recorded sources, and forward them to `StartTask`; verify with a handler test.

## 2. Frontend Integration & Verification

- [ ] 2.1 In `ui/src/api/client.js` (`rerunTask`), pass `from_run_id` when invoking the re-run endpoint from the run detail page; verify component test.
- [ ] 2.2 Verify full test suite `make test` and `make ui-test` pass with 100% statement coverage.

## Workflow follow-up

- Archive change via `openspec archive task-rerun-original-revision -y` upon implementation.

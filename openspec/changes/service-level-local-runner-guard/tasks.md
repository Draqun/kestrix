# Tasks: service-level-local-runner-guard

## 1. Test Harness Refactoring

- [ ] 1.1 In `internal/service/service_test.go`, introduce a helper that initializes `RunService` with `Runner.AllowLocal = true`; verify helper compiles and can be invoked.
- [ ] 1.2 Update test fixture call sites across `service_test.go`, `run_test.go`, `sources_test.go`, and related files to use the helper; verify all updated tests compile.

## 2. Service Layer Enforcement

- [ ] 2.1 In `internal/service/service.go`, add `CheckLocalAllowed` check to `Start`, `StartTask`, and `ApproveManualTask`; verify with a unit test that when `AllowLocal = false`, each method returns `ErrLocalExecutionDisabled` without fetching sources.
- [ ] 2.2 Run full test suite `go test ./internal/service/...` to ensure all tests pass with 100% statement coverage.

## Workflow follow-up

- Archive change via `openspec archive service-level-local-runner-guard -y` upon implementation.

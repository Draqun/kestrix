## 1. Store

- [ ] 1.1 Add `FailInterruptedRuns(reason string) (int, error)` to `domain.RunStore` and implement it in `internal/store/pgstore_run.go` in one transaction (`SELECT ... WHERE status='running' FOR UPDATE`, latest-per-task derivation shared with `ApplyTaskResult`, rewrite `running`→`failed`/`pending`→`skipped`, append output line, set `status`, `error`, `finished_at`); verify with integration tests covering: run with a running task, run with only passed+pending, manual-only run untouched, no running runs (zero, no writes), idempotent second call.
- [ ] 1.2 Update `RunStore` fakes in `internal/mocks` and service tests so `make test` compiles; add a no-op implementation to any in-memory store used by e2e tests.

## 2. Startup wiring

- [ ] 2.1 Call `FailInterruptedRuns("interrupted by server restart")` in `cmdServe` after `Migrate` and before `NewServerE`, logging `runs: failed %d interrupted run(s) from a previous instance`; verify a `cmd/kestrix` unit test with a fake store asserts the call and the log line, keeping the package at 100% coverage; verify a store error at this point exits non-zero with a message naming the step.

## 3. Docs

- [ ] 3.1 Add the single-instance constraint and the restart behaviour to `docs/architecture.md`, and a `CHANGELOG.md` entry under Unreleased/Changed; verify `docs/docs_test.go` passes.

## 4. Verification

- [ ] 4.1 `make test-coverage` reports 100% on `internal/store` (integration layer), `internal/service`, `cmd/kestrix`.
- [ ] 4.2 Manual: start `bin/kestrix serve`, trigger a run with a `sleep 60` task, kill the server mid-run, restart it, confirm `GET /api/projects/{name}/runs/{id}` shows `failed` / `interrupted by server restart` and the Pipelines page renders it as failed with the output line.

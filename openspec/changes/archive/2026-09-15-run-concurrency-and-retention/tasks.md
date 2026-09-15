## 1. Configuration

- [x] 1.1 Add `runner.max_concurrent_runs`, `runner.run_history_limit`, `runner.max_output_bytes` keys, env forms, defaults (`2`, `1000`, `10485760`), positive-integer validation, and source-report entries to `internal/config`; verify the reflection completeness test (`TestKeyConstantsMatchMultiFormSettingKeys`) and precedence table tests pass with the three new rows, and invalid values fail with the setting name.
- [x] 1.2 Thread the three values into the runner/service wiring in `cmdServe` (`WithRunnerConfig` snapshot); verify `cmd/kestrix` startup tests cover them and coverage stays 100%.

## 2. Concurrency and `queued`

- [x] 2.1 Add `queued` to the run status vocabulary: `CreateRun` accepts an initial status, `ApplyTaskResult`/`runStatus` treat `queued` as non-terminal, `CountRunsByStatus` reports it; verify store integration tests for create-as-queued and the status derivation.
- [x] 2.2 Add a slot semaphore to `RunService` (capacity from config) with FIFO waiters; `Start`, `RunTask`, `ApproveManualTask` create the run, acquire a slot (creating as `queued` and flipping to `running` on acquire), and release on completion; verify unit tests with a fake store: three starts at limit two yield two running + one queued, the queued one starts after a release, and `trackActive` bookkeeping is unchanged.
- [x] 2.3 Make `Cancel` on a `queued` run remove it from the waiters and call `CancelRun` without executing; verify a unit test asserts no pipeline call and the next waiter is promoted.
- [x] 2.4 Expose `queued` in the UI: status chip style in `Pipelines.jsx` and counter on `Dashboard.jsx` (resolve the design open question here); verify component tests render the new state and `make ui-test` passes.

## 3. Retention and output bound

- [x] 3.1 Add `PruneRuns(limit int)` to the store, called inside `FinishRun`'s transaction with the terminal-only predicate, mirroring `pgstore_webhook.go`; verify integration tests: limit exceeded deletes oldest terminal runs only, non-terminal rows survive, limit not exceeded deletes nothing.
- [x] 3.2 Add a `boundedWriter` after the `redactingWriter` in `prepareRunExecution` that stops storing bytes past the bound and appends a marker line `--- output truncated at N bytes (runner.max_output_bytes) ---` on flush; verify unit tests: under-bound unchanged, over-bound ends with the marker and is at most bound+marker, redaction still applies to a secret straddling the cut.

## 4. Docs and gate

- [x] 4.1 Document the three settings in `docs/usage.md`, `.env.template`, `README.md` configuration table, and the Compose pass-through; add `CHANGELOG.md` entries (Added: queueing, retention, bound; note first-prune-after-upgrade); verify `docs/docs_test.go` passes.
- [x] 4.2 Run `make test-coverage` (100% on gated packages) and `make test-integration`.

## 5. Verification

- [x] 5.1 Manual: set `KESTRIX_RUNNER_MAX_CONCURRENT_RUNS=1`, trigger two runs with `sleep 30` tasks, observe the second as `queued` in the UI and `GET /api/runs?status=queued`, cancel it, confirm `canceled` with no task output; then set `KESTRIX_RUN_HISTORY_LIMIT=3`, finish four runs, confirm three remain.

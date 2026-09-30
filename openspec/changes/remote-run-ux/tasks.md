## 1. Single-task re-runs never resume (fix first: it dispatches unrequested tasks)

- [x] 1.1 `resumeRemoteRun` returns before resolving the DAG when `GetRemoteRunSnapshot` returns no snapshot (design D2). Unit test: a run with no snapshot whose single task completes dispatches nothing and records no other task result. Unit test: an approved manual task in a run with a snapshot still dispatches its dependents.
- [x] 1.2 Integration test through `CompleteRemoteJob` against Postgres: the `lint` (`!repo.docs.changed`) / `build` / `deploy` pipeline from the proposal. After re-running `build` and completing it, `remote_jobs` holds only the `build` job.

## 2. Re-run label

- [x] 2.1 Idempotent migration `runs.rerun_task TEXT NOT NULL DEFAULT ''`; `CreateRun` takes the re-run task; `domain.Run.RerunTask` (`json:"rerun_task"`) read by every run query. Integration test: round-trip, and an old row reads as `""`.
- [x] 2.2 The task-run handler and its secret-error path (`internal/web/handler_run.go`) create the run with `rerun_task` set and an empty `changed`. Handler test asserts both fields. Regenerate Swagger if the run schema is annotated.
- [x] 2.3 UI: `Pipelines.jsx` (run list and run detail) and `PipelineEditor.jsx` show "Re-run: <task>" when `rerun_task` is set, and otherwise keep the `changed` rendering. Component tests for both cases. Run vitest memory-capped, one file at a time. Compare booleans, never DOM nodes, inside `waitFor`.

## 3. Task output from the whole job

- [x] 3.1 Idempotent migration `remote_jobs.output TEXT NOT NULL DEFAULT ''`. `AppendJobTrace` appends the redacted chunk to it with `boundAppendChunk` in the same transaction (design D1). Integration test: two appends accumulate, and a stale-epoch append stores nothing.
- [x] 3.2 `CompleteJob` sets the task result's `output` to the job's accumulated output plus the completion's remainder, bounded by `max_output_bytes`. Integration tests: trace-only output lands on the task; over-bound output ends with the truncation marker; the aggregate `runs.output` is unchanged from today.

## 4. Runner credentials

- [x] 4.1 `requireRunnerAuth`: a rejected bearer token → `401 invalid or revoked runner token`; an `AuthenticateRunner` error → `500`; a missing or non-bearer header keeps its message. Handler tests assert status and body for all three.
- [x] 4.2 `kestrix runner`: `pollNextJob` reports a `401` distinctly; `runPollLoop` returns it and `cmdRunner` exits non-zero, printing the body. `500` and connection errors keep the retry loop. Unit tests with an `httptest` server for each case.

## 5. Docs

- [x] 5.1 `docs/remote-runners.md`: task output on the run detail, re-run behaviour in `remote` mode, the runner exiting on a revoked token. `CHANGELOG.md` `[Unreleased]`: one `Fixed` entry per defect, and `rerun_task` under `Added`.

## 6. Verification

- [x] 6.1 `scripts/test-all.sh` all layers, `make lint`, `make ui-lint` green; 100% unit coverage gate holds.
- [x] 6.2 Migrations both ways: a scratch database started by the `main` binary, then the branch binary, then `main` again. `/api/health` answers 200 each time.
- [x] 6.3 Manual, on the remote-mode stack (Postgres, Kestrix, one runner container), in Chrome:
  - the proposal's three-task pipeline;
  - re-run `build`: only `build` runs, the run reads "Re-run: build", and `build`'s panel shows its output;
  - revoke the runner in Settings: its process exits naming `invalid or revoked runner token`.

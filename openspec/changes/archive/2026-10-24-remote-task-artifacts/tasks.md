## 1. Declaration

- [x] 1.1 Parser: `artifacts` on `ci_task` (`domain.Task.Artifacts`), validated per design D1. Parser tests: valid list, absolute path, `..`, empty entry, 33 entries.
- [x] 1.2 Every mode (D2): after a task exits 0, a missing declared path fails it with `artifact "<path>" not found`. Unit tests for `local` and `docker` executors, and for the runner-side check.

## 2. Storage and upload

- [x] 2.1 Migration `remote_artifacts(run_id REFERENCES runs ON DELETE CASCADE, task_name, archive BYTEA, created_at)`; store methods to put, list-by-tasks and delete-for-terminal-runs. Integration tests, including replacement on retry and cascade on run prune.
- [x] 2.2 `PUT /api/runner-jobs/{id}/artifacts?epoch=N` (design D3):
  - epoch/ownership/terminal fencing like trace;
  - `KESTRIX_RUNNER_ARTIFACT_MAX_BYTES` (default 50 MiB, validated positive) with `413` over the cap;
  - entries outside declared paths rejected.

  Handler tests for each rejection.
- [x] 2.3 Runner: after a passed task, tar and gzip the declared paths, upload, then complete. On `413` or a failed upload, complete as failed naming the reason. Unit tests with an `httptest` server.

## 3. Delivery to dependents

- [x] 3.1 `resumeRemoteRun`/`buildRemoteJobPayload` add the passed ancestors' artifacts in D4 order. The runner unpacks them over the source with the existing safe unpack. Unit tests: chain, diamond (order and overwrite), no ancestors.
- [x] 3.2 The terminal sweep deletes a run's artifacts (D5). Integration test.
- [x] 3.3 Integration test through `CompleteRemoteJob` against Postgres: `build` writes `dist/app.txt` and declares `dist/`; `deploy` depends on it and `cat`s the file. The run passes in `remote` mode with a fake runner, and in `local` mode.

## 4. Docs

- [x] 4.1 `docs/pipeline-definition.md` (`artifacts`, relative paths, missing-artifact failure), `docs/remote-runners.md` (artifact flow, limit, lifetime, "don't declare secret files"). `CHANGELOG.md` `[Unreleased]`: `Fixed` (remote tasks now receive their dependencies' outputs when declared) and `Added` (`artifacts`).

## 5. Verification

- [x] 5.1 `scripts/test-all.sh` all layers, `make lint`; 100% unit coverage gate holds.
- [x] 5.2 Migrations both ways: a scratch database started by the `main` binary, then the branch, then `main` again. `/api/health` answers 200 each time.
- [x] 5.3 Manual, on the remote-mode stack with two runner containers:
  - `build` → `deploy` where `deploy` reads `build`'s artifact passes even when the two jobs land on different runners;
  - an artifact over the limit fails `build` naming it.

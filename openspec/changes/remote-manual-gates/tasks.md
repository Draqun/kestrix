## 1. Reproduce

- [ ] 1.1 Integration tests on Postgres through `RunService.Start` and `CompleteRemoteJob` with the real run and job stores, one per case: (a) `build` → `deploy` (manual): after `build` passes the run is `running`, `deploy` is `manual` and no job exists for it; (b) the same with `include_manual`: a job for `deploy` is claimable after `build`; (c) `build` fails: `deploy` is `skipped` with `dependency failed` and the run is `failed`; (d) every triggered task manual: each is `manual` and the run is `running`; (e) `Start` given `InitialSources`: the run records them. Red on `main`: (a) the run is `passed` and `deploy` has no result, (b) no job, (c) `deploy` has no result, (d) the run stays `running` with no results, (e) no sources recorded.

## 2. Dispatch

- [ ] 2.1 `eligibleForDispatch` takes `includeManual`, returns the gates to record, and re-examines a `manual` result for a failed, canceled or skipped dependency (design D1-D3). Table-driven unit tests: a gate is recorded whatever the state of its dependencies; it is never returned as eligible; with a failed, canceled or skipped dependency it is skipped; with a pending or passed one it stays `manual`; dependents of a gate wait; a gate behind another gate; a gate declared before its dependency; `includeManual` makes it an ordinary task.
- [ ] 2.2 `startRemote` records the gates before it creates the first job, and takes `IncludeManual`; `resumeRemoteRun` passes `run.IncludeManual`, records the skip, and records a gate it finds unrecorded. Unit test with a recording mock that the gate is appended before the first `CreateRemoteJob`. Tests 1.1 (a)-(d) are green.
- [ ] 2.3 `startRemote` stores `opts.InitialSources` with `UpdateRunSources` right after `CreateRun` (D4). Unit tests: a store without `RunSourceStore` is skipped; a failing `UpdateRunSources` returns the error. Test 1.1 (e) is green.

## 3. Approval

- [ ] 3.1 Handler test on Postgres with a fake source session: approving the recorded gate of a remote run re-opens the recorded source, creates a job for the gate, leaves the task `pending` and the run `running`; approving a `skipped` gate answers `400`. The dependents of an approved gate are left to `remote-run-waits-for-dependents`.

## 4. Docs and specs

- [ ] 4.1 `docs/remote-runners.md`: manual tasks and `include_manual` in a remote project - when a gate appears, what approving does, what a failed dependency does. `CHANGELOG.md` `[Unreleased]`, `Fixed`: manual tasks wait for approval in a remote project.
- [ ] 4.2 `openspec validate remote-manual-gates --strict` passes with the `fix-pipeline-manual-status` rename (D5), and `openspec archive` applies it to a copy of the specs without error.

## 5. Verification

- [ ] 5.1 `scripts/test-all.sh` all layers and `make lint`; the 100% unit coverage gate holds. Mutation checks: not recording the gates turns 1.1 (a) and (d) red; ignoring `includeManual` turns (b) red.
- [ ] 5.2 Manual, on the remote-mode stack with a `build` → `deploy` (manual) pipeline:
  - after `build` the run reads `running`, `deploy` reads MANUAL, and the Run button appears;
  - pressing it runs `deploy` on the runner and the run reads `passed`;
  - with `build` failing, `deploy` reads `skipped` and the run `failed`;
  - a run started with the include-manual switch runs `deploy` without approval.

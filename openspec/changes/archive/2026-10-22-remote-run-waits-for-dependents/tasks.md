## 1. Reproduce

- [x] 1.1 Integration test through `CompleteRemoteJob` against Postgres with the real run and job stores: a whole run `build` → `deploy`. After `build` passes, the run is `running` and a runner can claim `deploy`; after `deploy` passes, the run is `passed`. Red on `main`: the run is `passed` and `ClaimNextJob` returns nothing.

## 2. Store

- [x] 2.1 `CompleteJob` has one protocol (design D1): it records the result, the output and the job, and leaves the run's `status`, `error` and `finished_at` alone. The store keeps a second, private path for silent-runner recovery: `ReclaimStaleJobs` fails a stale job and ends its run in the same transaction, when the run's results leave nothing in flight. Integration tests: a completion leaves the run's status alone, a reclaim ends the run only when nothing is left in flight, and a fenced completion is unchanged.
- [x] 2.2 `RunStore.FinishRunIfDone` (D2) in `pgstore_run.go`, with its mock. Integration tests: passed, failed and canceled outcomes; still `running` while a task is pending or running; a finished run is left alone, a canceled one included; history is pruned as `CompleteJob` did.

## 3. Service

- [x] 3.1 `CompleteRemoteJob` holds the run's lock across `CompleteJob`, the walker and `FinishRunIfDone` (D3); `resumeRemoteRun` no longer takes it itself. The existing double-dispatch test keeps passing. New test: two jobs of one run complete at the same moment and one unblocked task is dispatched once, with the run `running` until it finishes.
- [x] 3.2 The walker returns for a finished run (D4). Unit test: a run canceled after the completion was recorded and before the walker runs gets no dispatch.
- [x] 3.3 A dispatch failure fails the task and the run finishes `failed`; an error before the due tasks are known leaves the run `running` (D5). Unit tests with a mock that fails `CreateRemoteJob`, and one that fails `GetRemoteRunSnapshot`.

## 4. Docs

- [x] 4.1 `docs/remote-runners.md`: when a remote run finishes (after its dependents are decided). `CHANGELOG.md` `[Unreleased]`, `Fixed`: a task unblocked by the last running task now runs.

## 5. Verification

- [x] 5.1 `scripts/test-all.sh` all layers, `make lint`; the 100% unit coverage gate holds. Mutation check: letting `CompleteJob` finish the run again turns task 1.1's test red.
- [x] 5.2 Manual, on the remote-mode stack with the proposal's `build` → `deploy` pipeline:
  - after `build` the run reads `running`, not `passed`; `deploy` runs on the runner; then the run reads `passed`;
  - with `build` failing: `deploy` shows `skipped` and the run reads `failed`.

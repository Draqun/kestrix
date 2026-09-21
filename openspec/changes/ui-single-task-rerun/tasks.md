## 1. Service: split acceptance from execution

- [ ] 1.1 Add `RunService.StartTask` in `internal/service` that tracks the run, calls `markQueuedIfBusy` synchronously, and returns `(queued, error)`; verify with a unit test asserting it returns before the task executes
- [ ] 1.2 Move the background half (`waitAndStart`, `runTaskInternal`, `FinishRun`, the "canceled wins over local verdict" check, workspace cleanup) into a named method called from `StartTask`'s goroutine; verify with unit tests over the passed, failed, and canceled-while-queued paths
- [ ] 1.3 Keep `RunTask` working for its remaining callers and assert both paths share the scheduling halves; verify by running the existing `internal/service` unit tests unchanged
- [ ] 1.4 Assert a failure inside the background half still leaves the run terminal; verify with a unit test injecting a task-runner error and reading the run record

## 2. API: new response contract

- [ ] 2.1 Rewrite `handleRunTask` to create the run, call `StartTask`, and respond with `{"run_id", "project", "task", "status"}` where status is `running` or `queued`; verify with unit tests for both statuses
- [ ] 2.2 Assert the handler no longer blocks: with every slot busy, the response arrives while the task is still queued; verify with a unit test that fills the slots first
- [ ] 2.3 Keep the existing error paths (unknown project `404`, unknown task `404`, secret resolution failure `400` with the failed run recorded) and their workspace cleanup; verify with unit tests per path
- [ ] 2.4 Update the Swagger annotations and `docs/` for the changed response; verify `make docs` (or the swag target) regenerates without diff noise and `glab ci lint` stays clean
- [ ] 2.5 Add a **BREAKING** `[Unreleased]` CHANGELOG entry naming the removed `output` field and the replacement endpoint; verify by reading the rendered section

## 3. UI

- [ ] 3.1 Add `runTask(name, task)` to `ui/src/api/client.js` following the existing wrapper conventions; verify with a unit test over the request shape and error handling
- [ ] 3.2 Render a **Re-run** action per terminal task in the run detail job list, gated on `canOperate` and a terminal run, next to the manual-approval button; verify with a component test asserting it appears for a failed task and not for a manual-waiting one
- [ ] 3.3 Render the same action in the selected-job header; verify with a component test
- [ ] 3.4 Hide the action from a viewer; verify with a component test using a viewer role
- [ ] 3.5 Navigate to the returned `run_id` after acceptance and show its live status, including `queued`; verify with a component test asserting the view switches to the new run
- [ ] 3.6 Show the API error on the current run and stay there when the re-run is rejected; verify with a component test

## 4. Verification

- [ ] 4.1 Run `make test` and `make test-coverage`; verify green and 100% unit coverage
- [ ] 4.2 Run `make ui-test` and `make lint`; verify both are clean
- [ ] 4.3 Manual end-to-end on a live instance: run a pipeline with a failing task, re-run just that task from the run detail view, confirm the view lands on the new run, the new run contains only that task, and the original run is unchanged
- [ ] 4.4 Manual check of the queued path: set `KESTRIX_RUNNER_MAX_CONCURRENT_RUNS=1`, start a long run, re-run a task from another finished run, and confirm the response is immediate and the new run shows `queued` until the slot frees

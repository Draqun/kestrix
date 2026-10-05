## 1. Reproduce

- [ ] 1.1 Integration tests on Postgres through `RunService.Start` with the real run and job stores, one per case: (a) `lint` (trigger `repo.docs.changed`) and `deploy` (trigger `repo.app.changed`, depends on `lint`), only `app` changed: a runner can claim `deploy`; (b) the same pipeline with `app` and `docs` changed: `lint` is claimable and `deploy` is not until `lint` passes; (c) no task triggered: the run is `passed` and finished, with no result and no job; (d) a working directory that does not exist: `Start` returns the error, the run is `failed` with an error naming the cause, and it has no job. Red on `main`: (a) no job, (c) and (d) the run stays `running`. (b) is green and guards the rule.

## 2. Start

- [ ] 2.1 `eligibleForDispatch` ignores a dependency that is not among its nodes (design D1). Table-driven unit tests: a dependency outside the run is ignored; one inside still holds; a diamond whose root is outside the run; an outside dependency next to a failed inside one is blocked by the failed one. Test (a) is green.
- [ ] 2.2 `startRemote` finishes a run whose first pass dispatches nothing `passed` (D2). Test (c) is green; a unit test with a store whose `FinishRun` fails returns that error.
- [ ] 2.3 `startRemote` finishes the run `failed` when the archive or the snapshot fails, and records a task `failed` when its job cannot be created (D3), the error redacted. Test (d) is green. Unit tests with mocks that fail `SetRemoteRunSnapshot` and `CreateRemoteJob`: the first job failing gives a `failed` run at once; the second failing while the first is `pending` leaves the run `running` until the first completes, then `failed`.

## 3. Docs

- [ ] 3.1 `docs/remote-runners.md`: what a remote run does at start - a dependency outside the run, nothing triggered, a start that fails. `CHANGELOG.md` `[Unreleased]`, `Fixed`: a remote run no longer hangs `running` in these three cases.

## 4. Verification

- [ ] 4.1 `scripts/test-all.sh` all layers and `make lint`; the 100% unit coverage gate holds. Mutation checks: restoring the wait on a dependency without a result turns 1.1(a) red; removing the finish in `startRemote` turns (c) red.
- [ ] 4.2 Manual, on the remote-mode stack:
  - a pipeline where `deploy` depends on a task triggered by another repo: a push that touches only the repo that triggers `deploy` runs `deploy` on the runner and the run reads `passed`;
  - a push that triggers no task: the run reads `passed` at once.

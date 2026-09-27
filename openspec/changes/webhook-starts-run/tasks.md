## 1. Shared run start

- [ ] 1.1 Extract `startProjectRun` from `handleRunProject` (design D1): `BuildVarStore`, then the secret-error path through `failRunOnSecretError`, then `RunService.Start`, returning the run ID. `handleRunProject` calls it with unchanged behaviour. The existing handler tests pass unmodified.

## 2. Push event facts

- [ ] 2.1 `PushEvent` records whether the ref is a branch or a tag, and whether the push is a deletion (all-zero checkout SHA). `ParseGitLabPushEvent` fills both, and `default_branch`. Parser unit tests: branch, tag named like a branch, deletion.
- [ ] 2.2 A deletion is recorded `ignored` with reason `branch deleted` before any pipeline load (design D4). Handler test asserts that no load, acquisition or dispatch happens.

## 3. Start a run from a push

- [ ] 3.1 `resolveOne` decides per design D2/D3 and, for a push to the configured ref with at least one triggered task and no dispatch, calls `startProjectRun` with the loaded session. It hands the session's cleanup to the run instead of deferring it, and uses actor `webhook:<source name>`. The result carries `run_id`.
- [ ] 3.2 Handler tests:
  - push to `repo_ref` starts one run with the webhook's changed set and actor;
  - another branch, a tag, zero triggered tasks and the dispatch-configured case start none;
  - a secondary push to its `ci_repo` `ref` starts a run;
  - fan-out to two projects starts one run each;
  - a secret-resolution failure records a failed run;
  - the session is cleaned up exactly once on every path.
- [ ] 3.3 Integration test against Postgres: a signed delivery to `POST /webhook` creates a run row with `started_by` `webhook:<project>` that reaches a terminal status in `local` mode.

## 4. Docs

- [ ] 4.1 `docs/webhook-receiver.md`: pushes start runs, the configured-ref rule, dispatch replacing the run, deletion; response `run_id`. `CHANGELOG.md` `[Unreleased]`: `Added` entry.

## 5. Verification

- [ ] 5.1 `scripts/test-all.sh` all layers, `make lint` green; 100% unit coverage gate holds.
- [ ] 5.2 Manual, on local Docker Compose with a real GitLab project webhook (or a signed `curl` replay of a captured delivery):
  - a push to `repo_ref` starts a run visible in the UI, attributed to the webhook;
  - a push to another branch shows in Webhook Logs and starts nothing;
  - deleting a branch is logged `ignored`.

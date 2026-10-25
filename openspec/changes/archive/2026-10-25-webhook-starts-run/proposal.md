## Why

A push does not run anything in Kestrix. `POST /webhook` authenticates the
delivery, matches projects, loads the pipeline at the pushed revision and
resolves the triggered tasks. It records all that in webhook history. Then
`resolveAndDispatch` (`internal/web/handler_webhook.go`) either triggers a
GitLab CI pipeline, when dispatch is configured with a GitLab project ID, or
stops. The `local`, `docker` and `remote` modes run only when someone
presses Run in the UI or calls the API.

For a CI control plane this is the missing core loop: push → the triggered
tasks run. It also blocks any Git host other than GitLab
(`github-webhook-payload`): a GitHub push could at most be logged.

## What Changes

- A verified push to a registered project that triggers at least one task
  starts a Kestrix run with the project's mode. The run uses the pipeline and
  source revision the webhook already loaded, and the changed set the
  webhook computed. It goes through the same concurrency queue, secret
  resolution and redaction as a run started from the UI.
- Only a push to the configured ref starts a run: the project's `repo_ref`
  for its primary repository, the `ci_repo`'s own `ref` for a secondary
  one. A push to any other branch, or a tag push, is resolved and recorded
  as today but starts nothing.
- A project that dispatches to GitLab CI keeps doing only that; the dispatch
  is its execution.
- A push that deletes a branch is recorded as `ignored` without loading a
  pipeline. Today it reaches the pipeline load with the all-zero SHA.
- A webhook-started run is attributed to `webhook:<source name>`, using the
  kind `run-attribution` reserved for it. The webhook response carries each
  result's `run_id`.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `webhook-receiver`: a push starts a run; only the configured ref does;
  deletions are ignored.
- `run-attribution`: `webhook:` becomes a produced actor kind.

## Non-goals

- Runs for other branches or merge requests, and a per-branch secret
  policy. Every accepted run gets the project's secrets, so only the
  configured ref may start one.
- Starting runs in single-directory mode (no registered project).
- Deduplicating a redelivered push. A redelivery starts another run.
- Linking a webhook history row to its run in the Webhook Logs page.
- Changing when GitLab dispatch happens.
- Commit lists truncated by the Git host (GitLab sends the newest 20
  commits), which can under-report changed files.

## Impact

- API: webhook results gain `run_id`. Runs gain `started_by` values
  `webhook:<source>`.
- Behaviour: an instance with registered projects starts real runs on push
  where it only logged before. Kestrix has not been published, so no
  opt-in switch is added.
- No schema change.
- Code: `internal/web/handler_webhook.go`, `internal/web/webhook.go`
  (branch vs tag, deletion), `internal/web/handler_run.go` (shared run
  start), `docs/webhook-receiver.md`. The UI already shows a non-`user:`
  actor verbatim (`webhook:shop`).

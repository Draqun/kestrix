## Why

Running `remote-runner-registration`'s manual tasks 6.3-6.5 end to end found
that `remote` mode breaks guarantees every other mode keeps:

- A resolved runtime secret is stored and shown **in plaintext** in the run
  output. `handleRunnerJobTrace`/`handleRunnerJobComplete` redact only the
  server's own config secrets (`knownSecrets()`), never the run's resolved
  `secret_ref` values.
- The same secret sits in plaintext in the database at rest, in
  `remote_jobs.payload` and `runs.remote_snapshot`, and stays there after the
  run ends - `runtime-secrets` promises it never appears "in the database in
  plaintext".
- Silent-runner recovery counts reclaims from `claim_epoch`, which also moves
  on every claim, so `KESTRIX_RUNNER_JOB_MAX_RECLAIMS=1` reclaims zero times
  and the default `3` reclaims once. A job reclaimed while no runner is left
  stays `pending` forever instead of eventually failing.
- A runner that cannot start a task (the shipped image has no `bash`, so every
  `--mode local` task fails) reports no reason: the UI shows "(No output)".
- A remote task reads `PENDING` in the UI for its whole execution.

These block archiving `remote-runner-registration`.

## What Changes

- Trace and completion output are redacted against the run's resolved secret
  values on the server, and streamed through a boundary-safe redacting writer
  on the runner, so a secret split across two trace chunks is caught too.
- A job's variables and secret values, and a remote run's snapshot variables,
  are stored encrypted with the credential master key and deleted once the
  job/run is terminal; startup strips the plaintext left by earlier versions.
- A new `reclaim_count` drives the reclaim limit; a reclaimed job nobody
  claims within the timeout counts as another unanswered attempt.
- A completion report may carry an `error`, stored on the task result.
- Claiming a job records the task as `running`.
- The runtime image ships `bash`.
- UI: the project detail page shows the project's mode; unknown routes show a
  not-found page instead of a blank screen.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `remote-runner-execution`: redaction, secrets at rest, recovery counting,
  error reporting, running status, mode on the project page.
- `production-deployment`: the image can run `local`-mode tasks.
- `ui-responsive-layout`: a not-found route.

## Non-goals

- The "Changed: <task>" chip on a single-task re-run: `run.Changed` holds the
  task name and `resumeRemoteRun` reads it, so fixing the label needs a run
  marker and is left for its own change.
- Per-task output for remote tasks: a runner's trace streams into the run's
  output only, so selecting a remote job shows "(No output)" (found while
  verifying this change; existed before it).
- Encrypting the source archive, key rotation for job payloads, or redacting
  encoded forms of a secret (same limits as in-process mode).

## Impact

- Archive order: `remote-runner-registration` (after its 6.3-6.5 pass on
  this fix), then this change, then `remote-runner-management`.
- Schema: `remote_jobs.secrets_enc BYTEA`, `remote_jobs.reclaim_count INT`,
  `runs.remote_secrets_enc BYTEA`, plus an idempotent plaintext scrub.
- API: `PUT /api/runner-jobs/{id}/complete` accepts optional `error`; the
  claim payload gains `secret_values`. Older runners keep working.
- Code: `internal/store`, `internal/web`, `internal/redact`,
  `cmd/kestrix/runner.go`, `docker/Dockerfile`, `ui/src`.

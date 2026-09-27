## Why

`remote-runner-registration` shipped Phase 1 of remote execution: a runner
registers, long-polls for jobs, and any polling runner claims any pending job.
That is enough for one project on two interchangeable machines, and nothing
more. An operator cannot tell whether a registered runner is still alive, cannot
route a task to the one machine that has what it needs (an architecture, a
GPU, a deploy credential), and can only register or revoke a runner through
`curl` against `/api/runners` - there is not even a list endpoint to find a
runner's ID before revoking it.

The target spec already names all three gaps as `(Phase 2)` requirements in
`remote-runner-execution`. This change builds them, so Kestrix can take on a
second project or a heterogeneous set of runners without a manual workaround.

## What Changes

- **Runner liveness**: `runners.last_seen_at` is stamped on every
  authenticated runner request (poll, trace, complete) inside the existing
  token lookup - no extra round trip, no sweep loop. A runner is reported
  `online`, `busy` (holds a claimed job, which can legitimately go quiet longer
  than a poll), `offline` (silent past `KESTRIX_RUNNER_OFFLINE_AFTER`, default
  `60s` - three long-poll intervals), or `never seen`.
- **Runner tags and task matching**: an administrator assigns tags when
  registering a runner (`POST /api/runners {name, tags}`). A task declares
  `runner_tags = [...]` in its `ci_task` block. A job is only offered to a
  runner whose tags are a superset of the task's; an untagged task matches
  every runner. Tags are server-assigned, never runner-declared.
- **Runner management**: new admin-only `GET /api/runners` (name, tags,
  created, last seen, liveness; never the token or its hash), and a **Runners**
  section in Settings to register a runner (token shown once), see liveness,
  and revoke with confirmation.
- The three `(Phase 2)` requirements in `remote-runner-execution` lose the
  suffix and gain the scenarios above; `pipeline-definition` gains the
  `runner_tags` attribute.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `remote-runner-execution`: registration takes tags; liveness, tag matching
  and the management interface become normative.
- `pipeline-definition`: `ci_task` accepts optional `runner_tags`.

## Non-goals

- Editing a registered runner's tags or name - revoke and re-register.
- Runner-declared tags, labels on the runner CLI, or per-runner concurrency.
- Alerting or notifications when a runner goes offline.
- Warning at run start that no registered runner matches a task's tags - the
  job stays `pending`, visible as such (see design Risks).
- Tag matching for `local`/`docker` projects - `runner_tags` is ignored
  outside `remote` mode.

## Impact

- **Depends on** `remote-runner-registration` being archived first: this
  change's deltas modify requirements that only exist in that change's spec
  until it is archived (its tasks 6.3-6.5 are manual).
- Schema: `runners.tags TEXT[]`, `runners.last_seen_at TIMESTAMPTZ`,
  `remote_jobs.required_tags TEXT[]`, all idempotent `ADD COLUMN IF NOT
  EXISTS` with empty defaults, so existing runners and jobs behave as today.
- API: `GET /api/runners` (new), `POST /api/runners` accepts optional `tags`
  (backward compatible).
- Code: store, runner-job handlers, parser, remote dispatch, config, a new
  Settings section, `docs/remote-runners.md`.

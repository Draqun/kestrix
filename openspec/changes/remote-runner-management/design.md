## Context

Phase 1 (`remote-runner-registration`) stores runners as `runners(id, name,
token_hash, created_at, revoked_at)` (`internal/store/pgstore.go:174`) and
claims work in `ClaimNextJob` (`internal/store/pgstore_remote_job.go:102`):
the oldest `pending` job of a non-terminal run, `FOR UPDATE SKIP LOCKED`, for
whichever authenticated runner asks. `AuthenticateRunner`
(`pgstore_runner.go:51`) resolves a bearer token to a runner ID and touches
nothing. `POST`/`DELETE /api/runners` are the only management routes
(`internal/web/server.go:563-564`); the UI has no runner code at all.

## Goals / Non-Goals

**Goals:** make the three `(Phase 2)` requirements normative and built, with
the smallest change to the Phase 1 hot path (poll + claim).

**Non-Goals:** see `proposal.md`.

## Architecture

```
admin ──POST /api/runners {name,tags}──▶ runners(tags, last_seen_at)
admin ──GET  /api/runners──────────────▶   │ liveness computed on read
                                           │
runner ──GET /api/runner-jobs/next──▶ AuthenticateRunner
          (bearer token)                   UPDATE runners SET last_seen_at=now()
                                           WHERE token_hash=$1 AND revoked_at IS NULL
                                           RETURNING id, tags
                                                │
                                           ClaimNextJob(runnerID, tags)
                                           ... AND rj.required_tags <@ $tags
                                                │
dispatch: ci_task.runner_tags ──▶ remote_jobs.required_tags
```

## Decisions

### Liveness is a timestamp stamped during authentication

`AuthenticateRunner` becomes `UPDATE ... SET last_seen_at = now() ...
RETURNING id, tags` instead of a `SELECT`. Every runner endpoint already calls
it, so poll, trace and complete all refresh liveness with no new query. A
revoked runner's `UPDATE` matches no row, so a stale token never looks alive.
Status is derived on read, not stored: no sweep goroutine, nothing to keep
consistent.

- `never seen`: `last_seen_at IS NULL`.
- `busy`: holds a job in `claimed` status. Checked before staleness because a
  runner executing a quiet task sends nothing until it completes; calling it
  `offline` would be wrong and the Phase 1 reclaim loop already owns "silent
  while holding a job".
- `online`: seen within `KESTRIX_RUNNER_OFFLINE_AFTER`.
- `offline`: otherwise.

Alternative rejected: a heartbeat endpoint. It adds a runner-side loop and a
route for information the existing calls already carry.

Default `60s` is three long-poll intervals (`runnerLongPollInterval = 20s`,
`internal/web/handler_runner_jobs.go:24`), so one slow poll does not flap a
runner offline. Validated positive, like `KESTRIX_RUNNER_JOB_TIMEOUT`.

### Tags are assigned by the administrator, not declared by the runner

A runner's only credential is its token. If the runner announced its own tags
on poll, anyone holding any runner token could claim jobs tagged for a trusted
machine (e.g. `deploy`), including jobs carrying that project's secrets. Tags
set at registration keep "which machine may run which task" an admin decision.
The poll endpoint stays body-less.

### Matching is a Postgres array containment in the claim query

`runners.tags` and `remote_jobs.required_tags` are `TEXT[] NOT NULL DEFAULT
'{}'`. `ClaimNextJob` adds `AND rj.required_tags <@ $2` - the job's tags are
contained in the runner's. An empty `required_tags` is contained in every set,
so untagged tasks run anywhere and every pre-existing job matches. A tagged
runner still takes untagged jobs (superset semantics, GitLab's
`run_untagged: true` default). `SKIP LOCKED` ordering is unchanged; a job no
polling runner matches is simply skipped, so it never blocks jobs behind it.

Tags are normalised at the boundary (trimmed, lowercased, deduplicated,
`[a-z0-9._-]{1,64}`, at most 32 per runner/task) so `GPU` and `gpu` cannot
silently mismatch. Invalid tags fail registration with `400` and fail pipeline
parsing with the task name.

### `runner_tags` is a `ci_task` attribute, snapshotted onto the job

The HCL parser decodes it on `domain.Task`; `buildRemoteJobPayload`
(`internal/service/remote_dispatch.go:201`) copies it into
`remote_jobs.required_tags` at dispatch. Editing the pipeline later does not
retarget an already-pending job - the same snapshot rule Phase 1 applies to
the job payload. Outside `remote` mode the attribute is parsed and ignored.

### Management UI follows the Accounts section

`RunnersSection.jsx` + `useRunners(isAdmin)` mirror `AccountsSection`
(`ui/src/components/accounts/AccountsSection.jsx`): admin-only via
`roleAtLeast('admin')`, hook skips fetching for non-admins. Registration shows
the token once in a dialog with a copy button and the exact `kestrix runner`
command; closing it discards the token from component state.

## Security Considerations

- `GET /api/runners` is admin-only and never returns `token_hash` or a token.
- Tags widen nothing: they only restrict which runner may claim a job. A
  runner still authenticates only runner-job routes.
- Stamping `last_seen_at` turns token lookup into a single-row write. Runner
  endpoints have no request-rate limit (Phase 1 bounds body size and
  concurrent long-polls per token, not rate), but only a valid token reaches
  the write, and trace/complete already write per request, so the added cost is
  of the same order. An unknown or revoked token matches no row and writes
  nothing.

## Risks / Trade-offs

- **A task whose tags no runner has waits forever** (until the run is
  canceled). Accepted for this change: the run detail already shows the task
  `pending`; the docs name the cause. A warning at dispatch is a follow-up if
  it bites.
- **`busy` hides a runner that died mid-job** until the Phase 1 reclaim loop
  returns the job (`KESTRIX_RUNNER_JOB_TIMEOUT`); after that the runner reads
  `offline`. The window is bounded by an existing setting.
- **Clock**: liveness uses the database's `now()` for both write and read, so
  runner clock skew is irrelevant.

## Migration Plan

Idempotent `ALTER TABLE ... ADD COLUMN IF NOT EXISTS` in
`internal/store/pgstore.go`. Existing runners get `tags = '{}'` and
`last_seen_at = NULL` (`never seen` until their next poll, seconds after
upgrade). Existing pending jobs get `required_tags = '{}'` and match any runner,
exactly as today. Rollback: the old binary ignores the new columns.

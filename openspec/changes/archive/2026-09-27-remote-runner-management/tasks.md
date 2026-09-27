> Implement only after `remote-runner-registration` is archived - these
> deltas modify requirements that exist only in its spec until then.

## 1. Schema and store

- [x] 1.1 Add `runners.tags TEXT[] NOT NULL DEFAULT '{}'`, `runners.last_seen_at TIMESTAMPTZ`, `remote_jobs.required_tags TEXT[] NOT NULL DEFAULT '{}'` as idempotent `ADD COLUMN IF NOT EXISTS` in `internal/store/pgstore.go`; integration test that a pre-existing runner/job row reads back with empty tags
- [x] 1.2 Shared tag normaliser (trim, lowercase, dedupe, `[a-z0-9._-]{1,64}`, max 32) in `internal/domain` with unit tests for each rule
- [x] 1.3 `CreateRunner(name, tags)` persists normalised tags; `RunnerStore` port updated
- [x] 1.4 `AuthenticateRunner` becomes `UPDATE ... SET last_seen_at = now() WHERE token_hash = $1 AND revoked_at IS NULL RETURNING id`; integration tests: valid token stamps, revoked/unknown token writes nothing
- [x] 1.5 `ClaimNextJob(runnerID)` adds `AND rj.required_tags <@ (SELECT tags FROM runners WHERE id = $runnerID)` (the port keeps its signature); integration tests for superset match, withheld job, untagged job on a tagged runner, and unmatched-oldest-does-not-block
- [x] 1.6 `ListRunners()` returns unrevoked runners with tags, `created_at`, `last_seen_at`, and whether each holds a `claimed` job

## 2. Config

- [x] 2.1 `KESTRIX_RUNNER_OFFLINE_AFTER` (default `60s`, positive duration) in `internal/config/environment.go` next to `KESTRIX_RUNNER_JOB_TIMEOUT`; `.env.template`, wired the same way as `KESTRIX_RUNNER_JOB_TIMEOUT`; startup-failure test for a non-positive value

## 3. Pipeline definition and dispatch

- [x] 3.1 `domain.Task.RunnerTags` (`hcl:"runner_tags,optional"`), decoded by `internal/parser/hcl_parser.go`, normalised with 1.2; parser tests for valid, normalised, and invalid tags naming the task
- [x] 3.2 `buildRemoteJobPayload` / `CreateRemoteJob` snapshot the task's tags into `remote_jobs.required_tags`; unit test that a later pipeline edit does not change a pending job's tags

## 4. API

- [x] 4.1 `POST /api/runners` accepts optional `tags`; `400` naming an invalid tag; response echoes normalised tags
- [x] 4.2 Admin-only `GET /api/runners` returning `{id, name, tags, created_at, last_seen_at, status}` with `status` in `busy|online|offline|never seen` (derived per design.md); handler tests for each status, `403` for operator, and absence of token/hash fields
- [x] 4.3 Swagger annotations for both routes

## 5. UI

- [x] 5.1 `listRunners` / `createRunner` / `revokeRunner` in `ui/src/api/client.js`, `useRunners(isAdmin)` hook
- [x] 5.2 `RunnersSection.jsx` in Settings, admin-only (same gating as `AccountsSection`): table with name, tags, last seen, status chip; register dialog showing the token once with the `kestrix runner --server ... --token ...` command and a copy button; revoke with confirmation
- [x] 5.3 Vitest coverage: hidden for non-admins, token visible only until the dialog closes, revoke calls the API and removes the row

## 6. Docs and verification

- [x] 6.1 `docs/remote-runners.md`: tags (admin-assigned, superset matching, untagged runs anywhere), `runner_tags` in HCL, liveness states, the Settings page, and "a task whose tags no runner has stays pending"
- [x] 6.2 `CHANGELOG.md` `[Unreleased]` entry
- [x] 6.3 `make test`, `make test-coverage` (100%), `make lint`, `make ui-test`, `make ui-lint` green
- [x] 6.4 Manual: register two runners from Settings (one tagged `gpu`), run a pipeline with one `runner_tags = ["gpu"]` task and one untagged task, confirm the tagged task only ever runs on the `gpu` runner; stop one runner and confirm it reads `offline` after the threshold; revoke from the UI and confirm its next poll gets `401`

> 6.4 was run on 2026-09-26 against the branch image with Kestrix, Postgres
> and two runner containers (`gpu-box` tagged `GPU, linux` - normalised to
> `gpu`, `linux` - and untagged `plain-box`), both registered from Settings.
> With both up the `runner_tags = ["GPU"]` task ran on `gpu-box` and the
> untagged one on `plain-box`; with `gpu-box` stopped the tagged task stayed
> `pending` while `plain-box` ran the other, `gpu-box` read `offline` after
> the threshold (set to `KESTRIX_RUNNER_OFFLINE_AFTER=20s` for the run instead of
> the 60s default), and it claimed the waiting task once restarted. Revoking
> `plain-box` from the UI removed it from the list and its next poll got
> `401`.


## Why

Kestrix executes every task in-process, on the same host as the Kestrix
server: `local` runs bare `bash -c` on that host, `docker` runs a container on
that host, `gitlab-runner` invokes `gitlab-runner exec` on that host (and is
dead — `exec` was removed from gitlab-runner, per the wiki's known gaps). There
is no way to run a task on a different machine without going through an actual
external GitLab project as an intermediary (`dispatch-pipeline`, a separate
capability). An operator who wants to host the Kestrix control plane on a
small, cheap instance and execute tasks on their own separate machines —
including using Kestrix to build and test itself — has no path to do that
today.

This blocks a concrete near-term goal: running Kestrix on a small VPS with two
self-hosted machines executing its own CI, as the first step toward migrating
other projects off GitLab CI onto Kestrix.

## What Changes

- `Mode` becomes a genuinely per-project setting, with a fourth value,
  `remote`: every task of a `remote`-mode project dispatches as a job for a
  registered runner to claim and execute, instead of running in-process. This
  is a real change to how `Mode` works today, not just a new enum value —
  see Impact.
- A runner registers with a long-lived bearer token (issued by an admin via
  `POST /api/runners`) and polls `GET /api/runner-jobs/next` (long-poll) for
  work, executing with whatever local engine its own configuration selects
  (Docker or bare shell) — Kestrix does not dictate the runner's execution
  engine.
- Output streams back incrementally (`PATCH /api/runner-jobs/{id}/trace`,
  offset-based) through the same secret redaction applied to in-process task
  output. Storing it needs a new, append-shaped write rather than the
  existing `RunStore.FlushRunOutput`: that one overwrites the run's whole
  output column and assumes the single in-process writer holding the full
  buffer, which no longer holds once two jobs of one run execute on two
  runners concurrently — see design.md D9.
- A job left claimed with no trace or heartbeat past a configured timeout
  returns to `pending` for another runner, or fails after repeated timeouts —
  the same shape as `orphan-run-recovery`'s handling of a run orphaned by a
  server restart, triggered instead by a silent runner.
- This proposal specifies the full capability, including a second phase
  (runner liveness tracking, tags/labels and task-to-runner matching, a
  Settings UI for managing runners). **Only the first phase ships in this
  change's `tasks.md`** — see design.md's Phasing. Every second-phase
  requirement in the spec is marked `(Phase 2)` so an implementation audit
  does not credit this change with it.
- **BREAKING** for nothing existing: `remote` is a new, opt-in `Mode` value: a
  project already on `local`, `docker`, or `gitlab-runner` is unaffected.

## Capabilities

### New Capabilities
- `remote-runner-execution`: registering a runner, the job a runner polls for
  and executes, how its output and completion reach Kestrix, and what happens
  when a runner goes silent mid-job.

### Modified Capabilities
- `task-execution-isolation`: the control-plane-secrets-withheld guarantee
  ("Task environment is an explicit allowlist") extends to the job payload a
  remote runner receives — the same exclusion list, enforced at payload
  construction instead of env passthrough, since a remote runner has no access
  to the Kestrix host's environment at all.
- `access-control`: the runner-job endpoints sit outside `/api` with their own
  bearer-token authentication, the same shape as `/metrics` in the
  `observability-metrics-and-structured-logging` proposal — a new capability
  needing its own authentication statement, not a change to the existing `/api`
  route groups.
- `project-registration`: the project-delete guard's definition of an active
  run widens to include a `remote`-mode run with outstanding jobs, which by
  design has no execution goroutine for the existing definition to catch.
- `run-scheduling`: a `remote`-mode task occupies no local concurrency slot
  (`max_concurrent_runs` bounds in-process execution); its pacing is the
  registered runners' own availability instead.

## Non-goals

- Multi-tenant or untrusted runners. A registered runner is assumed to belong
  to the same operator as the Kestrix instance; there is no per-runner
  permission scoping beyond the bearer token.
- Per-task mode mixing within one pipeline (some tasks local, some remote).
  `Mode` stays a project-level setting in this change; mixing is a candidate
  for a later change if it turns out to matter.
- Autoscaling, runner pools, or any cloud-provisioning integration.
- Stopping a runner's in-flight execution on cancel. Kestrix has no inbound
  channel to a runner (design.md D8: the transport is poll-only, so a runner
  works from behind NAT) to push a stop signal; canceling a
  remote-mode run stops Kestrix from claiming/awaiting its remaining jobs but
  does not terminate a process already running on the runner — see spec.md
  "Cancelling a remote-mode run".
- Migrating existing GitLab-CI-hosted projects onto remote runners. This change
  only makes Kestrix itself capable of dogfooding its own CI; migration is a
  separate, later effort once this is proven.
- A Settings UI page for runners (Phase 2, specified but not built here).

## Impact

New `internal/domain` ports (`RemoteJobStore` and `RunnerStore`, or similar)
and Postgres tables (`remote_jobs`, `runners`), a new `internal/web` route
group with its own auth middleware, a new `kestrix runner` subcommand
poll-loop (not a second top-level binary — `cmd/kestrix/main.go` dispatches
every existing command through one `switch os.Args[1]`, and this change
follows that established single-binary convention rather than departing from
it) reusing the existing `internal/runner` `Executor` implementations, and a new
`docs/remote-runners.md` (linked from `docs/deployment.md`) covering
registering and running a remote runner — `docs/deployment.md` itself is
scoped to a single-host Compose install and doesn't fit a multi-machine
workflow.

`Mode` today is instance-wide, not per-project: it lives only on
`config.RunnerConfig` and is wired once into a single shared
`runner.PipelineExecutor{Mode: cfg.Runner.Mode}` in `internal/web/server.go`;
neither `domain.ProjectConfig` nor `domain.Project`/`model.Project` has a
`Mode` field, so every project on one instance necessarily runs under the
same mode today. Supporting a `remote`-mode project alongside `local`/
`docker`/`gitlab-runner` projects on the same instance therefore requires
adding `Mode` to `ProjectConfig` (and a migration for existing rows,
defaulting to the current instance-wide `RunnerConfig.Mode`), and replacing
the single shared `PipelineExecutor` with per-project/per-run executor
selection at dispatch time. This is real, non-trivial structural work, not a
one-line enum extension — see tasks.md 2.1. No change to any existing runner
mode's behavior once a project stays on it.

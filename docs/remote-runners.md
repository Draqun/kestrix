# Remote runners

A `remote`-mode project dispatches every task as a job for a registered
runner to claim and execute, instead of running it on the Kestrix host. This
is how you host the Kestrix control plane on a small, cheap instance and
execute tasks on your own separate machines - including using Kestrix to
build and test itself.

This guide is scoped to registering a runner and running `kestrix runner`.
For installing Kestrix itself, see [`deployment.md`](deployment.md), which
covers a single-host Compose install and doesn't fit a multi-machine
workflow like this one.

## How it works

A runner is a poll-loop, not a listener: it only ever makes outbound HTTPS
requests to the Kestrix server, so it works from behind NAT with no inbound
port, no public address, and no firewall hole.

```
Kestrix host                              Runner (your own machine)
─────────────                             ──────────────────────────
remote_jobs table                         kestrix runner --token T
     │                                          │
     │  pending ─────────────────────────> GET /api/runner-jobs/next
     │                                          │  (long-poll)
     │                                    200 + payload (script,
     │                                    resolved variables, source
     │                                    archive, claim epoch)
     │                                          │
     │                                    unpacks archive, executes
     │                                    with the runner's own
     │                                    configured engine
     │                                    (docker or local)
     │                                          │
     │  <───────────────────────────  PATCH .../trace (periodic)
     │  <───────────────────────────  PUT .../complete
```

The job payload never contains a container image or engine selection -
your runner's own `--mode` decides whether it runs tasks in Docker or as
bare shell subprocesses, the same choice `local`/`docker` mode makes on the
Kestrix host itself (see [`ci-tools.md`](ci-tools.md#runner)). It also never
contains a repository credential: Kestrix acquires the source once, on the
host, the same way every other mode does, and ships the resulting checkout
to the runner as an archive.

## 1. Register a runner

Registering a runner requires the admin role. The easiest way is
**Settings → Runners → Register runner**: give it a name and optional tags,
and the dialog shows the token together with the exact `kestrix runner`
command to start it. The same page lists every runner with its tags and
liveness, and revokes a runner after confirmation. Or use the API:

```bash
curl -X POST http://localhost:8080/api/runners \
  -H 'Authorization: Bearer <admin session token>' \
  -H 'Content-Type: application/json' \
  -d '{"name": "build-box-1", "tags": ["gpu", "linux"]}'
```

The response includes a bearer token:

```json
{"id": 3, "name": "build-box-1", "tags": ["gpu", "linux"], "token": "b7f2c1..."}
```

**This token is shown once.** Store it somewhere your runner machine can
read it (a secrets manager, an `.env` file with restrictive permissions);
Kestrix stores only its hash and cannot show it again. If you lose it,
revoke the runner and register a new one.

To revoke a runner (also admin-only):

```bash
curl -X DELETE http://localhost:8080/api/runners/3 \
  -H 'Authorization: Bearer <admin session token>'
```

A revoked runner's token stops authenticating immediately, and the running
`kestrix runner` exits on its next poll. Any job it had claimed is recovered
by silent-runner recovery (see below) once it goes quiet.

The runner-job endpoints (`/api/runner-jobs/*`) sit outside the `/api`
route group your session token authenticates: a runner's own bearer token
authenticates nothing there, and a session token does not authenticate the
runner-job endpoints either. The two credential spaces are entirely
separate.

## 2. Run `kestrix runner` on the remote machine

Copy the same `kestrix` binary you run the server with to the remote
machine - there is no separate runner binary; `runner` is a subcommand of
the same single binary (see the project's Impact notes for why: it's the
same convention every other command follows).

```bash
kestrix runner --server https://kestrix.example.com --token b7f2c1...
```

Or via environment variables (useful for a systemd unit or a container):

```bash
export KESTRIX_RUNNER_SERVER=https://kestrix.example.com
export KESTRIX_RUNNER_TOKEN=b7f2c1...
kestrix runner
```

By default the runner executes tasks as bare shell subprocesses on its own
host (`local` mode, matching the Kestrix host's own `local` mode). To run
tasks in Docker containers instead, the runner machine needs a working
Docker installation:

```bash
kestrix runner --server https://kestrix.example.com --token b7f2c1... --mode docker
```

or `KESTRIX_RUNNER_MODE=docker`. Docker-mode hardening (PID/memory/CPU
limits, dropped capabilities) applies exactly as it does for an in-process
`docker`-mode project - see
[`ci-tools.md`'s Docker mode hardening](ci-tools.md#docker-mode-hardening);
the same `KESTRIX_RUNNER_DOCKER_*` settings on the runner machine's own
environment control it, since the job payload names no image or engine.

The runner keeps polling until you stop it (Ctrl-C, or your process
supervisor's stop signal) or the server rejects its token. A `401` - `invalid
or revoked runner token`, for example after an administrator revoked the
runner - makes `kestrix runner` print the server's message and exit with a
non-zero status, because no retry can succeed; register a new runner and start
it with the new token. Any other poll failure, such as a `500` or a dropped
connection, is logged and retried every 5 seconds. It logs each claimed job
and its outcome to stderr. When a task fails before producing any output (for example its
shell is missing), the runner sends the reason with its completion report
and the run detail view shows it as the task's error.

**Upgrade runners together with the server.** A resolved runtime secret
(`secret_ref`) is redacted twice: the runner redacts its streamed output
before sending it, and Kestrix redacts every trace chunk and completion
again before storing it. Only the runner's pass catches a secret whose bytes
happen to be split across two trace chunks, so an older runner binary that
does not redact leaves that case open. Kestrix stores a job's variables and
secret values encrypted with the credential master key and deletes them
once the job or its run is finished.

## Runner tags

Tags route a task to the machines that can run it (an architecture, a GPU,
a deploy credential). They are **assigned by an administrator when the
runner is registered** - a runner never declares its own, so holding any
runner token is not enough to receive jobs meant for a trusted machine. To
change a runner's tags, revoke it and register it again.

A task names the tags it needs in its `ci_task` block:

```hcl
ci_task "train" {
  trigger     = "repo.ml.changed"
  runner_tags = ["gpu", "linux"]
  execute {
    repo   = "ml"
    script = "./train.sh"
  }
}
```

A job is offered only to a runner whose tags include **all** of the task's
tags. A task with no `runner_tags` runs on any runner, tagged or not. Tags
are trimmed, lowercased and deduplicated, must match `[a-z0-9._-]{1,64}`,
and at most 32 are allowed. They are copied onto the job when it is
dispatched, so editing the pipeline later does not retarget a job already
waiting. Outside `remote` mode `runner_tags` is accepted and ignored.

**A task whose tags no runner has stays `pending`** until a matching runner
is registered or the run is canceled; Kestrix does not warn about it.
Silent-runner recovery never fails a job that was never claimed.

## Runner liveness

Every request a runner makes (poll, trace, completion) records its
last-seen time. The Runners page and `GET /api/runners` report each runner
as:

| Status | Meaning |
|--------|---------|
| `busy` | Holds a claimed or running job (a quiet task sends nothing until it completes) |
| `online` | Seen within `KESTRIX_RUNNER_OFFLINE_AFTER` (default 60s, three long-poll intervals) |
| `offline` | Not seen for longer than that |
| `never seen` | Registered but has not polled yet |

## 3. Switch a project to `remote` mode

```bash
curl -X PUT http://localhost:8080/api/projects/my-app \
  -H 'Authorization: Bearer <admin session token>' \
  -H 'Content-Type: application/json' \
  -d '{"mode": "remote", ...}'
```

Every task of a `remote`-mode project now dispatches as a job instead of
running on the Kestrix host - including a single-task re-run and an
approved manual gate (see [`ci-tools.md`](ci-tools.md#runner) for how those
two differ from every other mode once a project is on `remote`).

### Task output

The run detail shows each task's own output. For a remote task that is
everything its runner streamed with `PATCH .../trace`, in order, followed by
any `output` in the completion report, which goes on its own line when the
trace ended mid-line. If [silent-runner recovery](#silent-runner-recovery)
returned the job to the queue, the task shows only the claim that completed
it; the run's aggregate output keeps what every claim streamed. Kestrix
redacts it and bounds it per
task by `KESTRIX_RUNNER_MAX_OUTPUT_BYTES`, the way an in-process task's output
is: past the bound it is cut and ends with the truncation marker. It appears
when the task completes, not while it runs; the run's aggregate output stays
live.

### Re-running one task

A single-task re-run dispatches one job, for that task, and nothing else.
Completing it does not dispatch, skip, or record any other task of the run,
whatever the pipeline's triggers say: a negated trigger such as
`!repo.docs.changed` holds for any set of changes, which is why a re-run of
`build` used to start `lint` and `deploy` as well. Approving a manual task
inside a whole run still resumes that run's dependents. Kestrix resumes only
runs that were started with a source snapshot, and a re-run has none.

The run is recorded with `rerun_task` set to the task's name and an empty
`changed`, and is labelled **Re-run: build** in the run list and the run
detail. Re-runs recorded before this label existed keep showing
**Changed: build**.

## Runner-job protocol

For anyone writing their own runner. Every call sends the runner's token as
`Authorization: Bearer <token>`.

**`GET /api/runner-jobs/next`** long-polls for up to 20s: `204` when nothing
is pending, otherwise `200` with the claimed job:

| Field | Meaning |
|-------|---------|
| `id`, `run_id`, `task_name` | The job, its run, and the task it executes |
| `claim_epoch` | Fencing token; send it back on every trace and completion |
| `payload.task` | The task definition (`name`, `execute.repo`, `execute.script`, `runner_tags`, ...) |
| `payload.repos` | Declared repositories (`name`, `path`, `kind`) to resolve the task's working directory inside the archive |
| `payload.default_image` | The project's default image, for a Docker-mode runner |
| `payload.variables` | The task's resolved variables, including runtime secrets, as a flat map |
| `payload.secret_values` | Which of those values are secrets: redact them from everything you send back |
| `payload.archive` | Base64 tar.gz of the source checkout |

**`PATCH /api/runner-jobs/{id}/trace`** with `{"epoch", "offset", "chunk"}`
appends output; `offset` is the number of bytes already accepted. `204` on
success, `409` when the epoch is stale or the job is not yours.

**`PUT /api/runner-jobs/{id}/complete`** with `{"epoch", "status", "output",
"error"}` reports the outcome once. `status` is `passed` or `failed`;
`output` is any output not already sent as trace; `error` (optional, at most
4 KiB kept) says why the task failed. `204` on success, `409` when the report
is fenced out.

Kestrix redacts known secrets from every chunk, `output`, and `error` again
before storing them. The task's result shows the accepted trace chunks
followed by `output` on its own line.

A request with a missing or non-bearer `Authorization` header is rejected
`401 missing or malformed Authorization header`. A bearer token Kestrix does
not accept, unknown or revoked alike, is rejected `401 invalid or revoked
runner token`: stop, because retrying cannot help. Kestrix answers `500`
when it cannot check the token at all; that is transient, so retry.

## Settings

| Setting | Env var | Default |
|---------|---------|---------|
| Runner-job request body bound | `KESTRIX_RUNNER_JOB_MAX_BODY_BYTES` | 20971520 (20 MiB) |
| Concurrent long-polls per runner token | `KESTRIX_RUNNER_JOB_MAX_CONCURRENT_POLLS` | 2 |
| Silent-runner timeout | `KESTRIX_RUNNER_JOB_TIMEOUT` | 120s |
| Max reclaim attempts before failing a job | `KESTRIX_RUNNER_JOB_MAX_RECLAIMS` | 3 |
| Silence before an idle runner is reported offline | `KESTRIX_RUNNER_OFFLINE_AFTER` | 60s |
| `kestrix runner`'s server URL (flag: `--server`) | `KESTRIX_RUNNER_SERVER` | none |
| `kestrix runner`'s bearer token (flag: `--token`) | `KESTRIX_RUNNER_TOKEN` | none |
| `kestrix runner`'s execution backend (flag: `--mode`) | `KESTRIX_RUNNER_MODE` | `local` |

All eight are also documented in `.env.template`.

## Silent-runner recovery

If a runner goes quiet mid-job (crashed, network partition, `kestrix
runner` killed) without reporting completion, Kestrix notices once the job
has been silent longer than `KESTRIX_RUNNER_JOB_TIMEOUT`: it returns the
job to `pending` for another runner to claim, up to
`KESTRIX_RUNNER_JOB_MAX_RECLAIMS` times, after which it fails the job
naming the reason. A returned job that no runner claims within the same
timeout counts as another attempt, so with every runner gone the job - and
its run - still fails after `MAX_RECLAIMS + 1` timeouts instead of waiting
forever. A job that was never claimed at all is left waiting. The original runner's own eventual, late report (if it
comes back) is fenced out by the job's claim epoch and rejected - see
[`ci-tools.md`'s Cancellation section](ci-tools.md#cancellation) for the
same fencing idea applied to cancellation.

## Non-goals

This guide covers registering and running a single-tenant fleet of runners
you already trust - see the project's own Non-goals for the full list
(multi-tenant/untrusted runners, per-task mode mixing, autoscaling,
editing a runner's tags after registration, offline alerts, and migrating
existing GitLab-CI-hosted projects onto remote runners are all out of scope
for this feature).

## Context

See proposal.md — Why. The constraints that shape this design:

- `internal/runner/runner.go:178`, `:214`, `:502`: `LocalExecutor`,
  `GitLabRunnerExecutor`, and the `docker run -e` loop each build the task
  environment independently from `config.Current().Environ()`, which is the
  normalized copy of `os.Environ()` (`internal/config/environment.go:221,263`).
- `runner.go:130-149`: Docker arguments are `run --rm --name kestrix-<task>-<ns>
  -v <workdir>:/workspace -w ... -e ... <image> sh -c <script>`. The container
  name is already deterministic per task invocation, which is what a stop needs.
- `internal/service/service.go:164-176`: `RunService.Start` runs
  `executeRun` in a goroutine with the caller's `ctx`, and
  `internal/web/handler_run.go:55` passes `context.Background()`. Nothing holds
  a cancel function; `handleCancelRun` (`handler_run.go:298`) calls
  `runStore.CancelRun` and returns.
- `commandContext = exec.CommandContext` (`runner.go:24`) is the one seam the
  unit tests stub (`unit_coverage_test.go:41`). `exec.Cmd` in Go 1.25 exposes
  `Cancel func() error` and `WaitDelay`, which is the stdlib's process-group
  kill hook; no goroutine or signal plumbing of our own is required.
- `runner-unit-only-coverage` holds `internal/runner` at 100% unit-only
  statement coverage. Anything that actually signals a process must be
  reachable through an injectable function, or it becomes untestable dead code.
- The dogfood pipeline reads `DOCKER_REGISTRY`, `DOCKER_USERNAME`,
  `DOCKER_PASSWORD`, `DEPLOY_HOST`, `KESTRIX_TEST_DATABASE_URL` from the host
  (`scripts/docker-push.sh:6-21`, `scripts/test.sh:22`). The allowlist breaks
  it unless those names are passed through or delivered by `runtime-secrets`.
- `RunnerConfig` (`internal/config/config.go:98`) carries `mode` and `executor`
  from the YAML file; typed `KESTRIX_*` fields live in `environment.go`. The
  `source_cache` block already shows how one setting appears in both.

## Goals / Non-Goals

**Goals:**

- One function decides what a task sees; three executors call it.
- The Docker executor's default invocation is safe without operator tuning.
- Cancel means the process is gone, and the run record never says `canceled`
  before that is true.
- No new dependency; `os/exec` and `syscall` from the stdlib are enough.

**Non-Goals:**

- Sandboxing `bash -c` on the host beyond its environment. If that is required,
  the answer is `docker` mode, not a bespoke jail.
- A per-project or per-task passthrough list. One process-wide list keeps the
  boundary visible in one place; finer scoping is `runtime-secrets`' job.
- Rewriting the executors around a container SDK. The `docker` CLI keeps the
  test seam and the operator's `docker ps` mental model.

## Architecture

```
  config.Current() ──▶ TaskEnv(taskVars) ──────────────┐
    base: PATH HOME LANG LC_ALL TMPDIR TERM USER          │   one function,
    + CI_PROJECT_DIR                                      │   three callers
    + KESTRIX_RUNNER_ENV_PASSTHROUGH ∖ denied             │
    + pipeline variables (win on collision)               │
                                                          ▼
   ┌──────────────┐   ┌──────────────────┐   ┌────────────────────┐
   │ LocalExecutor│   │ DockerExecutor   │   │ GitLabRunnerExec.  │
   │ bash -c      │   │ docker run --rm  │   │ gitlab-runner exec │
   │ Setpgid      │   │  --cap-drop ALL  │   │                    │
   │ Cancel:      │   │  --security-opt  │   │ Cancel: kill group │
   │  kill(-pgid, │   │   no-new-priv    │   └────────────────────┘
   │   TERM)      │   │  --pids-limit N  │
   │ WaitDelay:   │   │  --memory M      │
   │  grace→KILL  │   │  --cpus C        │
   └──────▲───────┘   │  --network NET   │
          │           │  [--user U]      │
          │           │ Cancel:          │
          │           │  docker stop -t  │
          │           │   grace <name>   │
          │           └────────▲─────────┘
          │                    │  ctx.Done()
   ┌──────┴────────────────────┴─────────┐
   │ RunService                          │
   │  Start: ctx, cancel := WithCancel   │
   │         active[runID] = cancel      │
   │  Cancel(runID): cancel(); wait ≤    │
   │         grace+kill for executeRun   │
   │         to return; then store       │
   │         CancelRun; else keep running│
   └─────────────────────────────────────┘
          ▲
   POST /api/projects/{name}/runs/{id}/cancel
```

## Decisions

### D1. One allowlist function at the configuration boundary

`config.Environment` gains `TaskEnv(vars map[string]string) []string`. It emits
the fixed base set from the snapshot, `CI_*` entries the caller supplies,
passthrough names, then pipeline variables last so they overwrite. All three
executors replace `config.Current().Environ()` with it. `Environ()` stays for
the dispatch path, which `runtime-configuration` still covers.

Alternative considered: a denylist (`KESTRIX_*`, `DATABASE_URL`, `DOCKER_*`).
Rejected — the next secret an operator exports (`AWS_SECRET_ACCESS_KEY`,
`GITLAB_TOKEN`) is leaked by default. An allowlist fails safe when the
deployment changes.

Alternative considered: an empty environment plus `PATH`. Rejected — `bash`
without `HOME` and `TMPDIR` breaks `go build`, `npm`, `git`, and the dogfood
scripts in ways that look like pipeline bugs, not isolation.

### D2. Passthrough is explicit, process-wide, and has a hard floor

`KESTRIX_RUNNER_ENV_PASSTHROUGH` is a comma-separated list of exact names.
Five names are refused at startup regardless: the two key paths,
`DATABASE_URL`, the GitLab token, and the webhook secret. Validation lives
beside the other startup checks in `environment.go` so a bad list fails the
process before the listener opens, the same way a bad key file does.

`KESTRIX_TEST_DATABASE_URL` is deliberately not on the floor: the dogfood test
task needs it, and the test helper already refuses any database whose name does
not end in `_test`, so passing it through cannot reach live data.

This is how the dogfood pipeline keeps working today: the deployment lists
`DOCKER_REGISTRY,DOCKER_IMAGE,DOCKER_USERNAME,DOCKER_PASSWORD,DEPLOY_HOST,KESTRIX_TEST_DATABASE_URL`.
That list is a visible smell by design — `DOCKER_PASSWORD` in a task
environment is exactly what `runtime-secrets` exists to remove, and the
passthrough list shrinks as that change lands.

### D3. Docker hardening is default-on, and network is the only open default

`--cap-drop ALL`, `--security-opt no-new-privileges`, `--pids-limit`,
`--memory`, `--cpus` are unconditional with defaults (`256`, `2g`, `2`).
`--network` defaults to `bridge` because `deploy.sh` and `docker-push.sh` need
egress; an operator sets `none` for build-only fleets. `--user` is emitted only
when configured, because most images assume root at build time and a wrong
UID breaks every `npm ci` before it breaks any attacker.

Alternative considered: `--read-only` root filesystem. Rejected for now —
`go build` and `npm` write outside `/workspace` (`$HOME/.cache`, `/tmp`), and a
tmpfs for each is tuning we cannot default correctly. Left as an operator
follow-up.

### D4. Cancellation uses `exec.Cmd.Cancel` and a process group

`LocalExecutor` sets `SysProcAttr{Setpgid: true}`, `cmd.Cancel = func() error
{ return killGroup(cmd.Process.Pid, syscall.SIGTERM) }`, and `cmd.WaitDelay =
grace`; the stdlib sends `SIGKILL` to the process after `WaitDelay`, and the
cancel function is extended to kill the group again with `SIGKILL` when
`WaitDelay` elapses (`os/exec` calls `Cancel` once, so the second signal is our
one line in a deferred timer). `killGroup` is a package variable defaulting to
`syscall.Kill(-pgid, sig)` so the unit tests replace it.

`DockerExecutor` sets `cmd.Cancel` to run `docker stop -t <grace> <name>`; the
`docker run` client exits once the container does. `GitLabRunnerExecutor`
takes the process-group path.

Alternative considered: rely on `exec.CommandContext`'s default kill. Rejected
— it kills `bash` only; `go test`, `npm`, and `docker build` children survive
as orphans, and a `docker run` client killed with `SIGKILL` leaves the
container running.

### D5. `RunService` owns the cancel function; the store is written last

`Start` wraps the caller's context with `WithCancel` and stores the cancel in
`active map[int]context.CancelFunc` under a mutex; `executeRun` deletes it on
return. `RunService.Cancel(runID)` cancels, waits up to `grace + 5s` on a
per-run done channel, and only then calls `runStore.CancelRun`. If the wait
times out, it returns an error and leaves the run `running`, because a status
that says `canceled` while a deploy script is still executing is worse than one
that says `running` and is true.

`handleCancelRun` calls `runService.Cancel` instead of the store directly. A
run with no active entry (manual wait, or a run from before a restart) takes
the old status-only path, which is what the "Cancel with no running process"
scenario requires.

Alternative considered: keep `CancelRun` in the store and have `executeRun`
poll the status. Rejected — a poll interval is a latency and a race; a cancel
func is neither.

## Security considerations

- Pipeline HCL is untrusted input. This supersedes `openspec/design.md`
  "Pipeline definitions are trusted — they are managed by the project owner":
  the owner registers the project, but every committer authors its pipeline.
- After this change a `local` task still runs as the Kestrix service account and
  can read the key files by path if it guesses them. The allowlist removes the
  paths and the database URL, which turns a certain compromise into a search;
  it does not make `local` mode multi-tenant safe. Docs must say so.
- `no-new-privileges` plus `cap-drop ALL` neutralize setuid binaries in the
  task image; `--pids-limit` bounds fork bombs; `--memory`/`--cpus` bound a
  noisy neighbour. None of this constrains a task's network unless `none`.
- The passthrough list is logged by name at startup, never by value.
- `docker stop` by name is safe from name injection: the name is built from the
  task name Kestrix already validated when parsing HCL plus a timestamp.

## Risks / Trade-offs

- [Existing pipelines lose host variables silently] → the run log prints the
  effective allowlist names on the first task, and the CHANGELOG marks the
  change breaking with the exact `KESTRIX_RUNNER_ENV_PASSTHROUGH` needed by the
  dogfood pipeline.
- [`WaitDelay` semantics differ from the double-signal we want] → tested with an
  injected `killGroup` recording `(pgid, signal)` pairs; the integration test
  in `runner_test.go` (excluded from unit coverage) runs a real `sleep` child.
- [Docker defaults too small for a real build] → defaults are configuration,
  and the failure surfaces as `failed` with the OOM in output, not a hang.
- [`Cancel` waiting inside an HTTP handler] → bounded by `grace + 5s` (15 s at
  defaults); the UI already polls run state, so the handler returning an error
  after 15 s is visible and recoverable.

## Migration Plan

1. Deploy with `KESTRIX_RUNNER_ENV_PASSTHROUGH` set to the names the existing
   pipelines read from the host; the dogfood list is in the CHANGELOG.
2. Run one representative pipeline per project; a missing variable fails fast
   in the task, not later.
3. Rollback is redeploying the previous binary; no schema changes.

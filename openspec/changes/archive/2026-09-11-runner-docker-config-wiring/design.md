## Context

See proposal.md — Why.

```
config.yaml runner:      ──┐
                           ├──► ApplyRunnerEnvironment() ──► Config.Runner
KESTRIX_RUNNER_DOCKER_*  ──┘                                 (no reader today)

KESTRIX_RUNNER_DOCKER_*  ──► Environment ──► config.Current() ──► DockerExecutor
```

Two facts settle the shape of this change:

1. `ApplyRunnerEnvironment` (`internal/config/config.go:305-331`) reconciles the
   two sources **into `Config.Runner`**. It does not write to `Environment`.
2. The runner never reads `Config.Runner`. It reads the `Environment` snapshot
   returned by `config.Current()` at three cancel-grace call sites, plus the
   `DockerExecutor` construction site that carries the values as struct fields:
   - `internal/runner/runner.go:451-460` — `DockerExecutor{PIDs, Memory, CPUs,
     User, Network, Grace}` built from the snapshot (the `Grace` field, read
     via `e.Grace` at `runner.go:228`; `DockerExecutor.RunTask` never calls
     `config.Current()` itself)
   - `internal/runner/runner.go:260,270` — `RunnerCancelGrace`, local execution
     (`LocalExecutor.RunTask`)
   - `internal/runner/runner.go:294,303` — `RunnerCancelGrace`, gitlab-runner
     execution (`GitLabRunnerExecutor.RunTask`)
   - `internal/runner/runner.go:619,629` — `RunnerCancelGrace`, manual approval
     (`RunOne`, reached via `PipelineExecutor.RunTask`, which has no `Mode`
     switch and always runs `bash -c` on the host — see Known Gap below)

Consequence: adding a bare `cfg.ApplyRunnerEnvironment(env)` call to
`cmd/kestrix/main.go` — the fix the first draft of this proposal described — is
**not sufficient**. It populates `Config.Runner`, which nothing reads. The
reconciled values must reach the `Environment` snapshot (or the runner must be
taught to read `Config`) before a `config.yaml` value can affect a container.

The serve path that matters is `internal/web/server.go:264` →
`runner.PipelineExecutor{Mode: cfg.Runner.Mode}` → `runner.Run` →
`DockerExecutor`.

## Goals / Non-Goals

**Goals:** a value set under `runner:` in `config.yaml` takes effect at the
`DockerExecutor` construction site and at every one of the three
`RunnerCancelGrace` read sites above, exactly as its `KESTRIX_RUNNER_*`
equivalent does today, when the environment variable is unset. The manual
approval path (`RunOne`) reads the grace period from the swapped snapshot like
the other sites, but still executes on the host even in Docker mode — see
Known Gap below.

**Non-Goals:** changing the precedence rule (config file wins, per
`runtime-configuration`), adding new settings, or changing how `DockerExecutor`
applies the values it is given.

## Decisions

### D1: Reconcile into a derived `Environment` snapshot and swap it in once at startup

Four ways to close the gap were considered.

**Option A — mutate the live `Environment` in place.** `main.go` calls
`ApplyRunnerEnvironment`, then writes the reconciled `cfg.Runner.*` values back
onto the `*Environment` returned by `config.Current()`.

- For: smallest diff, roughly five lines. Fixes the construction site and all
  three cancel-grace read sites at once.
  `internal/runner` and `internal/domain` untouched.
- Against: `Environment` is documented as an immutable snapshot
  (`internal/config/environment.go:451`, "immutable environment snapshot").
  In-place mutation of a shared global contradicts that contract.

**Option B — build a derived copy and replace the singleton (chosen).**
`Environment` gains `WithRunnerConfig(*Config) *Environment`, returning a copy
with the six runner fields overwritten from the reconciled `Config`; a
package-level `ReplaceCurrent` swaps `environmentLive` once, during startup.

- For: fixes the construction site and the same three cancel-grace read
  sites. The snapshot itself is never mutated —
  only the pointer is replaced, once, before any goroutine starts. Keeps the
  documented immutability contract intact. `internal/runner` and
  `internal/domain` untouched.
- Against: introduces a startup-only global swap that must cooperate with
  `environmentOnce` (`internal/config/environment.go:115-116, 420-423`).
  Roughly fifteen lines rather than five.

**Option C — thread the values through `runner.Options` / `domain.RunnerOptions`.**
Add the six settings to the domain port and the runner options struct, populate
them from `cfg.Runner` in `internal/web/server.go`.

- For: cleanest layering. No global mutation. The dependency is explicit.
- Against: breaks this change's non-goal of leaving `internal/runner` alone.
  Touches `internal/domain/ports.go`, `internal/runner/pipeline_executor.go`,
  `internal/runner/runner.go`, `internal/web/server.go` and
  `cmd/kestrix/main.go`. The three `RunnerCancelGrace` sites call
  `config.Current()` from deep inside task execution, so covering them means
  rewriting those paths too — or leaving `cancel_grace` inconsistent with the
  five Docker settings. Largest coverage cost against the 100% unit gate.

**Option D — give `PipelineExecutor` the resolved `Config`.** Extend
`PipelineExecutor` (which already receives `cfg.Runner.Mode`) with the runner
settings and build `DockerExecutor` from them.

- For: fewer fields to thread than Option C; `internal/web/server.go:264` is a
  natural place for it.
- Against: also breaks the `internal/runner` non-goal, and does not reach the
  `config.Current()` calls at `runner.go:270` (local), `runner.go:303`
  (gitlab-runner) and `runner.go:629` (`RunOne` / manual approval), which
  have no access to the executor's configuration.

**Chosen: Option B.** It is the only option that repairs the
`DockerExecutor` construction site and all three `RunnerCancelGrace` read
sites — `cancel_grace` included — without either breaking the immutability
contract (Option A) or expanding the change into `internal/runner` and
`internal/domain` (Options C and D), which would still leave `cancel_grace`
as separate follow-up work.

Shape:

```go
// internal/config/environment.go
func (e *Environment) WithRunnerConfig(c *Config) *Environment
func ReplaceCurrent(env *Environment) *Environment // returns previous snapshot
```

```go
// cmd/kestrix/main.go, after config.Load
if err := cfg.ApplyRunnerEnvironment(env); err != nil { /* exit 1 */ }
env = env.WithRunnerConfig(cfg)
config.ReplaceCurrent(env)
```

The rebinding matters: `cmdServe` keeps using its local `env` for roughly
fifteen further fields (`MasterKeyPath`, `HTTP*Timeout`, `DatabaseURL`, …)
down to `cmd/kestrix/main.go:298`. Replacing the singleton without rebinding
would leave two live snapshots that differ in the six runner fields for the
rest of the process.

### D4: `ReplaceCurrent` cooperates with the singleton contract

`ReplaceCurrent` swaps `environmentLive` after `Initialize`, which the
`runtime-configuration` "Startup-initialized immutable environment singleton"
requirement forbids as written ("subsequent configuration consumers SHALL read
the same snapshot"). The delta therefore also MODIFIES that requirement to
permit exactly one startup-time replacement: `ReplaceCurrent` MUST be called
after `config.Load` and before the server and runner are constructed (while
still single-threaded), and a `ReplaceCurrent` call made before `Initialize`
would be overwritten by the `sync.Once` body
(`internal/config/environment.go:420-423`), so `Initialize` MUST have run
first.

## Known Gap: manual approval bypasses Docker hardening (pre-existing)

`PipelineExecutor.RunTask` (`internal/runner/pipeline_executor.go:80-105`)
has no switch on `e.Mode`: it falls straight through to `RunOne`, which
builds `commandContext(ctx, "bash", "-c", task.Execute.Script)` at
`internal/runner/runner.go:624`. So with `runner.mode: docker`, every
manually approved task bypasses `--cap-drop ALL`, `--pids-limit`, `--memory`,
`--network` and runs unsandboxed on the Kestrix host. None of the six
settings this change wires can reach that execution path — this change only
routes the *cancel-grace value* to `RunOne`'s timer, not the sandbox.

This defect predates this change, but fixing the sandbox routing is a
separate change (it requires touching `internal/runner`, rejected as
out of scope in D1). It is recorded here so the cancel-grace scenario cannot
be misread as proof that Docker hardening applies to approvals, and so the
`task-execution-isolation` promise ("when any runner executor starts a task
process or container") has a tracked follow-up: #1
(`https://gitlab.com/kestrix/kestrix/-/work_items/1`).

### D2: `runner.cancel_grace` is in scope alongside the five Docker settings

`ApplyRunnerEnvironment` already reconciles `CancelGrace`
(`internal/config/config.go:326-329`), and it suffers from exactly the same
defect: the value is written to `Config.Runner` and never read. Option B repairs
it in the same step, at no extra cost. Excluding it would mean shipping a fix
that leaves one of the six settings in the `runner:` block still silently
inert.

### D3: `kestrix run` stays out of scope

`cmdRun` (`cmd/kestrix/main.go:83-125`) never calls `config.Load` at all, so no
`config.yaml` value — runner-related or otherwise — reaches the CLI run path.
That is a distinct defect with a distinct fix (loading configuration in the
`run` subcommand) and is deliberately left for separate work. This change fixes
the `serve` path.

## Security Considerations

None. This makes an already-reviewed, already-tested reconciliation function
(`config_test.go` covers it today) take effect in production. It does not change
which values are considered valid, and `Environment.ValidationError` is still
checked before any value is applied.

## Migration Plan

No data migration. Two behavior changes to call out in the release notes:

- An operator who already set `runner.docker_pids`, `runner.docker_memory`,
  `runner.docker_cpus`, `runner.docker_user`, `runner.docker_network` or
  `runner.cancel_grace` in `config.yaml` (where it had no effect) will see it
  take effect after this deploys.
- Where both the config file and the matching `KESTRIX_RUNNER_*` variable are
  set to different values, the config file now wins, per the existing documented
  precedence in `runtime-configuration`.

Rollback is a plain revert.

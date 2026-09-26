## Context

See proposal.md — Why. This spec covers the full capability (Phase 1 and
Phase 2); only Phase 1 is implemented by this change's `tasks.md` — see
Phasing below.

Relevant seams already in the codebase:

- `internal/domain/ports.go`'s `RunStore` already has an optimistic-claim
  precedent: `StartQueuedRun(id) (ok bool, err error)` — `ok` false means
  another path already claimed the row. The new job-claim mechanism reuses
  this shape.
- `internal/runner.Executor` (`RunTask(ctx, task, opts) (output string, err
  error)`) is the boundary a remote runner's poll-loop wraps. `Options`
  (`internal/runner/runner.go`) itself is not part of the job payload —
  fields like `Variables domain.VariableResolver` (an interface, not
  serializable), `RepoPaths`, `SourceSessions`, `SourceAcquirer`, and
  `CredentialResolver` are control-plane machinery the runner never receives.
  The runner-side poll-loop reconstructs a `model.Task` and a minimal
  `Options` locally from the job payload (script, resolved variables, source
  archive) before calling `RunTask` — see D7.
- `internal/service/redact.go`'s `redactingWriter` already redacts known
  secret values as bytes are written; the trace-append endpoint reuses it
  before storing. Storage is *not* `RunStore.FlushRunOutput` — see D9.
- `RunStore.AppendTaskResult`'s `UPDATE runs SET task_results =
  task_results::jsonb || $1::jsonb` is the existing precedent for a
  concurrency-safe append to a run row; the trace write follows its shape.
- `cmd/kestrix/main.go:305`'s `FailInterruptedRuns("interrupted by server
  restart")` is the existing precedent for reconciling work orphaned by a
  process going away; silent-runner recovery is the same idea, triggered by a
  stale job instead of a server restart.
- `internal/web/server.go`'s `buildRouter` already demonstrates a route group
  outside `/api` with its own authentication: `/webhook`
  (`r.With(maxBody(s.webhookMaxBodyBytes())).Post("/webhook", s.handleWebhook)`),
  secret-based auth inside the handler, its own body-size bound distinct from
  `/api`'s — the runner-job group follows the same shape. (The
  `observability-metrics-and-structured-logging` proposal's `/metrics` would
  be a second instance of this shape once implemented, but it isn't landed
  code yet — `/webhook` is the existing precedent to cite.)

**Sequencing note:** this change and the pending `ui-single-task-rerun`
change both add a `MODIFIED Requirements` delta to `run-scheduling`'s
"Concurrent run limit". Whichever is implemented and archived second must
re-read the just-landed main spec (not this delta file) before writing its
own delta, per this repo's `openspec-apply-change` convention — the two
deltas do not conflict in content, only in which one runs first.

```
Kestrix host (small VPS)                 Runner (operator's own machine)
─────────────────────────                ───────────────────────────────
remote_jobs table (Postgres)             kestrix runner --token T
     │                                         │
     │  pending ──────────────────────>  GET /api/runner-jobs/next (long-poll)
     │  SELECT ... FOR UPDATE                  │
     │  SKIP LOCKED, epoch++ ─claimed─>  200 + payload (script, resolved vars,
     │                                         source archive, claim epoch)
     │                                         — no image/engine field
     │                                         │
     │                                    unpacks archive, executes with
     │                                    runner's own configured engine
     │                                    (docker or bash)
     │                                         │
     │  epoch match? ── redact ──────────  PATCH .../trace (periodic, offset,
     │  → append to output                     epoch)
     │                                         │
     │  epoch match? → AppendTaskResult,       │
     │  dependency graph resumes  <───────  PUT .../complete {status, output,
     │                                         epoch}
```

## Goals / Non-Goals

**Goals (this change, Phase 1):**

- A task can execute on a different machine than the Kestrix host, reachable
  only outbound (NAT-safe), with no external CI system as intermediary.
- Reuse every existing execution and redaction mechanism; add the transport
  and claim layer, plus one genuinely new piece — see below.
- Spec the full target capability so Phase 2 has no surprise requirements
  later, without building Phase 2 now.

**The one thing that is not reuse:** resuming a run's dependency graph after a
job completes. There is no existing primitive to call. The only walker is
`taskLoop` (`internal/runner/runner.go`), a blocking in-memory loop over a
pre-computed `[]dag.Node` that tracks failures in a local map for the
lifetime of one `Run` call; the manual-gate helpers (`manualTaskContext`,
`taskResultsByName`) only validate one named task's own dependencies.
Nothing computes, from a run's *persisted* `task_results`, which
not-yet-dispatched tasks have just become eligible. Remote mode needs exactly
that — stateless over stored results, callable from the completion handler —
so `taskLoop`'s blocked/manual/ordering rules have to be expressed in that
form. It is its own task (tasks.md 3.6), not a line in the completion
handler.

**Non-Goals:** see proposal.md — Non-goals. Additionally: this design does not
address the `dispatch-pipeline` capability (external GitLab CI delegation) at
all — the two remain unrelated, parallel ways to run work outside the Kestrix
process.

## Decisions

### D1: Job claim via `SELECT ... FOR UPDATE SKIP LOCKED`, fenced by a claim epoch

Postgres's native pattern for multiple workers polling one queue without an
application-level lock or a busy-retry loop. One query, one round trip, no
races. This is the standard, well-proven solution for exactly this problem —
no alternative was seriously considered.

Each successful claim (including a reclaim by silent-runner recovery, D5)
increments a per-job `claim_epoch` integer and returns it in the poll
response. Trace-append and completion both carry the epoch they were issued
and are rejected as a no-op once the job's stored epoch has moved past it.
This closes the gap where a merely-slow (not dead) original runner keeps
calling `trace`/`complete` after reclaim: the epoch check, not clock
assumptions, is what keeps "job claimed exactly once" true across a reclaim,
and also makes a runner's own retried completion call (e.g. after a dropped
HTTP response) safe to resend under the same epoch without double-recording
the result.

### D2: `Mode: remote` is project-level, not per-task — and `Mode` itself moves from instance-wide to per-project

`Mode` is not project-level today: it lives only on `config.RunnerConfig`
(`internal/config/config.go`), a single instance-wide setting wired once into
one shared `runner.PipelineExecutor{Mode: cfg.Runner.Mode}`
(`internal/web/server.go`) — every project on one Kestrix instance runs under
the same mode. This change makes `Mode` a genuine per-project field on
`domain.ProjectConfig` (migrated, defaulting existing rows to the current
instance-wide value) and replaces the single shared `PipelineExecutor` with
per-project executor selection at dispatch time (proposal.md Impact,
tasks.md 2.1) — a real structural change, not an enum extension riding on an
already-per-project setting.

Within that corrected premise, the task-level dispatch granularity that
motivated choosing this over a run-level design (see the brainstorming
discussion) is satisfied by *every* task of a `remote`-mode project becoming
an independent job — two independent tasks in one run's dependency graph can
already land on two different runners. Per-task mode mixing within a single pipeline is
real additional value but a distinct, separable piece of scope; deferred (see
proposal.md Non-goals) rather than bundled in.

**Alternative considered:** a per-task HCL attribute selecting the mode.
Rejected for this change on YAGNI grounds — nothing in the stated goal (dogfood
Kestrix's own CI on two runners) needs mixed-mode pipelines yet.

### D3: Job payload names no execution engine

Kestrix sends a task's script, resolved variables, and source information.
Nothing in the payload says "docker" or an image. This keeps the payload
identical regardless of what a given runner is configured to do locally, and
keeps Kestrix out of the business of validating Dockerfile/image fields it
cannot itself execute — the runner's own config file owns that decision
entirely, the way GitLab Runner's `config.toml` does.

### D4: Runner token is bearer-only, scoped to the runner-job route group

Same shape as `observability`'s metrics scrape token: constant-time compare,
hashed at rest, authenticates nothing outside its own route group. No mTLS,
no per-runner client certificates in Phase 1 — bearer-over-TLS matches the
trust model (the operator's own machines) stated in proposal.md's Non-goals.

The trusted-operator threat model doesn't excuse an unbounded request:
`/webhook` already carries its own `maxBody` bound independent of `/api`'s
(`WebhookMaxBodyBytes`); the runner-job group gets the same treatment (a new
`RunnerJobMaxBodyBytes`) plus a per-token cap on concurrent long-poll
connections, so a misbehaving or compromised runner token has a documented
ceiling rather than none (see Requirement: Runner-job request bounds).

### D5: Silent-runner recovery reuses the orphan-recovery shape, not its code

`FailInterruptedRuns` fires once at startup for a specific cause (process
restart). Silent-runner recovery is continuous and keyed on a per-job
timestamp, not a startup sweep — a different trigger, so it is a new,
timeout-driven check (naturally placed alongside the existing run-scheduling
background loop), not a call site reusing that function. The *outcome* shape
(name the interruption reason, do not silently drop the run) is what carries
over. Returning a job to `pending` bumps its `claim_epoch` (D1), so a
subsequent claim by another runner — or a re-claim by the same one — is a
distinct epoch from whatever the original, merely-slow runner still holds.

### D6: Source is acquired server-side and shipped as an archive, not cloned by the runner

Every existing mode already acquires source *before* `Executor.RunTask` runs
(`handleRunTask`'s `loadProjectPipeline` resolves the checkout via
`SourceAcquirer`/`CredentialResolver` first, then hands a prepared `WorkDir` to
the executor) — the executor never holds repository credentials itself. Remote
mode keeps that boundary: Kestrix acquires the source the same way it does
today, packages the resolved checkout into an archive, and ships it inside the
job payload. The runner receives files, never a git URL plus a credential.

**Alternative considered:** the runner clones the repository itself, given a
URL and a short-lived credential. Rejected: it would be the only path in the
whole system where repository credentials leave the Kestrix host, undermining
the `runtime-secrets`/`repository-credentials` model for no benefit at PoC
scale, and it would need its own credential-scoping and revocation story this
change does not otherwise need.

### D7: The runner binary gets its own thin adapter, not the control-plane's `Options`

`Executor.RunTask(ctx, task model.Task, opts Options)` is reused as-is, but
`Options` itself is control-plane machinery (`Variables
domain.VariableResolver`, `RepoPaths`, `SourceSessions`, `SourceAcquirer`,
`CredentialResolver`) that cannot and should not cross the wire — it is
constructed fresh on the runner side from the job payload (unpacked source
archive → `WorkDir`, resolved variables → a payload-backed
`VariableResolver`, everything else left zero). This is a real, if small,
adapter layer, not just "call the existing function", and it is its own
`tasks.md` item (5.1).

It also means the `kestrix runner` subcommand imports `internal/domain`,
`internal/model`, and `internal/runner` — packages today consumed only by the
trusted control-plane binary — into a binary explicitly designed to run on an
operator's separate, less-trusted machine (proposal.md's Non-goals: "a
registered runner is assumed to belong to the same operator"). That's an
accepted trust-boundary shift, not a hidden one: the runner only ever
constructs an `Options` locally from data it already received over its own
authenticated channel, never reads control-plane config, and Non-goals
already excludes the multi-tenant/untrusted-runner case this would matter
for.

That argument is about the *call path*, not the *artifact*, and the
difference is worth stating plainly. Go links everything `package main`
imports regardless of which subcommand runs, and `cmd/kestrix/main.go`
already imports `internal/web` for `serve`, which pulls in `internal/auth`,
`internal/credentials`, `internal/service`, `internal/source`, the Postgres
store and the swagger docs. Because `runner` is a subcommand of that same
`package main` (D-note: this is the single-binary convention decided in
proposal.md Impact), the binary copied to the operator's machine *is* the
full control plane — JWT and master-key handling, credential encryption, the
whole `/api` surface, the database driver — merely never invoked. Accepted
for Phase 1: the runner machine belongs to the same operator and holds no
control-plane secrets to unlock any of it (no master key, no JWT key, no
`DATABASE_URL`), so the extra code is inert weight rather than a usable
escalation path. If the trust model ever widens to runners the operator does
not fully control, the fix is a separate build target for the runner (build
tag or its own `package main`), which reopens the single-binary decision on
that stronger ground rather than on convention.

### D8: The transport is poll-only — Kestrix never connects to a runner

Every exchange is a request the runner initiates: it polls for a job, pushes
trace chunks, and reports completion. Kestrix opens no connection to a runner
and holds no address for one, so a runner needs no inbound port, no public
address and no firewall hole — it works from behind NAT, which is the whole
point of the deployment this change targets (a small VPS control plane, the
operator's own machines executing).

The cost is that there is no channel to push anything *to* a runner: no stop
signal on cancel (see the cancellation requirement), no liveness probe, no
configuration push. Every such need must be expressed as something the runner
picks up on its next poll.

That cost breaks an existing invariant, so it is written down rather than
left implicit: `task-execution-isolation`'s "Cancellation terminates task
processes" requires a run's task to be stopped *before* the run is recorded
`canceled`, and `RunService.Cancel` enforces it today by waiting
`CancelGrace + 5s` and leaving the run `running` if the process cannot be
confirmed dead. Neither half is reachable in `remote` mode — the process is
on another machine, behind NAT, with no inbound channel to signal and no
reliable way to observe its exit. This change therefore carries a MODIFIED
delta on that requirement carving `remote` out, instead of silently
contradicting it. The consequence an operator sees: cancelling a
`remote`-mode run stops Kestrix waiting for it, but a task already running on
a runner runs to completion, and its late result is discarded (see
"Cancelling a remote-mode run").

**Alternative considered:** Kestrix calls back to a registered runner URL
(webhook-style, the way it already receives GitLab webhooks). Rejected: it
inverts the connectivity requirement — each runner would need a reachable
address and an open port, which the NAT-safe goal exists to avoid — and it
would need its own inbound authentication story on the runner side.

### D9: Remote trace output is appended in the database, not overwritten

`FlushRunOutput` is `UPDATE runs SET output = $1 WHERE id = $2` — it replaces
the whole column with the caller's copy of the run's complete output. That is
correct for its one existing caller: the in-process task loop, which is
sequential and owns the only `bytes.Buffer` for the run. Remote mode breaks
both assumptions at once — D2 has two independent tasks of one run landing on
two runners, so two trace-append handlers would each compute "the run's new
full output" and silently clobber each other's.

The trace endpoint therefore writes with a database-side append (`UPDATE runs
SET output = output || $1`), the shape `AppendTaskResult` already uses for
`task_results`. The offset a runner sends still does per-job deduplication
(task 3.3); the append is what makes two *different* jobs of one run safe.
`FlushRunOutput` stays as-is for the in-process path — this is an added
store method, not a change to the existing one.

The append must carry the output bound with it. In-process, the bound is
enforced because the single writer holds the whole buffer:
`prepareRunExecution` wraps it in `newBoundedWriter(output,
s.MaxOutputBytes)` before anything reaches `FlushRunOutput`. A store-side
append has no such writer, so the bound moves into the append itself: a chunk
is appended while the stored output is below `max_output_bytes`; the chunk
that crosses the bound is cut at it and the truncation marker appended once;
later chunks for that run are discarded. Redaction still happens before the
write, so the ordering `run-scheduling`'s "Run output bound" requires
(redact, then truncate) is preserved.

This is deliberately *not* a case like D8's cancellation carve-out: the main-spec requirement is
satisfied in `remote` mode, not carved out, so `run-scheduling` needs no
delta for it. The bound is the same value, with the same marker and the same
"truncation changes no status" guarantee — only the enforcement point moves
from the writer to the store.

**Alternative considered:** serialize trace writes per run behind a lock.
Rejected: it reintroduces a coordination point the database already solves
atomically, and it would not survive more than one Kestrix process.

### D10: A remote-mode run registers no in-process `active`/`done` entry

`RunService` has no notion of `Mode` today — `Start` unconditionally records
`active[runID]`/`done[runID]` and runs the pipeline in a goroutine, and
`Cancel` branches on whether that bookkeeping exists, not on mode. Rather
than teach `Cancel` about modes, `Start` skips the in-process half entirely
for a `remote`-mode project: it creates the run, dispatches a job per task,
and returns, with no goroutine, no `active`/`done` entry and no concurrency
slot (which tasks.md 2.3 already requires).

`Start` is not the only entry point that executes a task, and the other two
need the same gate: `RunTask` (single-task re-run) and `ApproveManualTask`
(manual gate) both reach `runTaskInternal` → `taskRunner.RunTask` with no
mode check at all today. Left alone they would execute a `remote`-mode
project's task inside the Kestrix process, contradicting "Remote project
mode"'s "no task of that run SHALL execute inside the Kestrix process" for
exactly the two paths a spec reader is least likely to check. Both therefore
dispatch a single job instead, reusing the same dispatch path as `Start`
(tasks.md 2.2) with one task rather than the whole dependency graph; a manual task's
approval is what makes its job `pending`, and a re-run enqueues a fresh job
for the one task.

That also keeps the `output` column single-shaped: the in-process path
records through `ApplyTaskResult`, which read-modify-writes `output`
(`out := run.Output; out += outputExtra`, then a whole-column `UPDATE`) and
would clobber a concurrent `AppendJobTrace` on the same run — D9's race
through a second writer. No in-process execution for a remote-mode project
means no such writer, and the remote completion path SHALL NOT introduce one:
it appends per D9 rather than read-modify-writing the column.

The cancel semantics then fall out of code that exists: `Cancel`'s
no-active-entry branch already cancels through `runStore.CancelRun` directly,
"with no termination attempted" — exactly what the `task-execution-isolation`
carve-out specifies. No mode-conditional branch is added to `Cancel` at all.

The one follow-on this forces is real and in scope:
`ActiveRunIDsForProject` reads the in-memory `active` map, and
`handler_project.go` uses it to refuse deleting a project with runs in
flight. A remote run has no entry there, so project deletion would no longer
be blocked by one. It therefore needs a store-backed check for runs with
outstanding (`pending`/`claimed`/`running`) remote jobs, unioned with the
in-memory set.

That reintroduces the store lookup the archived `project-delete-run-guard`
change (D1) deliberately avoided, for a stated reason: "any lookup error
degrades to skipping the run, which fails open into deletion" — which is why
the method takes no error return today and why the handler can claim it
"cannot fail open into a delete". Adding the query means choosing what a
query error does, and this change chooses **fail closed**: the method returns
an error, and the handler refuses the deletion when it cannot determine
whether remote jobs are outstanding. Refusing a delete is a retryable
inconvenience; deleting a project out from under a live remote run cascades
rows that a running job will still try to write. The archived change's
invariant — never fail open into deletion — is preserved; only the failure
mode for "don't know" moves from impossible to explicit.

**Alternative considered:** keep registering the entry and give `Cancel` a
mode-conditional branch that skips the `doneCh` wait. Rejected: it pushes
mode-awareness into a layer that today treats "has an active entry" and "is
executing in this process" as the same fact, and leaves a `done` channel
nobody ever closes.

## Risks / Trade-offs

- **A large repository makes the job payload large** → acceptable at PoC scale
  (dogfooding Kestrix's own repository); if this becomes real friction, a
  follow-up can shift to a fetch-by-reference model without changing the
  credential boundary D6 establishes.
- **The archive multiplies per task, not per run** → per `tasks.md` 1.1,
  `remote_jobs` is one row per task, so a run with N tasks stores N
  near-identical copies of the same source archive across N
  `remote_jobs.payload` columns (Postgres TOAST), not one shared archive —
  a distinct, additional ceiling beyond per-payload size. Acceptable at PoC
  scale (small repository, two runners); the same fetch-by-reference
  follow-up above removes this multiplication too, since a shared reference
  replaces N copies with N pointers to one acquired checkout.

- **Every trace chunk rewrites the whole `output` value** → Postgres has no
  in-place append, so `output = output || $1` writes a new row version of the
  full, growing value: O(current size) per chunk, O(n²) across a run's trace,
  with the dead-tuple and TOAST churn that implies on `runs`. Remote mode
  makes this the most-executed write in the design — many small appends per
  job, against the in-process path's periodic single flush. Bounded in the
  worst case by `max_output_bytes` (D9), and acceptable at PoC scale (one
  project, two runners); the upgrade path if a long streaming build makes it
  hurt is a separate append-only trace table, rows per chunk, assembled on
  read — which also removes the per-run write contention D9 works around.

- **A remote job's resolved secret values cross the network** → TLS is
  mandatory (documented, not enforced by Kestrix itself — same posture as
  today's `/api`), and the redaction guarantee (Requirement: Incremental
  output) keeps a leaked value from being echoed back into stored output even
  if a script prints it.
- **`SELECT FOR UPDATE SKIP LOCKED` needs a real transaction per claim** →
  matches the existing `RunStore` implementation pattern (`StartQueuedRun`
  already does exactly this); no new infrastructure.
- **Phase 1 has no tags** → with exactly two runners and one project running
  Kestrix's own CI, any-available claiming is sufficient; tags become load-
  bearing only once there is a reason to route specific tasks to specific
  machines (Phase 2).
- **Spec describes Phase 2 requirements this change does not build** → each is
  marked `(Phase 2)` in its requirement name specifically so
  `opsx:audit`/`opsx:status` do not credit this change with them; a future
  change implements them against the same delta path rather than writing a
  new one.
- **This change and `ui-single-task-rerun` both modify the same main-spec
  requirement** → see Sequencing note above; no content conflict, only an
  ordering note for whichever archives second.

## Migration Plan

1. A schema migration adds `Mode` to the projects table (D2): today no
   project carries a mode at all, so the migration is what gives each one a
   value. Existing rows default to the instance's current
   `config.RunnerConfig.Mode`, which is exactly the mode they already run
   under — so behaviour after the migration is identical to before it, for
   every existing project.
2. Nothing opts into `remote` by migration. A project moves to `remote` mode
   only when an admin edits its `Mode` afterward; existing runs are not
   touched or rewritten.
3. Rollback is the previous image; `remote_jobs` and `runners` tables are new
   and additive, safe to leave in place unused.
4. Phase 2 lands as its own later change, reusing this spec's already-written
   `(Phase 2)` requirements rather than re-deriving them.

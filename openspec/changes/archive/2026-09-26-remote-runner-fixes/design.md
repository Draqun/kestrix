## Context

Found by running `remote-runner-registration` tasks 6.3-6.5 manually on
2026-09-26 (Kestrix + Postgres + a separate runner container, `secret_ref`
variable created from Settings, runs driven from the UI):

- Run 2 of `manual-remote` stored `token=<secret value>` in `runs.output`
  and returned it from `GET .../runs/2`; `remote_jobs.payload` held the value
  for both jobs.
- `MAX_RECLAIMS=1`: a killed runner's job went `running` → `failed` with no
  reclaim. `MAX_RECLAIMS=2`: it went to `pending` once and was still `pending`
  2.5 minutes later with no runner, its run still `running`.
- The first job failed with `exec: "bash": executable file not found`; the
  run showed `(No output)` and `error: task failed`.

In-process modes get these right: `RunService.prepareRunExecution`
(`internal/service/service.go`) wraps output in a `redactingWriter` built from
`RunnerOptions.ResolvedSecretValues`, records `running` via `OnTaskStart`, and
stores `TaskResult.Error`. Remote dispatch (`internal/service/remote_dispatch.go`)
never carries `ResolvedSecretValues` past the HTTP request that started the
run.

## Architecture

```
run start (HTTP)                      runner
 BuildVarStore → vars, secretValues
   │
   ├─ runs.remote_secrets_enc  = Enc({per-task vars, secretValues})   AAD "run:<id>"
   └─ remote_jobs.secrets_enc  = Enc({vars, secretValues})            AAD "remote_job:<id>"
      remote_jobs.payload      = {task, repos, archive}    (no values)

 GET /api/runner-jobs/next ── claim ──▶ payload + decrypted {variables, secret_values}
                                        │ task_results += {task, running}
                                        ▼
                                  RunOne(stdout = redact.NewWriter(trace, secret_values))
 PATCH .../trace   ◀──────── chunk (already redacted) ──┘
   server: redact(chunk, knownSecrets ∪ job secret values) → runs.output
 PUT .../complete  ◀──────── {status, output, error}
   server: redact(output/error) → task result; secrets_enc = NULL

 reclaim loop (15s): DeleteTerminalRemoteSecrets: terminal jobs/runs → secrets_enc / remote_secrets_enc = NULL
                     ReclaimStaleJobs: stale claimed/running → pending, reclaim_count++
                                       reclaimed & pending past timeout → reclaim_count++
                                       reclaim_count ≥ max → failed
```

## Decisions

### D1. Redact on both ends

The server redacts every trace chunk and completion `output`/`error` against
`knownSecrets()` plus the job's secret values (`RemoteJobSecretValues`,
decrypted per request). That alone misses a value split across two chunks,
because each chunk is redacted on its own. The runner therefore streams
through the same boundary-safe writer in-process mode uses, fed the
`secret_values` from its claim payload, and flushes it before completing. The
runner already receives every value as a variable, so naming which ones are
secret exposes nothing new. The server-side pass stays as the backstop for a
runner that does not redact (an older binary). An older runner still
redacts nothing across chunk boundaries - a known ceiling, documented.

`redactingWriter` moves from `internal/service` to `internal/redact` as an
exported `Writer`, so `cmd/kestrix` can use it without importing the service
layer; `internal/service` keeps a thin alias.

### D2. Secret-bearing values are encrypted at rest and deleted when done

`remote_jobs.payload` keeps `task`, `repos`, `archive`; `variables` and
`secret_values` go into `secrets_enc BYTEA`, encrypted with
`credentials.Encrypt` and the store's existing `masterKeys`, AAD
`remote_job:<id>` - the job's own ID, reserved with `nextval` before the
insert, since run+task repeats when a single task is re-run. The remote run snapshot does the same with `runs.remote_secrets_enc`,
AAD `run:<id>`; the archive stays in `remote_snapshot`. With no master key
configured, writing fails closed (`ErrMasterKeyUnavailable`) - Kestrix
already refuses to start without one.

The reclaim loop, which already runs every 15s, also calls a separate
`DeleteTerminalRemoteSecrets`, which nulls `secrets_enc` for terminal jobs
and `remote_secrets_enc` for terminal runs. One sweep covers every path a
job or run can end by (completion, cancel, reclaim failure, startup
interruption) instead of hooking each, and it stays its own port method so
the guarantee is not hidden inside stale-job recovery.

Plaintext from earlier versions: at startup, before serving, the schema step
strips `variables` from every `remote_jobs.payload` and every
`runs.remote_snapshot`. No legacy-read fallback is needed, because the startup
interrupted-run sweep already fails every run left `running`, so no
pre-upgrade job is ever claimed again.

Rejected: resolving `secret_ref` again at claim time. That would change the
`runtime-secrets` rule that secrets resolve once at run start.

### D3. `reclaim_count`, not `claim_epoch`, counts reclaims

`claim_epoch` stays the fencing token and moves on claim and reclaim.
`reclaim_count INT NOT NULL DEFAULT 0` moves only on reclaim; a stale job
fails when `reclaim_count >= max`. A job returned to `pending` by recovery
(`reclaim_count > 0`) that nobody claims within the timeout gets
`reclaim_count++` too, and fails the same way at the limit. A job that was
never claimed is not touched: waiting for a first runner (or, later, a runner
with matching tags) is legitimate.

With the default `3` and every runner gone, a job fails after four timeouts
(one silent claim plus three unanswered pending periods).

### D4. The failure reason travels with the completion

`PUT .../complete` accepts an optional `error` (redacted, capped at 4 KiB),
stored as the task result's `Error`. The runner sends `TaskResult.Error` from
`RunOne`. An older runner sends none and behaves as today.

### D5. A claim records `running`

`ClaimNextJob` appends `{name, status: running}` to the run's `task_results`
in the same transaction as the claim, mirroring `OnTaskStart`.
`task_results` is an append log in every mode and the UI shows the last entry
per task, so this is enough for the job list and the DAG.

### D6. `bash` in the runtime image

`LocalExecutor` runs `bash -c`, so the runtime stage installs `bash` (about
1 MB on Alpine). This fixes both `kestrix runner --mode local` in a
container and a `local`-mode project run by the server container.

## Security Considerations

- D1 and D2 restore the `runtime-secrets` guarantees for `remote` mode. The
  claim payload still carries secret values to the runner over the
  runner-job route, which is the point of the mode.
- AAD binds each ciphertext to its row's identity, so a ciphertext copied to
  another job or run fails to decrypt.
- The `error` field is attacker-influenced text from a runner: it is
  redacted, bounded, and stored only on that job's task result.

## Risks / Trade-offs

- An older runner binary does not redact across chunk boundaries (D1).
  Operators should upgrade runners with the server; `docs/remote-runners.md`
  says so.
- Decrypting per trace chunk costs one AES-GCM open per request. That is
  negligible next to the existing row lock and write.

## Context

See proposal.md - Why. The constraints that shape this design:

- The credential store is already the right shape. `internal/credentials/normalize.go:33`
  switches on `input.Type` into `domain.Credential`; `internal/store/pgstore_credentials.go:55`
  encrypts the JSON of that struct with `credentialAAD` binding `{Ref, Type,
  Provider, Host}` as associated data; `credentials.credential_type` is a
  free `TEXT` column. A fourth type is a `case`, a struct field, and no
  migration.
- `internal/source/credentials.go:18` resolves any `credential_ref` for Git
  with no type check. Today every type is a Git type, so that was fine.
- `decodeVariableBlock` at `internal/parser/hcl_parser.go:353` accepts every
  attribute by name and never validates combinations. `domain.Variable`
  (`internal/domain/types.go:38`) is the parsed shape and is what
  `runs.pipeline_def` stores as JSON.
- The variable store is built twice: `service.BuildVarStore`
  (`internal/service/service.go:451`) and `(*Server).buildVarStore`
  (`internal/web/handler_helpers.go:16`). The web copy is the one the run
  handlers call (`handler_run.go:48`, `:127`, `:280`). Both read `v.Value`
  or `v.Default` and set `Masked` as metadata that nothing consumes.
- Task processes get variables through `opts.Variables.Get(repo, job, "")`
  in all three executors (`internal/runner/runner.go:180`, `:141`, `:218`).
  Injecting a resolved secret as a normal variable needs no runner change.
- Output flows through one `bytes.Buffer` set as `opts.Stdout` and
  `opts.Stderr` in `prepareRunExecution` (`internal/service/service.go:196`),
  flushed with `FlushRunOutput` on every task completion and written whole by
  `FinishRun`. Each executor also returns a per-task `taskOut` that becomes
  `TaskResult.Output` and is appended by `AppendTaskResult`. Two sinks, both
  reachable from the service layer.
- `scripts/test.sh:67` holds `internal/web` at 100% and `internal/service` at
  its threshold; `scripts/docker-deployment-e2e.sh:296` already greps API
  responses and logs for a test secret value.

## Goals / Non-Goals

**Goals:**

- One store, one master key, one lifecycle API, one Settings section for
  every secret Kestrix holds.
- The plaintext exists in exactly two places: the decrypting process's memory
  and the task's environment. Nothing else - snapshot, results, flushes, logs,
  API - ever holds it.
- A broken reference is a run failure with a name in it, discovered before a
  single task runs.
- No new dependency, no schema change, no new port beyond a field.

**Non-Goals:**

- Redaction that survives encoding (base64, URL-escaping, split across
  writes). The redactor is a byte-string match on the raw value.
- Per-task secret scoping stronger than the existing variable scope.
- Changing what the runner passes from the process environment. That is
  `runner-execution-isolation`.

## Architecture

```
  admin ──▶ POST /api/credentials {type:"secret", value}
                │
                ▼
        credentials.Normalize ── case "secret" ──▶ domain.Credential{Type, Value}
                │
                ▼
        pgstore.CreateCredential ── Encrypt(AAD{ref,type,...}) ──▶ credentials table

  git push ──▶ pipeline.tf: variable "X" { secret_ref = "cred_…" }
                │
                ▼
        parser.decodeVariableBlock ── rejects secret_ref+value/default
                │                     sets Variable{Key, SecretRef, Masked:true}
                ▼
        runs.pipeline_def (reference only)

  POST /api/projects/{n}/run ─┐
  POST …/approve ─────────────┼──▶ service.BuildVarStore(ctx, proj, resolver)
                              │        │  for each SecretRef:
                              │        │    resolver.Resolve ──▶ pgstore.ResolveCredential
                              │        │    type != "secret" ▶ error(key)
                              │        │    revoked/missing  ▶ error(key)
                              │        ▼
                              │     variables.Store  +  []secretValue
                              │        │                    │
                              ▼        ▼                    ▼
                    RunService.Start ── opts.Variables    redactor(values)
                              │                              │
                              ▼                              ▼
                    executor.RunTask ── env X=value      opts.Stdout/Stderr
                              │                          TaskResult.Output
                              ▼                              │
                    FlushRunOutput / AppendTaskResult / FinishRun  ◀── "***"
```

## Decisions

### D1. A `secret` is a credential type, not a new table

`domain.Credential` and `domain.CredentialInput` gain `Value string`.
`Normalize` gains `case TypeSecret:` requiring a non-empty `Value` and
rejecting any Git-only field, so a `secret` row can never carry a username or
key by accident. Everything downstream - encryption, AAD, rotate, revoke,
list, the `admin` route guard, the Settings section - is inherited unchanged.

Alternative considered: a separate `secrets` table and `/api/secrets`
endpoints. Rejected - it duplicates the encryption, key-version, revocation,
and RBAC code for a type that differs from `token` by one field name, and it
gives operators two places to look for "the thing that holds passwords".

Consequence: `credentialAAD` already includes `Type`, so a stored `token`
cannot be decrypted as a `secret` by editing the row. That property is now
load-bearing and gets a test.

### D2. `secret_ref` on the variable, exclusive with literals, implicitly masked

`domain.Variable` gains `SecretRef string`. The parser rejects
`secret_ref` together with `value` or `default` at parse time, where the
other structural errors already live, and sets `Masked = true` when
`secret_ref` is present. The snapshot in `runs.pipeline_def` therefore
carries the reference, and every API that returns a definition carries the
reference, with no redaction step needed.

Alternative considered: `value = "secret:cred_…"` magic-prefix syntax.
Rejected - it makes a string field bimodal, cannot be validated against
combination rules, and would need a redaction step everywhere `value` is
displayed.

Alternative considered: reference by credential display name. Rejected -
names are mutable and not unique; `credential_ref` is the stable key the rest
of the system already uses.

### D3. Resolution happens once, in the variable-store builder, in the service

`service.BuildVarStore` becomes
`BuildVarStore(ctx, proj, resolver) (*variables.Store, []string, error)`:
for each variable with `SecretRef` it calls `resolver.Resolve`, requires
`Type == "secret"`, and sets the decrypted `Value` into the store under the
variable's scope; the returned slice is the list of resolved values for D5.
`(*Server).buildVarStore` is deleted and its three callers use the service
function. A resolution error aborts the request before `RunService.Start` or
`ApproveManualTask` is called, and the run handler records the run as
`failed` with every task `skipped` so the failure is visible in run history
rather than only as a `400`.

Manual approval rebuilds the store (`handler_run.go:280` already does), so an
approved task sees a rotated secret. That is the documented behaviour, not a
side effect: the snapshot pins the pipeline, not the secret.

Alternative considered: resolve lazily inside the executor when a task
starts. Rejected - it puts the credential store behind the runner port, makes
a revoked secret fail mid-run after earlier tasks already ran, and spreads
the "never log this" surface into three executors.

Alternative considered: keep the web copy of `buildVarStore` and add
resolution there. Rejected - the duplication is the bug; the service layer is
where the run store and the credential resolver already meet.

### D4. Type enforcement in both directions, at the resolver boundary

`source.CredentialStoreResolver.Resolve` rejects `Type == "secret"`;
`BuildVarStore` rejects `Type != "secret"`. `ProjectService.CreateValidated`
and `UpdateValidated` check the referenced credential's type when a
`repo_credential_ref` is set, so the mistake is a `400` at registration rather
than a failed acquisition later. The UI selector filters `type === 'secret'`
out of the project credential list.

A single shared resolver with a type check on both sides costs less than two
resolvers and keeps `domain.CredentialResolver` unchanged.

### D5. Redaction is a writer wrapper owned by the service

`RunService` wraps the output `bytes.Buffer` in a redacting writer built from
the resolved values (length ≥ 8, longest first), and applies the same
redactor to `TaskResult.Output` in `OnTaskComplete` before
`AppendTaskResult`. Everything that persists output - `FlushRunOutput`,
`FinishRun`, `AppendTaskResult`, `ApplyTaskResult` - receives text that has
already passed through it.

The redactor buffers up to `maxSecretLen - 1` trailing bytes between writes
so a value split across two `Write` calls is still matched; the tail is
flushed at run end. That is enough for `echo $X` and log lines; it is not
enough for base64 or hex-encoded values, which the non-goals state.

Alternative considered: redact in each executor's `lockedWriter`. Rejected -
three copies, and the executor does not know which variables are secrets
without a new field on `Options`.

Alternative considered: redact on read in the API handlers. Rejected - the
plaintext would then sit in `runs.output` and in every database backup.

The 8-byte floor exists because redacting `""`, `1`, or `ok` would shred
unrelated output. Values under the floor are still injected, still never
logged by Kestrix itself, and documented as not guaranteed redacted.

### D6. Failure shape: name the key, never the material

Errors from `BuildVarStore` are of the form
`variable "REGISTRY_PASSWORD": secret credential is revoked` /
`… not found` / `… is not a secret credential` / `… could not be resolved`.
The last one wraps decrypt and store errors and drops their text from the
message; the underlying error is logged with the reference only. The
credential's display name is not in the message because `viewer` and
`operator` can read run errors and cannot list credentials.

## Security considerations

- Plaintext lifetime: the value is decrypted in `BuildVarStore`, held in the
  variables store and the redactor for the run's duration, and passed to the
  task as one environment entry. It is never written by Kestrix to disk,
  database, or log. The task itself can of course print it; D5 is what stands
  between that and the run history.
- The process environment already leaks to tasks in `local` and
  `gitlab-runner` modes (`runner.go:178`, `:214`). This change adds nothing
  to that surface - a secret variable is one more `KEY=value` beside the
  variables that already flow - and `runner-execution-isolation` is where
  the surface itself shrinks. Secrets must remain in whatever allowlist it
  introduces; that is stated as a requirement so the two changes cannot
  drift apart.
- Role boundary: `admin` creates and rotates secrets; `operator` starts runs
  that use them; `viewer` sees the variable key and `***`. No role below
  `admin` can learn a value through the API. An `operator` who can push to
  the pipeline repository can add a task that prints a secret to output -
  redaction handles the plain case, and an `operator` with push rights is
  already trusted to run arbitrary scripts.
- Dispatch: secret variables are excluded from the GitLab pipeline
  variables map by construction - D3 never adds them to the dispatch path,
  which builds its map independently at `handler_webhook.go:192`. A test
  asserts the exclusion so a future "forward all variables" change trips it.
- AAD binding on type (D1) prevents a row-level type swap from turning a
  Git token into an injectable secret or vice versa.
- Revocation is immediate for new runs and for manual approvals; a running
  task that already received the value keeps it until it exits. That is the
  same window every environment-variable secret has.

## Risks / Trade-offs

- [Redactor misses encoded or split values] → documented non-goal; the
  cross-write buffer covers the common `echo` case; E2E asserts the raw
  value is absent from output, API, and logs.
- [Short secrets under 8 bytes are not redacted] → injected anyway, floor
  documented; the UI shows a warning on create when the value is under 8
  bytes.
- [Redactor over-matches a long secret that happens to appear legitimately]
  → acceptable; `***` in place of a real token in output is the intended
  outcome.
- [Resolving before `RunService.Start` means a failed resolution has no run
  row unless the handler creates one] → the handler creates the run and
  finishes it `failed` with all tasks `skipped` on resolution error, so
  history shows the failure (D3).
- [Rotated secret picked up by a later manual approval surprises someone
  expecting a pinned run] → documented; pinning secrets to a run would
  require storing plaintext or a per-run ciphertext copy, which is worse.
- [Two `buildVarStore` copies diverge again] → the web copy is deleted, not
  deprecated; `staticcheck` unused-code checks keep it gone.

## Migration

No schema change and no configuration change. Existing definitions parse
exactly as before: `secret_ref` is a new optional attribute, and the new
combination validation cannot fail a definition that has no `secret_ref`.
Existing credentials keep their types and remain valid for Git. Rolling back
to a previous binary leaves `secret` rows in `credentials` that the old
`Normalize` cannot re-normalize on rotate; they can be revoked or deleted by
hand, and a definition with `secret_ref` fails parsing on the old binary with
an unknown-attribute error rather than silently running with an empty value.

## Open questions

None that change the specs or tasks. Whether the redaction floor should be
configurable can be decided when someone has a real secret under 8 bytes.

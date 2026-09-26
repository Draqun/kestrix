## 1. Redaction

- [x] 1.1 Move `redactingWriter` from `internal/service/redact.go` to `internal/redact` as an exported `Writer` (`NewWriter`, `Write`, `Flush`) with its tests; `internal/service` uses it through a thin alias, coverage unchanged
- [x] 1.2 `domain.RemoteJobPayload` gains `SecretValues []string` (`json:"secret_values,omitempty"`); remote dispatch fills it from `RunnerOptions.ResolvedSecretValues` for every job it creates (initial run, resumption, single task, approved manual)
- [x] 1.3 Store `RemoteJobSecretValues(jobID)`; `handleRunnerJobTrace` and `handleRunnerJobComplete` redact against `knownSecrets()` plus those values; handler tests with a `secret_ref`-style value in a chunk and in the completion output
- [x] 1.4 `kestrix runner` wraps its trace writer in `redact.NewWriter(trace, payload.SecretValues)` and flushes it before `reportComplete`; unit test with a value split across two writes

## 2. Secrets at rest

- [x] 2.1 Schema: `remote_jobs.secrets_enc BYTEA`, `runs.remote_secrets_enc BYTEA` (idempotent `ADD COLUMN IF NOT EXISTS`)
- [x] 2.2 `CreateRemoteJob` stores `payload` without `variables`/`secret_values` and `secrets_enc = Encrypt({variables, secret_values})`, AAD `remote_job:<id>` (ID reserved before insert); reads (`ClaimNextJob`, `GetRemoteJob`) decrypt and merge; no master key → `ErrMasterKeyUnavailable`
- [x] 2.3 `SetRemoteRunSnapshot`/`GetRemoteRunSnapshot` keep the archive in `remote_snapshot` and move variables (+ the run's secret values) into `remote_secrets_enc`, AAD `run:<id>`; resumption reuses the stored secret values for new jobs
- [x] 2.4 Reclaim loop calls a separate `DeleteTerminalRemoteSecrets` that nulls `secrets_enc` of terminal jobs and `remote_secrets_enc` of terminal runs
- [x] 2.5 Startup schema step strips `variables` from every `remote_jobs.payload` and `runs.remote_snapshot`
- [x] 2.6 Integration tests: no plaintext secret in any column of a pending job or its run; values gone after the run finishes; a ciphertext copied to another job fails to decrypt; legacy plaintext stripped

## 3. Recovery counting

- [x] 3.1 Schema: `remote_jobs.reclaim_count INT NOT NULL DEFAULT 0`
- [x] 3.2 `ReclaimStaleJobs`: stale `claimed`/`running` → fail if `reclaim_count >= max`, else `pending` with `reclaim_count+1`; `pending` with `reclaim_count > 0` and `updated_at` past the timeout → same rule; never-claimed pending untouched
- [x] 3.3 Integration tests: max 1 allows exactly one reclaim; max 2 with no runner fails after the limit and the run becomes terminal; never-claimed pending job left alone; existing reclaim tests updated

## 4. Error reporting and running status

- [x] 4.1 `PUT .../complete` accepts optional `error` (redacted, capped at 4 KiB); `CompleteJob` stores it as the task result's `Error`; handler + store tests
- [x] 4.2 `kestrix runner` sends `TaskResult.Error` from `RunOne`; test for a task whose executor fails before producing output
- [x] 4.3 `ClaimNextJob` appends `{task, running}` to the run's `task_results` in the claim transaction; integration test

## 5. Image and UI

- [x] 5.1 `docker/Dockerfile` runtime stage installs `bash`; `docker run --rm --entrypoint bash <img> -c true` succeeds
- [x] 5.2 Project detail page shows the project's mode (chip next to the name); Vitest
- [x] 5.3 Catch-all route renders a not-found page with a link to the dashboard; Vitest

## 6. Docs and verification

- [x] 6.1 `docs/remote-runners.md`: upgrade runners together with the server (cross-chunk redaction is runner-side), `error` on completion, reclaim counting and the no-runner-left case
- [x] 6.2 `CHANGELOG.md` `[Unreleased]` → `Fixed` entries
- [x] 6.3 `scripts/test-all.sh` (all layers) and `make lint`, `make ui-lint` green; 100% coverage gate holds
- [x] 6.4 Manual re-run of `remote-runner-registration` 6.3-6.5 in a separate-container setup with a `secret_ref` variable: secret shown as `***` in the UI and absent from every DB column, a killed runner's job reclaims and (with no runner) fails at the limit, the runner started from the image runs `--mode local` without extra packages, a failing task shows its error

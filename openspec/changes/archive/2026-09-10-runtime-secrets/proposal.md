## Why

Kestrix can clone a private repository but cannot deploy anything from it:
there is nowhere to keep a registry password, a cloud token, or an API key a
task needs at run time. `docs/repository-credentials.md` defers "runtime or
deployment secrets" to a mechanism that does not exist. The only knob is
`variable { masked = true }` in HCL - plaintext committed to Git and masked
nowhere: `internal/runner/runner.go:180` copies the value into the task
environment and `internal/service/service.go:451` never reads the flag.

## What Changes

- Add a `secret` credential type to the existing encrypted credential store:
  one opaque value plus a display name, managed through the same
  `/api/credentials` endpoints, `admin` role, master key, and Settings section
  as Git credentials.
- Add `secret_ref` to the HCL `variable` block. It is mutually exclusive with
  `value` and `default`, always masked, and keeps `scope`, `repo`, `job`,
  `env`, and `protected` semantics.
- Resolve every `secret_ref` once, when a run or an approved manual task
  starts. A missing, revoked, or non-`secret` reference fails the run before
  any task starts, naming the variable and never the value.
- Redact resolved values from run and task output before it is persisted or
  returned. The stored snapshot, task results, sources, API responses, and
  logs carry the reference only.
- A `secret` credential is refused as a repository credential, and a Git
  credential is refused as a `secret_ref`.

## Non-goals

- External secret managers (Vault, AWS Secrets Manager); `secrets-integration`
  in `openspec/config.yaml` is that follow-up and gets a `secret_ref` to
  hang on.
- Per-environment secret sets or versioning beyond rotate-in-place.
- Passing secrets to a dispatched GitLab pipeline. GitLab's pipeline-variables
  API has no masked flag; dispatch keeps sending only `KESTRIX_*` metadata
  (`internal/web/handler_webhook.go:192`).
- Deciding which process-environment variables reach a task. That is
  `runner-execution-isolation`; this change only requires that resolved
  secrets are pipeline variables and so pass its allowlist.
- Giving `masked = true` on a plaintext `value` new behaviour. It stays a
  no-op and the documentation says so.

## Capabilities

### New Capabilities

- `runtime-secrets`: declaring, resolving, injecting, and redacting run-time
  secrets referenced from a pipeline definition.

### Modified Capabilities

- `repository-credentials`: the store also holds `secret` credentials; Git
  types stay outside task processes, `secret` is the type that reaches them,
  and a `secret` is not a repository credential.
- `pipeline-definition`: the `variable` block is specified for the first time,
  including `secret_ref` and its exclusivity and masking rules.

## Impact

- `internal/credentials/normalize.go`: one more `case`; `domain.Credential`
  and `domain.CredentialInput` gain `Value`.
- `internal/parser/hcl_parser.go:353`: parse and validate `secret_ref`;
  `domain.Variable` gains `SecretRef`.
- `internal/service/service.go:451` and its duplicate at
  `internal/web/handler_helpers.go:16` collapse into one builder that takes a
  `CredentialResolver`; the two run handlers and manual approval call it.
- `internal/service/service.go:196`: the output buffer gains a redacting
  writer, so `FinishRun`, `FlushRunOutput`, and `AppendTaskResult` see
  redacted text only.
- `ui/src/lib/credentialForm.js`: a fourth type; the project credential
  selector hides it.
- No schema change - `credentials.credential_type` already holds the type.
  `scripts/test.sh` keeps `internal/web` and `internal/service` at 100%.

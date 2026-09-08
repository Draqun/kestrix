## 1. Secret credential type

- [ ] 1.1 Add `Value string` to `domain.Credential` and `domain.CredentialInput` in `internal/domain/ports.go` (JSON `value`, omitted when empty), and verify `go build ./...` and `portscheck` pass.
- [ ] 1.2 Add `TypeSecret = "secret"` and its `case` to `credentials.Normalize` in `internal/credentials/normalize.go`: require non-empty `Value`, reject any of `Username`, `Password`, `Token`, `PrivateKey`, `KnownHosts` being set, and verify unit tests cover a valid secret, an empty value, and each rejected extra field.
- [ ] 1.3 Verify an integration test in `internal/store/pgstore_integration_test.go` creates, lists, rotates, and revokes a `secret` credential through the existing `CredentialStore` methods, asserts the row's `encrypted_payload` does not contain the value, and asserts metadata responses have no `value` field.
- [ ] 1.4 Verify a test asserts that a `credentials` row whose `credential_type` is rewritten from `token` to `secret` fails to decrypt (AAD binding), so a type swap cannot repurpose a stored credential.
- [ ] 1.5 Verify a handler test asserts `POST /api/credentials` with `type: "secret"` answers `201` for `admin` and `403` for `operator`, and that an empty value answers `400` with no value echoed.

## 2. `secret_ref` in the pipeline definition

- [ ] 2.1 Add `SecretRef string` (`hcl:"secret_ref,optional" json:"secret_ref,omitempty"`) to `domain.Variable` in `internal/domain/types.go`, and verify the existing parser and model tests still pass.
- [ ] 2.2 Parse `secret_ref` in `decodeVariableBlock` (`internal/parser/hcl_parser.go:353`), set `Masked = true` when present, and reject `secret_ref` combined with `value` or `default` with an error naming the variable and the attributes; verify unit tests cover a valid secret variable, each conflicting combination, and that an existing literal variable parses unchanged.
- [ ] 2.3 Validate scope targets in `decodeVariableBlock`: `scope = "repo"` requires `repo`, `scope = "job"` requires `repo` and `job`; verify tests cover each missing target and that the `ci/pipeline.tf` and every `examples/*/pipeline.tf` still parse.
- [ ] 2.4 Verify a test asserts a parsed definition containing a secret variable, serialized as JSON the way `runs.pipeline_def` stores it, contains the `secret_ref` and no `value`.

## 3. Resolution in the service layer

- [ ] 3.1 Change `service.BuildVarStore` (`internal/service/service.go:451`) to `BuildVarStore(ctx context.Context, proj *model.Project, resolver domain.CredentialResolver) (*variables.Store, []string, error)`: resolve each `SecretRef`, require `Type == credentials.TypeSecret`, set the decrypted value under the variable's scope, and return the resolved values; verify unit tests with a mock resolver cover project/repo/job scope injection, a literal variable untouched, and a definition with no secrets returning an empty slice and nil error.
- [ ] 3.2 Implement the four error shapes from design D6 (`not found`, `revoked`, `not a secret credential`, `could not be resolved`) naming the variable key only, log the underlying error with the reference, and verify tests assert each message and assert none contains the value, the credential name, or the wrapped error text.
- [ ] 3.3 Delete `(*Server).buildVarStore` from `internal/web/handler_helpers.go:16` and call `service.BuildVarStore` from `handleRunProject`, `handleRunTask`, and `handleApproveManualTask` in `internal/web/handler_run.go`, passing `s.credentialResolver()`; verify `go vet` and `staticcheck` report no unused code and the existing run handler tests pass.
- [ ] 3.4 On a resolution error in `handleRunProject`, create the run and finish it `failed` with every task `skipped` and the D6 error as the run error, then answer `400`; verify a handler test asserts the run row, the task statuses, the response, and that the pipeline runner was never invoked.
- [ ] 3.5 On a resolution error in `handleApproveManualTask`, leave the run and task state unchanged and answer `400` with the D6 error; verify a handler test asserts no task result was appended.
- [ ] 3.6 Verify a test asserts that approving a manual task after the secret was rotated injects the rotated value.
- [ ] 3.7 Verify a test asserts the GitLab dispatch variables map built in `handleWebhook` (`internal/web/handler_webhook.go:192`) contains no key of any secret variable when the definition declares one.

## 4. Type isolation between Git and secret credentials

- [ ] 4.1 Reject `Type == "secret"` in `source.CredentialStoreResolver.Resolve` (`internal/source/credentials.go:18`) with an error naming the type mismatch, and verify a unit test asserts the error and that no Git process would be started (the acquirer test with a mock store).
- [ ] 4.2 In `ProjectService.CreateValidated` and `UpdateValidated` (`internal/service/service.go:65`, `:93`), when `RepoCredentialRef` is set, load its metadata and reject type `secret` with a `400`-mapped error; verify service tests cover accept for `token`, reject for `secret`, and unchanged behaviour for an empty ref.
- [ ] 4.3 Verify a handler test asserts `POST /api/projects` with a `repo_credential_ref` naming a `secret` credential answers `400` and does not persist the project.

## 5. Output redaction

- [ ] 5.1 Implement a `redactingWriter` in `internal/service` wrapping an `io.Writer`, replacing each configured value (≥ 8 bytes, longest first) with `***`, buffering up to `maxLen-1` trailing bytes across writes and exposing `Flush()`; verify unit tests cover a value inside one write, a value split across two writes, two overlapping values, a value under 8 bytes left untouched, and empty configured values.
- [ ] 5.2 In `prepareRunExecution` (`internal/service/service.go:196`) wrap the output buffer with the redactor built from the resolved values so `opts.Stdout`/`opts.Stderr`, `FlushRunOutput`, and `FinishRun` see redacted text, and apply the same redactor to `TaskResult.Output` in `OnTaskComplete` before `AppendTaskResult`; verify service tests with a fake pipeline runner that echoes the secret assert `***` in the flushed output, the final output, and the appended task result.
- [ ] 5.3 Apply the redactor on the `RunTask` and `ApproveManualTask` paths (`service.go:293`, `:393`) where `ApplyTaskResult` receives `outputExtra`; verify a test asserts the persisted extra output is redacted.
- [ ] 5.4 Call `Flush()` in `finishRun` before persisting so a value at the very end of output is still redacted; verify a test whose output ends mid-secret asserts redaction.
- [ ] 5.5 Verify a test captures the server log during a run with a revoked secret and a run with a valid secret and asserts neither contains the value.

## 6. Interface

- [ ] 6.1 Add `{ value: 'secret', label: 'Runtime secret' }` to `CREDENTIAL_TYPES` in `ui/src/lib/credentialForm.js` with a single required `value` field (multiline, write-only) and helper text stating it is injected into tasks as a pipeline variable and is not a Git credential; verify the component tests cover selecting the type, required validation, and that switching type clears the value.
- [ ] 6.2 Show an inline warning in `CredentialFormDialog.jsx` when a `secret` value is shorter than 8 characters, stating it will not be redacted from output; verify a component test asserts the warning appears and disappears at the boundary.
- [ ] 6.3 Filter `type === 'secret'` out of `CredentialSelectField.jsx` for the project create/edit dialogs; verify a component test asserts a secret credential is not offered while Git credentials are.
- [ ] 6.4 Verify the Settings credential list renders a `secret` row with its type label and rotate/revoke actions, and that `make ui-test` still meets the documented coverage thresholds.

## 7. Documentation and deployment checks

- [ ] 7.1 Document the `secret` type, `secret_ref`, exclusivity with `value`/`default`, implicit masking, run-start resolution, manual-approval re-resolution, the 8-byte redaction floor, and the "literal `masked = true` is not a secret" note in `docs/pipeline-definition.md` and `docs/repository-credentials.md`; verify `make ui-build` copies the updated docs.
- [ ] 7.2 Add a CHANGELOG entry under Unreleased describing the new credential type, `secret_ref`, redaction, and the new validation that `secret_ref` cannot be combined with a literal.
- [ ] 7.3 Extend `scripts/docker-deployment-e2e.sh` to create a `secret` credential, register a project whose pipeline echoes the variable, run it, and assert the value appears in none of: run output via API, task result output, container logs, and that `***` does; verify with `bash -n` and, where a Docker daemon is available, `make docker-e2e`.
- [ ] 7.4 Add an example under `examples/` (or extend `multi-service`) declaring one secret variable with `scope = "job"` and verify `kestrix validate` accepts it.

## 8. Verification

- [ ] 8.1 Run `make quality` and verify it passes with `internal/web` and `internal/service` at their coverage thresholds.
- [ ] 8.2 Run the integration suite against `kestrix_test` and verify the new credential and project-validation tests pass without any schema change.
- [ ] 8.3 Run `npx -y @fission-ai/openspec@latest validate runtime-secrets --strict` and grep the change directory and tracked files for the E2E secret value to verify no test value leaked into committed files.

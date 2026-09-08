## 1. Configuration knobs

- [ ] 1.1 Model `KESTRIX_HTTP_READ_TIMEOUT` (default `30s`), `KESTRIX_HTTP_WRITE_TIMEOUT` (`120s`), `KESTRIX_HTTP_IDLE_TIMEOUT` (`120s`), `KESTRIX_HTTP_MAX_BODY_BYTES` (`1048576`), and `KESTRIX_WEBHOOK_MAX_BODY_BYTES` (`8388608`) in `internal/config/environment.go`, rejecting non-positive or unparseable values with an error naming the setting, and verify a config test covers each default, an explicit value, `0`, a negative, and garbage.
- [ ] 1.2 Carry the five values into `web.Config` from `cmd/kestrix/main.go`, drop `SignatureConfig.Enabled` and the `Enabled: cfg.Webhook.Secret != ""` line at `main.go:286`, and verify `go build ./...` and `go vet ./...` pass.

## 2. HTTP server limits

- [ ] 2.1 Replace `listenAndServe(addr, router)` in `internal/web/server.go` with an `*http.Server{ReadTimeout, WriteTimeout, IdleTimeout}` built from `web.Config`, keeping a test seam over the server value, and verify a unit test asserts the three fields equal the configured durations.
- [ ] 2.2 Add a `maxBody(n int64)` middleware wrapping `http.MaxBytesReader` and mount it on the `/api` subtree with `MaxBodyBytes` and on `/webhook` with `WebhookMaxBodyBytes` in `buildRouter`, and verify a test asserts a `/api` body one byte over the bound is refused while the same body under the webhook bound reaches `handleWebhook`.
- [ ] 2.3 Add `decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool` to `internal/web/errwrite.go` that answers `400` on a parse error and `413` on `*http.MaxBytesError`, and verify a unit test covers valid JSON, invalid JSON, and an oversized body.
- [ ] 2.4 Route the ten `json.NewDecoder(r.Body).Decode` sites (`handler_auth.go:42`, `handler_credentials.go:39,73`, `handler_pipeline.go:138,195`, `handler_project.go:357`, `handler_run.go:31`, `handler_user.go:90,148,209`) through `decodeJSON`, and verify a table-driven test sends an oversized body to each route and asserts `413` with no business effect (no row created, no run started).
- [ ] 2.5 Make `throttleLogin` (`login_throttle.go:189`) answer `413` on `*http.MaxBytesError` before touching the throttle, and verify a test asserts an oversized login body never increments either throttle counter.

## 3. Webhook verification

- [ ] 3.1 Reduce `internal/web/webhook_verify.go` to constant-time primitives: `verifyGitLabToken(header, secret string) bool` and `verifyHMAC(header string, body []byte, secret string) bool`, delete the middleware constructors and `SignatureConfig.Enabled`, and verify the existing `webhook_verify_test.go` cases are ported and still pass.
- [ ] 3.2 Add `verifyDelivery(method string, h http.Header, body []byte, projectSecret, globalSecret string) bool` that tries the project secret when non-empty, then the global secret when non-empty, and returns false when both are empty, and verify a unit test covers: project match, global match, project set but wrong and global absent, both empty, `hmac` method rejecting a matching `X-Gitlab-Token`, and `gitlab` method rejecting a matching `X-Hub-Signature-256`.
- [ ] 3.3 Add a two-field project-ID probe (`{"project":{"id":N}}`) used before full parsing, and verify a unit test asserts a non-JSON body and a body without an ID both yield `0`.
- [ ] 3.4 Rewrite the top of `handleWebhook` (`handler_webhook.go:55-70`): read the capped body once, answer `413` on `*http.MaxBytesError`, probe the ID, look up the project secret through `FindByGitLabID` when `projectStore != nil` and ID > 0, call `verifyDelivery`, and answer `401 {"error":"unauthorized"}` on failure before `ParseGitLabPushEvent` runs, and verify tests cover a project-secret accept, a global-secret accept, and the no-secret / wrong-secret / unknown-project `401`s.
- [ ] 3.5 Verify a test asserts the `401` status and body are byte-identical across unknown project, wrong secret, and no secret configured, and that none of the three writes a webhook history row.
- [ ] 3.6 Verify a test asserts a delivery that fails verification with an invalid push payload answers `401`, not `400`, and that an unregistered project verified by the global secret still answers `200 ignored` and records the event.
- [ ] 3.7 Verify a test asserts a store error during the project-secret lookup answers `503` and records nothing, consistent with `rbac-hardening` fail-closed behaviour.

## 4. Write-only project webhook secret

- [ ] 4.1 Add `HasWebhookSecret bool json:"has_webhook_secret"` to `domain.ProjectConfig` in `internal/domain/ports.go` and a `publicProject(cfg) domain.ProjectConfig` helper in `internal/web` that blanks `WebhookSecret` and sets the flag, and verify a unit test asserts the copy carries the flag and no secret.
- [ ] 4.2 Apply `publicProject` at the three encode sites in `handler_project.go` (`:118`, `:140`, `:317`) and in the list handler's items, and verify a test creates a project with a secret and asserts neither the create response, the list, nor the detail body contains the string `webhook_secret` or the secret value.
- [ ] 4.3 Make `ProjectService.UpdateValidated` (`internal/service/service.go:93`) copy the stored `WebhookSecret` onto `cfg` when the request's is empty, and verify a service test covers omit-keeps, empty-keeps, and non-empty-replaces.
- [ ] 4.4 Verify an integration-level test: create with secret, update without it, deliver a webhook with the original secret, assert `200`.

## 5. `repo_url` restrictions

- [ ] 5.1 Extend `validateProject` in `internal/service/service.go` with the scheme allow-list (`https`, `ssh`, scp-like `[user@]host:path`) and the literal-host deny-list (loopback, link-local, unspecified) using `net/netip`, and verify a table-driven test covers the three accepted forms, `http://`, `file://`, `git://`, `ext::`, `localhost`, `127.0.0.1`, `[::1]`, `169.254.169.254`, `[fe80::1]`, and an allowed `10.0.0.5`.
- [ ] 5.2 Verify a handler test asserts `POST /api/projects` and `PUT /api/projects/{name}` answer `400` for a rejected URL and that the validation function was reached without `validateSource` (the network step) being called.

## 6. Deployment and documentation

- [ ] 6.1 Add the five `KESTRIX_HTTP_*` / `KESTRIX_WEBHOOK_MAX_BODY_BYTES` entries to `.env.template` and both Compose files, remove the `:-dev-secret` default from `docker/docker-compose.yml:32` and `docker-compose.prod.yml:21` with a comment stating deliveries are refused until a secret is set, and verify `make docker-up` still starts.
- [ ] 6.2 Update `docs/webhook-receiver.md` (Authentication section: fail-closed rule, per-project first, uniform `401`, `413`), `docs/authentication.md` cross-reference, and `docs/usage.md`, and verify `make ui-build` copies them into `ui/dist/docs`.
- [ ] 6.3 Add a CHANGELOG entry marking fail-closed verification and the removed `dev-secret` default as BREAKING, listing the new environment variables, `has_webhook_secret`, and the `repo_url` restrictions.
- [ ] 6.4 Extend `scripts/docker-deployment-e2e.sh` with a delivery lacking any secret asserting `401` and a delivery with the per-project secret asserting `200`, and verify it with `bash -n` plus one full run.

## 7. Verification

- [ ] 7.1 Run `make quality` and verify `internal/web` and `internal/service` hold their 100% thresholds.
- [ ] 7.2 Run `make test-integration` and verify the project store round-trips `webhook_secret` unchanged and the schema needs no migration.
- [ ] 7.3 Run `npx -y @fission-ai/openspec@latest validate webhook-ingress-hardening --strict` and verify it passes with no secret material in tracked files.

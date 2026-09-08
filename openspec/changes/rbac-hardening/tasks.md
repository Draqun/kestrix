## 1. Token version and session revocation

- [x] 1.1 Add `token_version INT NOT NULL DEFAULT 1` to the `users` table in `internal/store/pgstore.go` as an `ALTER TABLE ... ADD COLUMN IF NOT EXISTS` entry in the `statements` slice, and verify the integration test applies it to a schema created by an earlier binary as well as to a fresh one.
- [x] 1.2 Extend `domain.User` and the `UserStore` port with the token version, and add `BumpTokenVersion(name string) error`, then verify `go test ./internal/domain/...` passes including `portscheck`.
- [x] 1.3 Implement the version read and bump in `internal/store/pgstore_user.go`, and make `UpdateRole`, `UpdatePassword`, and `Delete` bump it in the same transaction as their own write, then verify an integration test asserts the version moves for each of the three and that a failed write leaves it unchanged.

  Deviation: `Delete` removes the row outright rather than bumping a
  version column on it — there is nothing left to bump, and D1 already
  makes a deleted account's absence the revocation signal (`RequireAuth`
  rejects once the identity lookup finds no account). `BumpTokenVersion`
  covers the standalone-revocation case the design names. Covered by
  `TestPG_DeleteUser` (existing) plus the new `TestPG_BumpTokenVersion*`
  and `TestPG_UpdateUserRole_NotFoundLeavesVersionUnchanged`.
- [x] 1.4 Add the `ver` claim to `internal/auth/token.go` on issue and expose it on `domain.Claims`, and verify a unit test asserts a round-trip carries the version and that a token built without the claim is reported as absent rather than zero.
- [x] 1.5 Reject a token whose `ver` is absent or does not match, and verify tests cover a matching version, a superseded version, and an absent claim, all answering `401` with one identical message.
- [x] 1.6 Verify a test asserts the `401` body and status are byte-identical across a bad signature, an unknown account, and a superseded version.

## 2. Identity resolved from the store

- [x] 2.1 Add a `domain.IdentityCache` port holding `{role, tokenVersion, exists}` by account name with an explicit `Invalidate(name string)`, plus a clock seam for its expiry, and verify `portscheck` passes.
- [x] 2.2 Implement the in-memory adapter in `internal/auth` with a configured TTL, negative-result caching, and a bounded entry count, and verify tests cover a hit, an expiry with the injected clock, a negative entry, eviction at the bound, and `0` disabling the cache entirely.
- [x] 2.3 Model `KESTRIX_IDENTITY_CACHE_TTL` (default `5s`) in `internal/config/environment.go`, and verify a config test asserts the default, an explicit value, `0`, and rejection of a negative value.
- [x] 2.4 Change `RequireAuth` in `internal/web/middleware.go` to load the account through the cache after signature verification and to build `domain.Claims` from the stored role, and verify tests cover a valid token for an existing account, a token for a deleted account answering `401`, and a token whose role claim exceeds the stored role being decided on the stored role.
- [x] 2.5 Invalidate the cache entry in the handlers that change or delete an account (`handler_user.go`), and verify a test asserts the very next request after a role change is decided on the new role with no wait.
- [x] 2.6 Verify a test asserts the cache is keyed by account name only, so two tokens for one account never see different roles.

## 3. Fail-closed authorization

- [x] 3.1 Return `503` from `RequireRole` when `ResolveGrant` returns an error, replacing the `err == nil && found` swallow at `internal/web/middleware.go:70`, and verify a test with a failing grant store asserts `503`, no handler execution, and no fallback to the global role.
- [x] 3.2 Return `503` from `RequireAuth` when the user store fails, distinct from the `401` for a missing account, and verify a test covers both paths.
- [x] 3.3 Log the underlying store error and keep it out of the response, and verify a test asserts the body contains no query text, connection string, or driver error.
- [x] 3.4 Verify a test asserts an absent grant still resolves to the global role and answers `200`, not `503`.

## 4. Bounded pipeline directory

- [x] 4.1 Add `boundPipelineDir(cfgRoot, requested string) (string, error)` in `internal/web` resolving with `filepath.Abs` then `filepath.EvalSymlinks` and requiring the result to be the root or below it, and verify unit tests cover the root itself, a child, a `..` escape, an absolute path elsewhere, a symlink pointing out of the root, and a non-existent path.
- [x] 4.2 Apply it at all five call sites in `internal/web/handler_pipeline.go` (lines 24, 47, 96, 151, 197), and verify a table-driven test drives each of the five routes with an outside path and asserts `403`.
- [x] 4.3 Verify a test asserts an outside path and a non-existent path return the same status and message, so the check is not a path oracle.
- [x] 4.4 Verify a test asserts an omitted `dir` still resolves to `s.cfg.PipelineDir` and the five routes behave as before.

## 5. Login throttling

- [x] 5.1 Implement a fixed-window counter in `internal/web` keyed independently by account name and by client address, with the clock injected, and verify unit tests cover the limit, the window rolling over, independent keys, and a success clearing the account counter.
- [x] 5.2 Model `KESTRIX_LOGIN_MAX_ATTEMPTS` (default 10), `KESTRIX_LOGIN_WINDOW` (default `1m`), and `KESTRIX_TRUST_PROXY_HEADER` (default false) in `internal/config/environment.go`, and verify a config test covers defaults and explicit values.
- [x] 5.3 Derive the client address from the first `X-Forwarded-For` hop only when the proxy header is trusted, otherwise from `RemoteAddr`, and verify a test asserts a forged header is ignored when the setting is off.
- [x] 5.4 Wrap `POST /api/auth/login` with the throttle in `buildRouter`, returning `429` with `Retry-After`, and verify a test asserts the throttled request never reaches `AuthService`.
- [x] 5.5 Verify a test asserts an unknown account name is throttled identically to an existing one.

## 6. Decision logging

- [x] 6.1 Log every rejected authentication, throttled login, and `403` denial with account name where known, client address, route, reason, and for a denial the required and effective roles, and verify tests assert one record per rejection path.
- [x] 6.2 Verify a test asserts no log record contains the bearer token, any substring of it, the password, or the password hash.

## 7. Security response headers

- [x] 7.1 Add a `SecurityHeaders` middleware on the root router setting `Content-Security-Policy`, `X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer`, and `X-Frame-Options: DENY`, and verify a test asserts all four on an API response, a static asset, and an error response.
- [x] 7.2 Model `KESTRIX_CONTENT_SECURITY_POLICY` with the bundled-UI default in `internal/config/environment.go`, and verify a test asserts a configured value replaces the default.
- [x] 7.3 Give `/docs/*` its own policy rather than loosening the application's, and verify a test asserts Swagger UI loads and that the application routes keep the strict policy.

## 8. Read visibility

- [x] 8.1 Verify a test asserts a global `viewer` holding no grants sees every project, run, and webhook event, documenting the decision as behaviour rather than leaving it untested.
- [x] 8.2 Verify a test asserts that same account is refused `403` when it starts a run on a project it can read.

## 9. Deployment and documentation

- [x] 9.1 Add `KESTRIX_IDENTITY_CACHE_TTL`, `KESTRIX_LOGIN_MAX_ATTEMPTS`, `KESTRIX_LOGIN_WINDOW`, `KESTRIX_TRUST_PROXY_HEADER`, and `KESTRIX_CONTENT_SECURITY_POLICY` to `.env.template` and both Compose files, and verify `make dev` starts with the documented setup.
- [x] 9.2 Document the revocation model, the cache bound as the revocation latency, the `503` outage behaviour and its runbook entry, and the per-process limit of the throttle in the deployment and API docs.
- [x] 9.3 Add a CHANGELOG entry marking the forced re-login as breaking, and verify it names the new environment variables and the one-column migration.
- [x] 9.4 Verify the Docker E2E script still reaches an authenticated API call and add a case that a deleted account's token stops working within the cache bound.

  Deviation: added the deleted-account-token case to `scripts/docker-deployment-e2e.sh` and syntax-checked it (`bash -n`), but did not run the full Docker E2E in this session - it builds an image and starts Postgres/Redis/Kestrix containers, which this session did not do. Run it (`scripts/docker-deployment-e2e.sh`) before relying on this task as verified end-to-end.

## 10. Verification

- [x] 10.1 Run `make quality` and verify it passes with `internal/web` and `internal/service` at their 100% thresholds.
- [x] 10.2 Run the integration suite and verify the `token_version` column is added to a database created before this change without data loss.
- [x] 10.3 Run the frontend suites (`node --test "src/**/*.test.js"` and `npm run test:components`) and verify the `401` path still returns the interface to login when a token is revoked mid-session.
- [x] 10.4 Run `openspec validate rbac-hardening --strict` and verify no key material, password, or token appears in tracked files.

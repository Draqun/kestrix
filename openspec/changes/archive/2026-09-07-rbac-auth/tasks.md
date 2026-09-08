## 1. Identity ports and password hashing

- [x] 1.1 Add `golang.org/x/crypto` and `github.com/golang-jwt/jwt/v5` to `go.mod` and verify `go build ./...` and `go mod tidy` leave the module clean.
- [x] 1.2 Add `User`, `Role`, and `ProjectGrant` types to `internal/domain/types.go` with role ordering, and verify a unit test covers `viewer < operator < admin` comparison including an unknown role.
- [x] 1.3 Add `UserStore`, `GrantStore`, `PasswordHasher`, and `TokenIssuer` ports to `internal/domain/ports.go` and verify `go test ./internal/domain/...` passes, including `portscheck`.
- [x] 1.4 Implement argon2id hashing in `internal/auth` with per-account salt and parameters encoded in the hash, and verify tests cover equal passwords producing different hashes and a hash carrying non-default parameters still verifying.
- [x] 1.5 Implement constant-time verification plus a dummy-hash path for unknown accounts, and verify a test asserts verification runs for a missing account.

## 2. Token issuing and the signing key

- [x] 2.1 Implement the file-backed signing key provider in `internal/auth`, mirroring `credentials.FileMasterKeyProvider`, and verify tests cover valid raw and hex keys plus missing, permissive, unreadable, and malformed files.
- [x] 2.2 Model `KESTRIX_JWT_SIGNING_KEY_PATH` in `internal/config/environment.go` next to `KESTRIX_MASTER_KEY_PATH`, and verify a config test asserts the unset and empty cases are rejected.
- [x] 2.3 Preflight the signing key in `initStores`/server startup before the listener opens, and verify a `server_startup_test.go` case fails startup with an unusable key file and leaks no key bytes into the error.
- [x] 2.4 Implement HMAC-SHA256 token issuing and verification with expiry and a 60-minute lifetime, and verify tests cover a valid token, an expired token, a token signed with another key, and an `alg` substitution attempt.

## 3. PostgreSQL adapters

- [x] 3.1 Add `users` and `project_members` to the `CREATE TABLE IF NOT EXISTS` block in `internal/store/pgstore.go`, with a unique account name and a foreign key from grants to projects that removes grants with their project, and verify the integration test creates a fresh schema without error.
- [x] 3.2 Implement `pgstore_user.go` (create, get by name, list, update role, update password, delete) and verify integration tests cover the duplicate-name rejection and that no query returns the password hash outside the store.
- [x] 3.3 Implement `pgstore_grant.go` (set grants for an account, list grants, resolve one project's grant) and verify an integration test asserts grants disappear when their project is deleted.

## 4. Authentication and authorization middleware

- [x] 4.1 Implement `AuthService` in `internal/service` composing the user store, hasher, and issuer, and verify unit tests cover a successful login and that wrong-password and unknown-account both return one identical error.
- [x] 4.2 Implement `RequireAuth` middleware in `internal/web` putting the verified claims in the request context, and verify tests cover a missing header, a non-bearer header, an invalid token, an expired token, and a valid token reaching the handler.
- [x] 4.3 Implement `RequireRole` middleware resolving the effective role as the higher of global role and project grant, and verify tests cover a grant raising a viewer on one project, another project refusing, and a grant failing to demote.
- [x] 4.4 Restructure the router in `internal/web/server.go` so `/api/health` and `/api/auth/login` sit on a public subrouter and every other `/api` route sits inside the protected group, and verify a table-driven test walks all registered routes and asserts each protected one answers `401` without a token.
- [x] 4.5 Assign the minimum role to each route group — `viewer` for reads, `operator` for runs and pipeline writes, `admin` for credentials, project lifecycle, and accounts — and verify a test asserts `403` for each group's role boundary and that `403` and `401` stay distinct.
- [x] 4.6 Verify a test asserts a role supplied in a header, query parameter, or body is ignored in favour of the token's role.

## 5. Auth and account endpoints

- [x] 5.1 Implement `handler_auth.go` with `POST /api/auth/login` returning the token and its expiry, and verify tests assert the response carries no password hash and that both failure modes return the same body.
- [x] 5.2 Implement `handler_user.go` with admin-only list, create, change-role, set-grants, and delete, and verify tests cover a `viewer` and an `operator` being refused with `403`.
- [x] 5.3 Add Swagger annotations for the new endpoints and verify `make docs` (or the existing swag target) regenerates without error.
- [x] 5.4 Verify `POST /webhook` still succeeds with only its shared secret and no user token, and still fails with a wrong secret.

## 6. Command-line account management

- [x] 6.1 Add a `user` command to `cmd/kestrix/main.go` with `create`, `list`, `passwd`, and `delete` subcommands operating directly on `DATABASE_URL`, and verify a CLI test creates an account against a test database and lists it.
- [x] 6.2 Read passwords from a terminal prompt or standard input, never from a flag, and verify a test asserts a password-bearing flag does not exist and that stdin input creates the account.
- [x] 6.3 Verify an end-to-end test creates the first administrator with the CLI against an empty database and then logs in through `POST /api/auth/login`.

## 7. User interface

- [x] 7.1 Add `AuthContext` in `ui/src/context/` holding the token in memory and `sessionStorage`, and verify component tests cover restoring a token on reload and dropping it on logout.
- [x] 7.2 Attach the bearer token to every request in `ui/src/api/client.js` from one place, and verify a test asserts each exported call sends the header.
- [x] 7.3 Add a login page and route guard in `App.jsx` that shows the login view when no valid token is held, and verify a component test asserts no project or run data renders while unauthenticated.
- [x] 7.4 Clear the token and route to login when any API call reports `401`, and verify a component test drives a `401` and asserts the redirect.
- [x] 7.5 Hide run, edit, and delete actions from a `viewer` across Dashboard, Projects, Pipelines, and WebhookLogs (Dashboard and WebhookLogs have no mutating actions to hide - verified by inspection), and verify component tests assert the actions are absent for `viewer` and present for `operator`.

## 8. Deployment and documentation

- [x] 8.1 Add `KESTRIX_JWT_SIGNING_KEY_PATH` to `.env.template` and both Compose files with a read-only key-file mount, and verify `make dev` starts with the documented setup.
- [x] 8.2 Add the `kestrix user create` bootstrap step to `scripts/docker-deployment-e2e.sh` and verify the Docker E2E run reaches an authenticated API call.
- [x] 8.3 Document login, roles, grants, signing-key provisioning, and lockout recovery in the deployment and API docs, and verify the docs name the CLI as the only way to create the first administrator.
- [x] 8.4 Add a CHANGELOG entry marking the API-authentication requirement as breaking, and verify it names both new environment and bootstrap steps.

## 9. Verification

- [x] 9.1 Run `make quality` and verify it passes with `internal/web` and `internal/service` at their 100% thresholds.
- [x] 9.2 Run the integration suite and Docker E2E and verify both pass with authentication enabled.
- [x] 9.3 Run the frontend suites (`node --test "src/**/*.test.js"` and `npm run test:components`) and verify both pass.
- [x] 9.4 Run `openspec validate rbac-auth --strict --no-interactive` and verify no signing key or password material is present in tracked files.

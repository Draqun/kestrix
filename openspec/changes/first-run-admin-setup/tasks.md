## 1. Store

- [ ] 1.1 Add `CreateFirstUser(name string, role domain.Role, passwordHash string) (domain.User, error)` and `ErrSetupDone` to `domain.UserStore` / `internal/store/pgstore_user.go`, implemented in one transaction that locks `users`, counts, and inserts only when the count is zero; verify with an integration test that creates on an empty table, returns `ErrSetupDone` when a row exists, and a goroutine race test where exactly one of N concurrent calls succeeds.
- [ ] 1.2 Update every `UserStore` fake/mock in `internal/mocks` and test files so `make test` compiles and stays green.

## 2. API

- [ ] 2.1 Add `handler_setup.go` with `handleSetupStatus` (`GET /api/setup`) and `handleSetup` (`POST /api/setup`) reusing `requireUserStore`, `passwordHasher`, `decodeJSON`, `invalidateIdentity` and the address-keyed login throttle; register both in the open route group in `server.go` next to `/auth/login`; verify unit tests cover `200 {required:true|false}`, `201`, `400` (missing fields), `409` (`ErrSetupDone`), `429` (throttled), and `503` (no user store / no hasher).
- [ ] 2.2 Regenerate Swagger (`make swagger`) with the two routes and verify `docs/swagger.yaml` contains them and `go vet ./...` passes.
- [ ] 2.3 Extend the auth middleware test that enumerates open routes so `/api/setup` is asserted open for both methods and every other route still returns `401`.
- [ ] 2.4 Run `make test-coverage` and confirm 100% on gated packages (`internal/web`, `internal/store`, `internal/service`).

## 3. UI

- [ ] 3.1 Add `setupStatus()` and `completeSetup(name, password)` to `ui/src/api/client.js`; verify a client test asserts method, URL and body.
- [ ] 3.2 Extend `AuthContext` with `setupRequired` resolved on mount alongside `/auth/me`, and expose `completeSetup` that calls the API then `login`; verify a context test covers `required:true`, `required:false`, and the `409` fallback to the login form.
- [ ] 3.3 Add `ui/src/pages/Setup.jsx` (name, password, confirm password, submit) and route it in `App.jsx` before `Login` while `setupRequired`; verify component tests for successful submit (lands on dashboard), validation error, and `409` message.
- [ ] 3.4 Run `make ui-test` and confirm coverage stays above the configured thresholds.

## 4. Docs

- [ ] 4.1 Update `README.md` Quick Start step 2 and `docker/docker-compose.prod.yml` header comment: UI setup first, `kestrix user create` as the scripted alternative; update `docs/authentication.md` with the two routes and the "claim it immediately" warning; add a `CHANGELOG.md` entry under Unreleased/Added.

## 5. Verification

- [ ] 5.1 Manual smoke on a fresh DB: `bin/kestrix serve --static ui/dist`, open the UI, see the setup form, create the admin, land on the dashboard; then `curl -X POST /api/setup` returns `409` and `GET /api/setup` returns `required:false`.

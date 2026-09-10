## 1. Configuration

- [x] 1.1 Model `KESTRIX_COOKIE_SECURE` (default `true`) in `internal/config/environment.go` next to `TrustProxyHeader`, rejecting a non-boolean value, and verify a config test covers the default, `false`, and a malformed value.
- [x] 1.2 Add `KESTRIX_COOKIE_SECURE` to `.env.template` with a comment naming it a plain-HTTP-development downgrade, and to both Compose files, and verify `make dev` starts with the documented setup.

## 2. Cookie on login and logout

- [x] 2.1 Add a `sessionCookie(token string, expiresAt time.Time) *http.Cookie` helper in `internal/web/handler_auth.go` producing `kestrix_session` with `HttpOnly`, `SameSite=Strict`, `Path=/api`, `Secure` from config, and `Max-Age` derived from `expiresAt`, and verify a unit test asserts every attribute and that `Max-Age` tracks a changed expiry.
- [x] 2.2 Set that cookie in `handleLogin` after `AuthService.Login` succeeds and keep the JSON body unchanged, and verify tests assert the cookie on a `200`, no `Set-Cookie` on a `401`, and a byte-identical body to today.
- [x] 2.3 Add `handleLogout` answering `204` with a clearing `Set-Cookie` (`Max-Age=0`, same `Path`) regardless of credentials, register it on the public group in `internal/web/server.go`, and verify tests cover a call with a valid cookie, with no credentials, and that `token_version` is unchanged afterwards.
- [x] 2.4 Add `handleMe` behind `RequireAuth` at `GET /api/auth/me` returning `{name, role, expires_at}` from the context claims, and verify tests assert the stored role is returned (not the token's), the token is absent from the body, and `401` without credentials.
- [x] 2.5 Add Swagger annotations for logout and me and regenerate via `make swagger`, and verify `/docs/swagger.json` lists both routes.

## 3. Cookie credential in RequireAuth

- [x] 3.1 Extract a `credentialFromRequest(r) (token string, fromCookie bool, err error)` in `internal/web/middleware.go` that returns the bearer header when present, `401` on a present-but-malformed header without falling back, and the cookie value otherwise, and verify a table-driven test covers header only, cookie only, both (header wins), malformed header plus valid cookie (`401`), and neither (`401`).
- [x] 3.2 Add the CSRF check in `RequireAuth`: when `fromCookie` and the method is not `GET`/`HEAD`/`OPTIONS` and `X-Kestrix-Request` is empty, answer `403` before `tokenIssuer.Verify`, and verify tests cover cookie `POST` without header (`403`, no store call), cookie `POST` with header (`200`), cookie `GET` without header (`200`), and bearer `POST` without header (`200`).
- [x] 3.3 Verify a test asserts a revoked, expired, and badly signed cookie token each answer `401` with a body byte-identical to the header path, and that `logAuthFailure` output contains no cookie value.
- [x] 3.4 Run `make test-coverage` and verify `internal/web` remains at 100%.

## 4. Interface

- [x] 4.1 Remove `readStoredToken`/`storeToken`/`decodeClaims` and the `STORAGE_KEY` from `ui/src/context/AuthContext.jsx`; on mount call a new `client.me()` and set `{name, role}` from it, treating a `401` as signed out, and verify `AuthContext.component.test.jsx` covers restore on load, `401` on load, and login setting claims from the login response's name plus a follow-up `me`.
- [x] 4.2 In `ui/src/api/client.js` drop `authToken`/`setAuthToken`, send `X-Kestrix-Request: 1` from `apiFetch` on every call, add `me()` and `logout()`, and verify `client.test.js` asserts the header on a `GET` and a `POST`, no `Authorization` header anywhere, and that `logout` posts to `/api/auth/logout`.
- [x] 4.3 Make the sign-out action call `client.logout()` before clearing context state, and verify `Layout.component.test.jsx` (or the component holding sign-out) asserts the call and the return to the login view.
- [x] 4.4 Verify `grep -rn sessionStorage ui/src` returns only the color-mode context, and that `npm run lint` and `npm run build` pass.
- [x] 4.5 Add the login-page hint for a `200` login followed by a failing `me` (the `Secure`-cookie-over-HTTP case, design D4 risk), and verify `Login.component.test.jsx` covers it.
- [x] 4.6 Run `make ui-test` and verify the credential, Settings, navigation, and project-form thresholds still hold.

## 5. Documentation

- [x] 5.1 Update `docs/authentication.md`: cookie attributes, header precedence, the `X-Kestrix-Request` rule, logout and me, `KESTRIX_COOKIE_SECURE`, and that Swagger UI stays bearer-only, and verify the `sessionStorage` trade-off paragraph is replaced rather than left alongside.
- [x] 5.2 Add a CHANGELOG entry under Unreleased naming the one forced browser re-login, the two new routes, and the new variable, and verify it states bearer clients are unaffected.
- [x] 5.3 Verify `scripts/docker-deployment-e2e.sh` passes unchanged (bearer path), and add one `curl -c/-b` case that a cookie `POST` without `X-Kestrix-Request` answers `403` and with it answers as the bearer path does.

## 6. Verification

- [x] 6.1 Run `make quality` and verify it passes with `internal/web` and `internal/service` at their thresholds.
- [x] 6.2 Start `make ui-serve DIR=.`, sign in, reload, confirm no `kestrix-auth-token` in DevTools storage and `kestrix_session` marked `HttpOnly`, sign out, and verify the login view returns and the cookie is gone.
- [x] 6.3 Run `npx -y @fission-ai/openspec@latest validate session-cookie-auth --strict` and verify it passes.

## Why

`rbac-auth` design D8 put the session token in the browser's `sessionStorage`,
and `rbac-hardening` added a Content-Security-Policy in front of it while
stating that the headers "narrow, but do not remove" the exposure
(`docs/authentication.md`). Any script that runs in the page — a future
dependency, a compromised CDN-free bundle, a browser extension — can still read
`kestrix-auth-token` and use the account for its remaining lifetime. An
`HttpOnly` cookie closes that path: script cannot read what it never receives.

## What Changes

- `POST /api/auth/login` keeps returning the token in the body for bearer
  clients, and additionally sets a `kestrix_session` cookie: `HttpOnly`,
  `Secure`, `SameSite=Strict`, `Path=/api`, `Max-Age` equal to the token TTL.
- Add `POST /api/auth/logout`, which clears the cookie, and
  `GET /api/auth/me`, which returns the signed-in account's name, role, and
  expiry so the interface can restore a session it can no longer decode.
- Authentication accepts the cookie as an alternative to
  `Authorization: Bearer`. The header wins when both are present.
- A cookie-authenticated request with a state-changing method must carry the
  custom header `X-Kestrix-Request`; without it the request is refused with
  `403`. Bearer-authenticated requests are exempt.
- Add `KESTRIX_COOKIE_SECURE` (default `true`); `false` is for plain-HTTP
  development only.
- The interface stops storing the token anywhere. It sends the custom header
  on every call, restores a session via `/api/auth/me` on load, and calls
  logout on sign-out.
- Swagger UI at `/docs/*` keeps using bearer tokens only.

## Non-goals

- Changing the token format, TTL, `token_version` revocation, or the identity
  cache. A cookie carries the same token a bearer client receives.
- Refresh tokens, "remember me", or sliding expiry.
- Cross-origin deployments of the interface. The bundled UI and the Vite dev
  proxy are same-origin; a UI served from another origin keeps using bearer.
- A server-side session table. Revocation already works through
  `token_version`; a cookie is only a transport.
- Cookie-based authentication for `/webhook` or `/docs/*`.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `user-authentication`: login also establishes a cookie session; new logout
  and current-session endpoints; state-changing cookie requests must prove
  same-origin intent.
- `access-control`: authentication accepts a cookie as well as a bearer header;
  the interface holds no token and relies on the cookie.

## Impact

- `internal/web/middleware.go` (`RequireAuth`): credential extraction from
  header or cookie, and the CSRF header check on cookie credentials.
- `internal/web/handler_auth.go`: `Set-Cookie` on login, new logout and me
  handlers. `internal/web/server.go`: two new routes.
- `internal/config/environment.go`: `KESTRIX_COOKIE_SECURE`.
- `ui/src/context/AuthContext.jsx` and `ui/src/api/client.js`: no token
  storage, custom header, session restore.
- `docs/authentication.md`, `.env.template`, both Compose files, CHANGELOG.
- Every signed-in browser session logs in again once. CLI, scripts, the Docker
  E2E test, and the Terraform provider
  (`https://gitlab.com/kestrix/terraform-provider-kestrix`) are unaffected.
- `scripts/test.sh` holds `internal/web` at 100%; every new branch needs a
  test.

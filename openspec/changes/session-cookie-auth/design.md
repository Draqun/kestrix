## Context

See proposal.md — Why. The constraints that shape this design:

- `internal/web/middleware.go:48-93` (`RequireAuth`) reads the token from the
  `Authorization` header only, verifies it, resolves the identity through the
  cache, checks `token_version`, and publishes `domain.Claims`. Every rule
  about what a token means already lives here; the cookie is only a second
  place to find the same string.
- `internal/web/handler_auth.go:36-58` (`handleLogin`) returns
  `{token, expires_at}`. `internal/auth/token.go:16` fixes `TokenTTL = time.Hour`;
  the cookie's `Max-Age` is derived from `expiresAt`, never a second constant.
- `internal/web/server.go:292-297` registers the public group (`/health`,
  `/auth/login`); `server.go:298-350` the `RequireAuth` group.
- `ui/src/context/AuthContext.jsx:16-31` reads and writes `sessionStorage`;
  `AuthContext.jsx:36-46` decodes `sub` and `role` from the token for display.
  `ui/src/api/client.js:14-34` holds the module-level `authToken` and attaches
  the header in `apiFetch`. Once the token is `HttpOnly`, the interface can
  neither store nor decode it, so it needs another source for name and role.
- The bundled UI is same-origin with the API (`server.go:372` serves both), and
  the Vite dev server proxies `/api` to `:8080` (`ui/vite.config.js:9-12`), so
  the browser sees one origin in every supported deployment.
- `internal/web/security_headers.go:8` sends `default-src 'self'` and
  `frame-ancestors 'none'`; `docs/*` has its own policy (`:14`). Swagger UI
  is a bearer client through its Authorize dialog.
- `scripts/docker-deployment-e2e.sh:182-241` logs in and uses the bearer token
  from the body; it must keep working unchanged.
- `scripts/test.sh` holds `internal/web` at 100%.

## Goals / Non-Goals

**Goals:**

- Script running in the page cannot obtain the token.
- One verification path: cookie and header tokens are the same token, rejected
  by the same code, with byte-identical responses.
- No change for bearer clients, and no second credential type.
- The interface's only stored session state is non-secret (name, role).

**Non-Goals:**

- Cross-origin UI deployments. `SameSite=Strict` and `Path=/api` assume the UI
  is served by Kestrix or proxied to look like it.
- A CSRF token per form or per session. See D3.
- Server-side session storage. See D1.

## Architecture

```
  browser ── POST /api/auth/login ──▶ handleLogin ──▶ AuthService.Login
             {name,password}              │
             ◀── 200 {token,expires_at} ──┤
                 Set-Cookie: kestrix_session=<token>;
                   HttpOnly; Secure; SameSite=Strict; Path=/api; Max-Age=<ttl>

  browser ── GET  /api/projects ───────────────┐
             Cookie: kestrix_session           │
  browser ── POST /api/projects/x/run ─────────┤
             Cookie: kestrix_session           │
             X-Kestrix-Request: 1              │
  CLI/TF ── POST /api/projects/x/run ──────────┤
             Authorization: Bearer <token>     │
                                               ▼
                                   ┌───────────────────────┐
                                   │ RequireAuth           │
                                   │ 1 token := header     │
                                   │     ?? cookie         │
                                   │ 2 from cookie &&      │
                                   │   method mutates &&   │
                                   │   !X-Kestrix-Request  │
                                   │     ▶ 403             │
                                   │ 3 verify, identity,   │
                                   │   version (unchanged) │
                                   └──────────┬────────────┘
                                              ▼
                                       RequireRole ▶ handler

  browser ── GET  /api/auth/me ───▶ {name, role, expires_at}   (restore on load)
  browser ── POST /api/auth/logout ▶ Set-Cookie: kestrix_session=; Max-Age=0
```

## Decisions

### D1. The cookie carries the JWT itself; there is no session table

`Set-Cookie: kestrix_session=<the same token the body returns>`. `RequireAuth`
takes the string from the cookie when the header is absent and runs the code
it already runs. Revocation is unchanged: a role or password change bumps
`token_version` and the cookie's token dies with it.

Alternative considered: an opaque session id backed by a `sessions` table.
Rejected — it adds a store, a port, a mock, a cleanup job, and a second
revocation mechanism, to solve a problem `token_version` already solves. The
token is short-lived and the identity cache bounds staleness; a table would
buy nothing the specs require.

Consequence: the cookie is as large as a JWT (a few hundred bytes). Acceptable.

### D2. Header wins over cookie, and a malformed header does not fall back

If `Authorization` is present it is the credential, full stop, even when a
valid cookie is also present. A header that is present but not `Bearer …` is
`401` as today, not a fall-through to the cookie.

Reason: a bearer client that sends a bad header should learn that, not be
silently authenticated as whoever the browser profile is. And the CSRF rule in
D3 depends on knowing which credential authenticated the request; falling back
would let a page attach a junk header to switch the request into the
cookie-plus-no-check path.

### D3. CSRF: a custom request header on cookie-authenticated mutations

A request authenticated by the cookie whose method is not `GET`, `HEAD`, or
`OPTIONS` must carry `X-Kestrix-Request` (any non-empty value). Missing header
is `403` from `RequireAuth`, before identity resolution, so it costs no store
lookup. Bearer requests are exempt: a bearer token is not an ambient credential,
so a cross-site page cannot make the browser attach it.

Why this works: a browser will only send a custom header cross-origin after a
CORS preflight, and Kestrix serves no CORS headers, so the preflight fails and
the request is never sent. Same-origin page script can set the header, but
same-origin script is exactly the attacker `HttpOnly` is not meant to stop —
that is the XSS threat CSP addresses, and it is out of scope for this change.

`SameSite=Strict` is set as well and is defense in depth, not the control:
older browsers, and some navigation edge cases, do not honor it uniformly.

Alternative considered: double-submit cookie (a second, readable CSRF cookie
echoed in a header). Rejected — it needs a second cookie, a generator, and a
comparison, and the readable half is again exposed to script. The custom-header
check achieves the same guarantee for one `if`.

Alternative considered: synchronizer token stored server-side. Rejected for the
same reason as the session table in D1.

### D4. `Path=/api`, `SameSite=Strict`, `Secure` by default

`Path=/api` keeps the cookie off static assets and `/docs/*`, so Swagger UI
never carries it and the top-level navigation to `/` never needs it — which is
what makes `Strict` harmless: a link from GitLab into the Kestrix UI loads the
SPA without the cookie, and the SPA's first `fetch` to `/api/auth/me` is
same-site and carries it.

`Secure` defaults on. Browsers treat `http://localhost` as a secure context, so
`make dev` and `make ui-serve` work unchanged. A plain-HTTP deployment on a
non-loopback host (the Compose profile on `:18080` without TLS in front) needs
`KESTRIX_COOKIE_SECURE=false`, and the documentation says why that is a
downgrade rather than a switch.

### D5. `GET /api/auth/me` replaces client-side token decoding

`AuthContext.jsx:36-46` decodes name and role from the token to render the
header and hide actions. With `HttpOnly` there is nothing to decode. Two
options: return name and role in the login body and persist them, or ask the
server. Both are needed: the login response already implies them (the UI knows
the name it sent, and the token body could be decoded once — but it is still
returned in the body only for bearer clients, and the UI should stop touching
it), and a reload has nothing at all.

`GET /api/auth/me` runs behind `RequireAuth` and returns
`{name, role, expires_at}` from `domain.Claims` — the stored role, not the
token's, so a demoted account sees its new role on the next load. The UI calls
it on mount; `401` means "show login". It replaces the `readStoredToken()` init.

Alternative considered: keep decoding the token from the login response and
persist name/role in `sessionStorage`. Rejected — name and role are not secret,
but a persisted role would go stale after a demotion until reload, and the
`me` call is one request the dashboard already amortizes.

### D6. Logout is public and idempotent

`POST /api/auth/logout` sets `kestrix_session=; Max-Age=0; Path=/api` and
answers `204` whether or not a valid cookie arrived. It is registered on the
public group so an expired session can still be cleaned up (an expired token
would `401` behind `RequireAuth` and leave the cookie in place until `Max-Age`
runs out — harmless, but confusing). It does not bump `token_version`: the
bearer clients of the same account are not the browser's to revoke, and an
administrator who wants that has the existing password/role path.

Cross-site logout is the only abuse: `SameSite=Strict` blocks the cookie on
such a request, so the clearing `Set-Cookie` is a no-op there.

### D7. Swagger UI stays bearer-only

`/docs/*` has its own CSP and lives outside `Path=/api`, so the cookie is never
sent to it; Swagger's own `fetch` calls to `/api/*` would carry the cookie, but
without `X-Kestrix-Request` every mutation would `403`, and its `GET`s would
succeed silently as the browser's session — a confusing half-state. The
Authorize dialog keeps taking a bearer token, and the documentation says so.
No code change; the CSRF rule already produces this outcome for mutations.

## Risks / Trade-offs

- [`Secure` cookie on a plain-HTTP, non-loopback deployment silently never
  arrives; the UI appears to log in and immediately bounces] → the login page
  shows a specific hint when `/api/auth/me` fails right after a `200` login,
  and `docs/authentication.md` names `KESTRIX_COOKIE_SECURE` in the Compose
  section.
- [A same-origin XSS can still act as the user by calling the API with the
  header] → unchanged from today and out of scope; what changes is that the
  token itself can no longer be exfiltrated and reused after the page closes.
- [Cookie and header disagree on identity and a developer expects the cookie]
  → D2 is documented; the `me` endpoint reflects whichever credential won.
- [`SameSite=Strict` blocks the cookie on a cross-site top-level `POST`] →
  Kestrix has no such flow; the UI is a SPA that only `fetch`es.
- [A reverse proxy strips or rewrites `Set-Cookie` `Path`] → the deployment
  documentation lists the cookie attributes so a proxy rule can be checked.

## Migration Plan

1. Deploy. No schema change, no key change, no token format change.
2. Every open browser session receives `401` on its next call (the stored
   token is gone from the new bundle) and is returned to login once.
3. Bearer clients, the Docker E2E test, and the Terraform provider need no
   change.
4. Rollback: redeploy the previous binary and bundle. Cookies set by the new
   version are ignored by the old `RequireAuth` and expire on their own within
   the hour.

## Security considerations

- The token never reaches page script after this change; `HttpOnly` is
  enforced by the browser, not by the UI code.
- The CSRF check runs before identity resolution and before the handler, so a
  missing header costs no database round trip and changes no state.
- `401` bodies for a rejected cookie token are the same `invalidTokenMessage`
  used for a header token, so a caller cannot tell the transport from the
  response.
- `Set-Cookie` is only emitted after `AuthService.Login` succeeds; a failed
  login sets nothing, so the response to a wrong password cannot be
  distinguished from an unknown account by cookie presence either.
- Logging (`logAuthFailure`, `logDenial`) receives no token from the cookie
  path, matching the existing rule for the header path.

# Authentication and access control

Every `/api` route requires a signed-in account, except `GET /api/health`,
`POST /api/auth/login`, and `POST /api/auth/logout`. `POST /webhook` is
unaffected: it authenticates GitLab deliveries with its own per-project and
global secrets, not a user account.
See [Webhook Receiver: Authentication](webhook-receiver.md#authentication)
for how those secrets are chosen and why verification fails closed.

## Signing in

`POST /api/auth/login` accepts a name and password and, on success, returns a
signed token and its expiry:

```bash
curl -X POST http://localhost:8080/api/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"name":"alice","password":"..."}'
```

A bearer client (the CLI, scripts, the Terraform provider) sends that token
on every later request as `Authorization: Bearer <token>`. A token is valid
for 60 minutes; there is no refresh flow, so a client re-authenticates by
logging in again. A request with no credential, a malformed header, or an
invalid/expired token receives `401`.

The login request itself must carry `Content-Type: application/json` -
anything else (including no header at all) is rejected with `415` before any
password verification runs. This is not an accident of the JSON decoder: it
is what keeps an HTML form from forging a login on a visitor's behalf now
that login has a browser-visible side effect (the cookie below).

### Browser sessions: the `kestrix_session` cookie

The same `200` login response also sets a cookie carrying the same token:

```
Set-Cookie: kestrix_session=<token>; Path=/api; HttpOnly; SameSite=Strict; Secure; Max-Age=<seconds until expiry>
```

- `HttpOnly` - never readable by page script. This is what closes the token
  exposure a Content-Security-Policy can only narrow (see "Security response
  headers" below).
- `Path=/api` - never sent to `/`, static assets, or `/docs/*`.
- `SameSite=Strict` and `Secure` - defense in depth; the actual CSRF control
  is the header rule below.
- `Secure` is controlled by `KESTRIX_COOKIE_SECURE` (default `true`). Leave it
  at the default on any deployment reachable over HTTPS, including
  `http://localhost` (browsers treat loopback as a secure context, so
  `make dev` and `make ui-serve` work unchanged). Set it to `false` only for
  a plain-HTTP deployment on a non-loopback host - a `Secure` cookie is
  silently dropped by the browser there, and the login page shows a hint
  when a `200` login is immediately followed by a failing `/api/auth/me` -
  the symptom of this exact misconfiguration.

`RequireAuth` reads the bearer header when present and the cookie otherwise;
when both are present, the header wins and the cookie is ignored entirely - a
malformed header never falls back to the cookie. A token presented either way
is verified by exactly the same rules, including token-version revocation,
and a rejected cookie token answers with the same `401` body as a rejected
header token, so the transport never leaks through the response.

### Same-origin proof for cookie mutations

A request authenticated by the cookie whose method is not `GET`, `HEAD`, or
`OPTIONS` must also carry the header `X-Kestrix-Request` (any non-empty
value); without it, Kestrix answers `403` before the route's handler runs.
Bearer-authenticated requests are exempt - a bearer token is never sent
automatically by the browser, so it needs no such proof. The bundled UI sends
this header on every request. This is the actual CSRF control: a browser
only attaches a custom header cross-origin after a CORS preflight, and
Kestrix sends no CORS headers, so that preflight fails and the request never
reaches the server.

### Logout and the current session

`POST /api/auth/logout` clears the cookie and always answers `204`, whether
or not a session was present; it is public (no credential required) so an
already-expired session can still be cleaned up. It never changes
`token_version`, so it has no effect on any bearer client holding a token for
the same account.

Logout still requires the `X-Kestrix-Request` header above, even though it
authenticates no one: without it, a cross-site page could force any visitor
to be signed out (the browser honors this response's clearing `Set-Cookie`
regardless of the request being cross-site - `SameSite=Strict` governs
attaching an *existing* cookie to a request, not accepting one from a
response). The header is not a credential, so a session-less cleanup call
that does send it is still served; the bundled UI always sends it.

`GET /api/auth/me` returns the signed-in account's current identity -
`{name, role, expires_at}`, from the stored role, never the token's - so a
client that cannot decode its own `HttpOnly` cookie can still restore a
session on reload:

```bash
curl -b cookies.txt http://localhost:8080/api/auth/me
```

### Swagger UI

`/docs/*` lives outside `Path=/api`, so the browser never sends the session
cookie there; its Authorize dialog keeps taking a bearer token exactly as
before. Cookie sessions apply only to the bundled single-page UI, which is
always served same-origin with the API.

## Roles

Every account has one global role: `viewer`, `operator`, or `admin`, each
including everything the role below it can do.

| Role | Can do |
|------|--------|
| `viewer` | Read pipelines, projects, runs, webhook history, and dashboard data. |
| `operator` | Everything `viewer` can, plus start and cancel runs, approve manual tasks, and edit pipeline definitions. |
| `admin` | Everything `operator` can, plus manage repository credentials, project registration/deletion, and accounts. |

A request whose role is too low for the route receives `403`, distinct from
the `401` a missing or invalid token receives.

## Per-project grants

An account can additionally hold a role grant scoped to one project. On a
request scoped to that project, the effective role is the higher of the
account's global role and its grant - a grant can raise access on one
project, never lower it. A global `operator` therefore keeps operator access
everywhere regardless of any grant.

Manage grants as an administrator, either from the Settings page's Accounts
section (create, promote, grant, and delete accounts without scripting HTTP
calls) or directly against the API:

```bash
curl -X PUT http://localhost:8080/api/users/alice/grants \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"grants":[{"project_name":"payments-api","role":"operator"}]}'
```

The request replaces the account's full grant set: a project omitted from the
list loses its grant. The Accounts section's grant control always sends the
full replacement set the same way, one field at a time (role via `PUT
.../role`, grants via `PUT .../grants`) rather than a combined update - the
grant target project must already be registered (`POST /api/projects`) or the
request is rejected.

## Revocation

Every request re-checks the caller's account against the database, not
against the token: the token proves identity, but the database is the
authority on what that account may currently do. A changed role, a changed
password, or a deleted account therefore takes effect immediately for the
process that made the change, and within `KESTRIX_IDENTITY_CACHE_TTL`
(default `5s`) for every other Kestrix process - `0` disables the cache
entirely, so every request resolves fresh. That bound is the revocation
latency: worst case, how long a session can act on since-revoked access
before Kestrix notices.

A role change or a password change also invalidates every token issued
before it (its stored token version moves), so the account authenticates
again to get a token usable at all - a lowered role does not quietly limit
what an old token can do, it stops the old token from working. Deleting an
account has the same effect for the same reason: the account no longer
resolves.

## Failure behavior

An authorization decision that cannot read the account or grant it needs
fails closed: Kestrix answers `503`, not a silently reduced role and not
`403`. A grant-store or user-store outage is therefore visible immediately as
`503` responses across the API, distinct from `401` (bad or absent
credentials) and `403` (a real, successful permission check that came out
too low). If you see `503` from an authenticated route, treat it as a
database-reachability incident, not a permissions problem - check the
database connection before checking any account's role or grants.

## Login throttling

`POST /api/auth/login` limits failed attempts per account name and per
client address independently: `KESTRIX_LOGIN_MAX_ATTEMPTS` (default `10`)
failures within `KESTRIX_LOGIN_WINDOW` (default `1m`) trip a `429` with a
`Retry-After` header, and a throttled attempt never runs password
verification. An unknown account name is throttled exactly like a real one,
so the response never discloses which accounts exist. A successful login
clears that account's own counter; a client address's counter is unaffected
by any one account's success, since other accounts may still be under attack
from it.

The throttle is per Kestrix process, not shared across replicas - running
more than one API process behind a load balancer multiplies the effective
limit by the replica count. `KESTRIX_TRUST_PROXY_HEADER` (default `false`)
controls whether the client-address key comes from the connection's peer
address or from the first hop of `X-Forwarded-For`; enable it only behind a
reverse proxy that sets that header itself; leaving it on elsewhere lets any
client forge its own throttle key.

## Request limits

The HTTP listener enforces read, write, and idle timeouts
(`KESTRIX_HTTP_READ_TIMEOUT`, `KESTRIX_HTTP_WRITE_TIMEOUT`,
`KESTRIX_HTTP_IDLE_TIMEOUT`; defaults `30s`/`120s`/`120s`) and bounds every
request body. `/api` routes share `KESTRIX_HTTP_MAX_BODY_BYTES` (default
`1048576`, 1 MiB); `POST /webhook` has its own, larger
`KESTRIX_WEBHOOK_MAX_BODY_BYTES` (default `8388608`, 8 MiB), since a push
payload carries file lists for up to twenty commits. A request whose body
exceeds its bound is rejected with `413` and never reaches the route's
business logic; a login request rejected this way never runs the throttle or
password verification either. Each setting must be a positive value - `0`, a
negative number, or an unparseable value fails Kestrix's startup.

## Security response headers

Every response carries `X-Content-Type-Options: nosniff`,
`Referrer-Policy: no-referrer`, `X-Frame-Options: DENY`, and a
`Content-Security-Policy` tuned for the bundled UI (same-origin scripts and
styles, no framing). `KESTRIX_CONTENT_SECURITY_POLICY` overrides the policy
for the application routes if you serve the UI from elsewhere; `/docs/*`
keeps its own, separately relaxed policy for Swagger UI's inline script
regardless of that setting. The bundled UI never holds the token in
script-readable storage: the browser session lives entirely in the
`HttpOnly` `kestrix_session` cookie described above, so a same-origin script
that runs in the page cannot read or exfiltrate it. These headers, and the
cookie's `HttpOnly` mark, are complementary defenses against different
threats: a page-script compromise can still call the API as the signed-in
user while it runs (see the CSRF section above for why it cannot forge a
mutating request), but it can no longer walk away with a reusable token.

## Provisioning the signing key

Kestrix signs tokens with the file named by `KESTRIX_JWT_SIGNING_KEY_PATH` -
32 raw bytes or 64 hex characters, mode `0600`. It is validated before the
HTTP listener opens, exactly like `KESTRIX_MASTER_KEY_PATH`:

```bash
mkdir -p ~/.kestrix
(umask 077 && openssl rand -hex 32 > ~/.kestrix/jwt.key)
export KESTRIX_JWT_SIGNING_KEY_PATH="$HOME/.kestrix/jwt.key"
```

Use a different file from `KESTRIX_MASTER_KEY_PATH`. The signing key
authenticates API sessions; the master key decrypts stored Git credentials.
Keeping them separate means a leaked signing key does not also expose
credential plaintext, and rotating one never forces rotating the other.
Restarting Kestrix with the same signing key file keeps existing unexpired
tokens valid; changing the file invalidates every outstanding token.

## Creating and recovering accounts

Kestrix never seeds an account automatically and has no unauthenticated mode
- an empty account table leaves the API reachable but unusable. The
`kestrix user` command manages accounts directly against the database, so it
works without a reachable API:

```bash
kestrix user create --role admin alice   # prompts for a password
kestrix user list
kestrix user passwd alice
kestrix user delete alice
```

The password is always read from a terminal prompt or standard input, never
from a command-line flag, so it cannot appear in shell history or the process
table.

If every administrator account is lost or locked out, `kestrix user create`
against the same `DATABASE_URL` recovers access without any other bootstrap
step - it is the same path used to create the first account.

## Attribution

Kestrix records which account started, approved, or canceled a run, so the
run detail can always answer "who did this":

- Starting a run sets the run's `started_by`.
- Approving a manual task sets that task's result entry `approved_by` and
  `approved_at`. Re-running a task directly by name (rather than through the
  approval gate) sets the same two fields, since both routes share the same
  execution path.
- Canceling a running run sets the run's `canceled_by`.

Each value is the actor string `user:<account name>`, taken from the signed
token that authenticated the request - never from the request body, a
header, or a query parameter, so it cannot be spoofed. Kestrix writes one log
line for each of the three actions, naming the actor, project, run id, and
(for an approval) the task name.

Attribution is stored as that string, not a reference to the account: it is
copied at the moment of the action and survives the account being deleted
later. A run started, approved, or canceled before this feature existed has
no recorded actor; the UI shows `unknown` for those fields, and no backfill
is possible since the information was never captured.

## Compatibility

Deploying this version is not backward compatible: `KESTRIX_JWT_SIGNING_KEY_PATH`
must be provisioned before start, and an administrator account must exist
before the UI or API is usable. See the [Usage guide](usage.md) for the
Compose key-file mount and the `.env.template` entry.

Deploying rbac-hardening is also breaking on its own: every token issued
before the upgrade carries no token version and is rejected, so every
signed-in session must log in again once. Nothing else about an existing
deployment's accounts, roles, or grants changes.

# Authentication and access control

Every `/api` route requires a signed-in account, except `GET /api/health` and
`POST /api/auth/login`. `POST /webhook` is unaffected: it authenticates GitLab
deliveries with its own per-project and global secrets, not a user account.
See [Webhook Receiver: Authentication](webhook-receiver.md#authentication)
for how those secrets are chosen and why verification fails closed.

## Signing in

The UI shows a login screen until a token is present. `POST /api/auth/login`
accepts a name and password and, on success, returns a signed token and its
expiry:

```bash
curl -X POST http://localhost:8080/api/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"name":"alice","password":"..."}'
```

Send the token on every later request as `Authorization: Bearer <token>`. A
token is valid for 60 minutes; there is no refresh flow, so a client
re-authenticates by logging in again. A request with no token, a malformed
header, or an invalid/expired token receives `401`.

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

Manage grants as an administrator:

```bash
curl -X PUT http://localhost:8080/api/users/alice/grants \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"grants":[{"project_name":"payments-api","role":"operator"}]}'
```

The request replaces the account's full grant set: a project omitted from the
list loses its grant.

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
regardless of that setting. These headers narrow, but do not remove, the
exposure from keeping the session token in `sessionStorage` rather than an
`HttpOnly` cookie - see [design.md D8](../openspec/changes/rbac-hardening/design.md)
for that trade-off.

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

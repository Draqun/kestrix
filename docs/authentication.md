# Authentication and access control

Every `/api` route requires a signed-in account, except `GET /api/health` and
`POST /api/auth/login`. `POST /webhook` is unaffected: it authenticates GitLab
deliveries with its own shared secret, not a user account, and continues to
work exactly as before.

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

## Compatibility

Deploying this version is not backward compatible: `KESTRIX_JWT_SIGNING_KEY_PATH`
must be provisioned before start, and an administrator account must exist
before the UI or API is usable. See the [Usage guide](usage.md) for the
Compose key-file mount and the `.env.template` entry.

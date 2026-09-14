## Why

`access-control`'s "Account administration API" requirement is fully
implemented: `GET/POST /api/users`, `PUT /api/users/{user}/role`,
`PUT /api/users/{user}/grants`, and `DELETE /api/users/{user}`
(`internal/web/handler_user.go`) exist, are admin-gated, and are covered by
tests. But `ui/src/pages` has no page for any of it — `find ui/src/pages`
lists `Dashboard.jsx`, `Login.jsx`, `PipelineEditor.jsx`, `Pipelines.jsx`,
`Projects.jsx`, `Settings.jsx`, `WebhookLogs.jsx`, and nothing for accounts.
An administrator who wants to create an account, promote a user, grant
project-scoped access, or remove an account must use `curl` or the Swagger UI
at `/docs`. The capability is reachable, but only for someone comfortable
scripting HTTP calls — every other admin-only action in the product (project
lifecycle, credentials) already has a page under `Settings` or `Projects`.

## What Changes

- A new "Accounts" section (under `Settings`, alongside Credentials) lists
  every account with its global role and project grants.
- Forms to create an account (name, password, initial role), change an
  account's global role, edit its project grants (add/remove a project +
  role pair), and delete an account, each calling the existing API.
- The section is visible only to an authenticated admin, matching how the
  Credentials section already hides itself from non-admins.

## Non-goals

- Self-service password change or reset (there is no such endpoint yet;
  password rotation stays `kestrix user passwd` via CLI).
- Any new API endpoint or response shape — the existing four routes already
  cover every action this page needs.
- Audit logging of account-administration actions; out of scope here.

## Capabilities

### New Capabilities

- `ui-account-administration`: the Accounts page — what it lists, what an
  admin can do from it, and how it enforces the same admin-only visibility
  the rest of the Settings area already uses.

### Modified Capabilities

(none — `access-control`'s API requirement is unchanged; this only adds the
UI that consumes it)

## Impact

- `ui/src/pages/Settings.jsx` (or a new `ui/src/pages/Accounts.jsx` linked
  from Settings, matching how Credentials is organized).
- `ui/src/api/client.js`: thin wrappers for the four existing `/api/users*`
  routes, following the pattern already used for credentials/projects.
- No backend change.

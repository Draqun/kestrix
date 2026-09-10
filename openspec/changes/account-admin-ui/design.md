## Context

See proposal.md — Why.

```
ui/src/pages/Settings.jsx
  └─ RepositoryCredentialsSection   (existing pattern: admin-only, own component file)
  └─ AccountsSection                (new, same pattern)
        │
        ▼
ui/src/api/client.js
  listAccounts()      ──► GET    /api/users
  createAccount()     ──► POST   /api/users
  setAccountRole()    ──► PUT    /api/users/{user}/role
  setAccountGrants()  ──► PUT    /api/users/{user}/grants
  deleteAccount()     ──► DELETE /api/users/{user}
```

`RepositoryCredentialsSection.jsx` already establishes the conventions this
follows: a self-contained component under `ui/src/components/`, admin-only
visibility resolved from the same auth context Settings already reads, thin
`client.js` wrappers matching the credentials ones (`listCredentials`,
`createCredential`, `revokeCredential`) at lines ~306-330.

## Goals / Non-Goals

**Goals:** every action `access-control`'s API already allows an admin to
perform is reachable from this one page.

**Non-Goals:** self-service password reset, audit logging, bulk import —
see proposal.md — Non-goals.

## Decisions

**D1: A new `AccountsSection` component and `Accounts.jsx`-free — keep it as
a Settings subsection, not a top-level route, matching where Credentials
already lives.** Alternative considered: a top-level `/accounts` page like
`Projects`/`Pipelines`. Rejected — account administration is an
infrequent admin task like credential management, not a primary workflow
surface; co-locating with Settings keeps navigation flat.

**D2: Client-side role/grant editing calls one endpoint per field changed
(role via `setAccountRole`, grants via `setAccountGrants`), not a combined
"update account" call — matching the API's own separation
(`PUT /role` vs `PUT /grants`) rather than inventing a merged request shape.**

**D3: No optimistic UI updates — apply a change to the list only after its
request resolves, and show the server's error otherwise (see spec scenario
"Backend rejection is surfaced").** Consistent with how
`RepositoryCredentialsSection` already handles its own mutations.

## Security Considerations

The page adds no new backend authority — it only exposes existing
admin-gated endpoints. It must not fetch or render the list for anyone
whose global role isn't admin (client-side hiding is a UX convenience only;
`RequireRole(RoleAdmin)` on the server already enforces the real boundary),
and it must never echo a password value back into the DOM after a create
(the API already never returns one).

## Migration Plan

No backend or data change. Deploy as a normal frontend release. Rollback is
a plain revert; the underlying API is unaffected either way.

## 1. API client wrappers

- [ ] 1.1 Add `listAccounts`, `createAccount`, `setAccountRole`, `setAccountGrants`, `deleteAccount` to `ui/src/api/client.js`, following the existing `list/create/revokeCredential` pattern (base URL, JSON body, thrown `Error` on non-OK response); verify a client test (or manual fetch-mock test, matching how credentials wrappers are tested) covers each function's request method, URL, and body shape.

## 2. Accounts section component

- [ ] 2.1 Create `ui/src/components/accounts/AccountsSection.jsx` rendering the account list (name, global role, grants) from `listAccounts`, visible only when the current user's global role is admin (reuse the same auth-context check `RepositoryCredentialsSection` uses); verify a component test asserts the section renders nothing for a non-admin session and the list for an admin session.
- [ ] 2.2 Add the create-account form (name, password, role) calling `createAccount`, appending the new row on success without a page reload; verify a component test submits the form and asserts the new row appears and the server error path (per the spec's "Backend rejection is surfaced" scenario) renders an error instead of a false-positive row.
- [ ] 2.3 Add per-row controls for role change (`setAccountRole`) and grant add/remove (`setAccountGrants`), updating only that row on success; verify component tests for both.
- [ ] 2.4 Add a delete control per row calling `deleteAccount`, removing the row on success; verify a component test.
- [ ] 2.5 Wire `AccountsSection` into `ui/src/pages/Settings.jsx` alongside `RepositoryCredentialsSection`.

## 3. Regression coverage

- [ ] 3.1 Run `make ui-test` (or the project's frontend test command) and confirm the new component tests pass alongside the existing Settings/Credentials suite.
- [ ] 3.2 Manual smoke test: as an admin, create an account, promote it, grant it a project, delete it, all from the Accounts section, confirming each action matches the equivalent `curl` call's effect.

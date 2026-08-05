# Verification

## Automated scenario mapping

| Capability scenario | Verification |
|---|---|
| Project onboarding: Create project from the UI | `Projects.component.test.jsx` verifies the remote dialog, active-only metadata, and absence of the full manager; `projectForm.component.test.jsx` verifies the remote-only payload. |
| Project onboarding: Incomplete form | `projectForm.test.js` and the measured `projectForm.component.test.jsx` verify required name, URL, ref, and pipeline directory plus omission of `repo_path`. |
| Project onboarding: First private project needs a new credential | `CredentialSelectField.component.test.jsx` creates a write-only credential inline, preserves all entered project fields, refreshes metadata, selects the returned reference, and verifies no secret enters the project payload. |
| Project onboarding: Manage an existing credential | `Projects.component.test.jsx` verifies the Settings management link and absence of rotate/revoke controls; `Settings.component.test.jsx` verifies those controls at their destination. |
| Credential settings: Open repository credential settings | `Settings.component.test.jsx` verifies metadata-only listing and create/revoke actions; `RepositoryCredentialsSection.rotate.component.test.jsx` verifies rotation and refresh. |
| Credential settings: Credential lifecycle is absent from project pages | `Projects.component.test.jsx` covers the list and `Projects.detail.component.test.jsx` covers project detail. |
| Credential type: Select username and password with a pointer | `CredentialFormDialog.component.test.jsx`. |
| Credential type: Select SSH with the keyboard | `CredentialFormDialog.component.test.jsx`. |
| Credential type: Change type after entering a secret | `CredentialFormDialog.component.test.jsx` and `credentialForm.utility.component.test.jsx` verify clearing and type-specific payloads. |
| Secret-safe operations: Create a credential in Settings | `Settings.component.test.jsx` verifies close/reset, metadata refresh, and absence of the submitted secret. |
| Secret-safe operations: Revoke a credential | `Settings.component.test.jsx` verifies confirmation and refreshed revoked state; `Projects.component.test.jsx` verifies revoked metadata is excluded from selection. |
| Secret-safe operations: Credential operation fails | `CredentialFormDialog.component.test.jsx` verifies an actionable API error without rendering the submitted secret. |
| Responsive navigation: Open Settings from the drawer | `Layout.component.test.jsx` covers desktop activation and direct tablet `/settings` active state. |
| Responsive navigation: Open Settings on mobile | `Layout.component.test.jsx`, `Settings.mobile.component.test.jsx`, and `Projects.mobile.component.test.jsx` cover 375 px navigation, Settings actions, and project onboarding controls. |
| Responsive navigation: Navigation contains no credential destination | `Layout.component.test.jsx` verifies Settings is the only destination and no Repository credentials navigation item is rendered. |

`useKeyboardNav.component.test.jsx` additionally verifies the `g+s` Settings
shortcut and that editable fields retain keyboard input.

## Operational verification

- `make ui-test`: 36 Node tests and 19 measured jsdom tests passed. Coverage is
  94.94% statements, 89.79% branches, 93.65% functions, and 95.56% lines;
  enforced thresholds are 94/89/93/95 respectively.
- `make ui-build`: production Vite bundle and generated documentation built.
- `make quality`: formatting, vet, and backend unit/coverage checks passed.
- `make test-integration`: PostgreSQL-backed integration suite passed.
- `make docker-e2e`: production image, API lifecycle, direct `/settings` SPA
  route, Settings credential bundle, restart, cache, and secret-redaction checks
  passed.

## 1. Interaction test foundation

- [x] 1.1 Add a jsdom-based React component test setup and Testing Library
  development dependencies while keeping existing Node tests under the
  Makefile-provided `ui-test` target.
- [x] 1.2 Add failing pointer and keyboard interaction tests for selecting
  token, basic, and SSH credential types, conditional fields, incompatible
  secret clearing, and type-specific API payloads.
- [x] 1.3 Add failing navigation tests proving Settings is reachable and active
  on desktop, tablet, and mobile while no Repository credentials navigation
  item exists.
- [x] 1.4 Add failing project-dialog tests proving the full manager is absent,
  entered project fields survive inline credential creation, revoked entries
  are excluded, and the returned reference is selected.

## 2. Shared credential components

- [x] 2.1 Extract credential form defaults, type normalization, secret reset,
  and type-specific payload construction into tested frontend utilities.
- [x] 2.2 Implement a reusable `CredentialFormDialog` with controlled MUI
  `Select`/`MenuItem` type input, accessible labels, create/rotate modes, and
  secret-safe success, close, and error behavior.
- [x] 2.3 Implement a reusable credentials data hook and
  `RepositoryCredentialsSection` that lists metadata and refreshes after
  create, rotate, and confirmed revoke operations.
- [x] 2.4 Implement a compact `CredentialSelectField` that offers public access,
  active metadata only, inline creation, and a Settings management link.

## 3. Settings navigation

- [x] 3.1 Add the `/settings` page and route with Repository credentials as a
  section rather than a standalone page.
- [x] 3.2 Replace the disabled Settings placeholder with an active destination
  in desktop/tablet navigation and keep Settings reachable in mobile
  navigation without overflow at 375 px.
- [x] 3.3 Verify route active-state behavior and direct `/settings` navigation
  with component tests.

## 4. Project onboarding cleanup

- [x] 4.1 Replace create/edit project credential controls with the shared
  compact selector and inline creation dialog while preserving project form
  state and selecting newly created metadata.
- [x] 4.2 Remove both embedded full credential manager cards from project list
  and project detail and route rotate/revoke discovery to Settings.
- [x] 4.3 Add regression tests that project payloads contain only the selected
  credential reference and never credential input values.

## 5. Documentation and verification

- [x] 5.1 Update repository credential and project onboarding documentation to
  describe Settings management, inline creation, and keyboard-accessible type
  selection.
- [x] 5.2 Run `make ui-test`, `make ui-build`, and `make quality`; fix all
  frontend and shared regressions.
- [x] 5.3 Run `make docker-e2e` to verify the production bundle and responsive
  Settings route in the deployment image.
- [x] 5.4 Run strict OpenSpec validation and reconcile each scenario with an
  automated interaction test or explicit operational verification.
- [x] 5.5 Measure frontend unit coverage with an enforced high threshold for
  the changed credential, Settings, navigation, and project-form modules.
- [x] 5.6 Add 375 px interaction regressions for Settings credential actions
  and project onboarding, and keep the production deployment route covered.

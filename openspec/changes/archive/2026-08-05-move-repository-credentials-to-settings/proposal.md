## Why

Repository credential management is duplicated inside project list and detail
views, which gives an infrastructure-wide concern too much prominence and
clutters project workflows. The credential type control also uses a native
select integration that cannot be operated reliably in the current UI.

## What Changes

- Activate the existing Settings navigation destination and add a Settings
  page containing a Repository credentials section.
- Remove the full credential manager from project list and project detail
  views; credentials do not receive their own top-level navigation item.
- Keep a compact credential selector and an `Add credential` action in project
  create/edit dialogs so the first private project can still be onboarded
  without leaving an incomplete form.
- Replace the broken native type selector with an accessible MUI select whose
  token, basic, and SSH choices work with pointer and keyboard input.
- Preserve write-only secret handling, metadata-only lists, active-only project
  selection, rotation, revocation, and automatic selection after inline
  creation.
- Add interaction tests for Settings navigation, type selection, conditional
  secret fields, and project-onboarding credential creation.

## Capabilities

### New Capabilities

- `repository-credential-settings`: Settings-based credential creation,
  rotation, revocation, accessible type selection, and secret-safe feedback.

### Modified Capabilities

- `project-registration`: Replace embedded full credential managers with a
  compact active-credential selector and inline creation action.
- `ui-responsive-layout`: Make the Settings destination reachable and active
  in desktop, tablet, and mobile navigation without adding a credential-specific
  menu entry.

## Impact

- Frontend routing and navigation in `ui/src/App.jsx` and
  `ui/src/components/Layout.jsx`.
- Credential and project onboarding UI currently embedded in
  `ui/src/pages/Projects.jsx`, extracted into reusable components and a Settings
  page.
- Frontend test dependencies may gain a DOM interaction harness; backend APIs
  and database schema remain unchanged.
- User documentation will point credential lifecycle management to Settings.
- `complete-remote-project-workflow` is the planning prerequisite and must be
  archived or synced before this delta is archived.

## Non-goals

- Changing credential encryption, persistence, provider-neutral API contracts,
  or Git authentication behavior.
- Adding provider-specific OAuth flows, automatic credential discovery, or a
  standalone Repository credentials navigation item.

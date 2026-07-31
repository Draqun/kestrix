## Context

The credential API and encrypted PostgreSQL storage already expose a
provider-neutral, metadata-only lifecycle. The current React implementation
places one `CredentialsCard` inside both project list and project detail, while
the navigation contains a disabled Settings placeholder. The same component
also renders credential type through a native select nested in MUI
`TextField`; in the current browser UI its options cannot be selected reliably.

This change is frontend-focused. It follows `complete-remote-project-workflow`,
whose remote project onboarding and credential-reference contract remain the
baseline.

```text
Desktop/tablet/mobile navigation
              |
              v
       /settings route
              |
              v
 RepositoryCredentialsSection ----> credential metadata API
              |                         create/rotate/revoke
              v
     CredentialFormDialog
              ^
              |
 Project create/edit dialog ----> active credential selector
        compact Add action          + selected ref only
```

## Goals / Non-Goals

**Goals:**

- Give Settings one responsive navigation destination and keep credentials as
  a section rather than a top-level product area.
- Remove the full credential lifecycle UI from project pages.
- Preserve inline first-project onboarding through a compact creation dialog.
- Make credential type selection operable by pointer and keyboard.
- Keep all secret values write-only and absent from metadata, errors, and
  project payloads.
- Cover the regression with DOM-level interaction tests.

**Non-Goals:**

- Backend, encryption, schema, or credential API changes.
- Provider-specific OAuth or token acquisition.
- Showing stored secrets or adding a credential details route.
- Turning Settings into a general runtime-configuration editor in this change.

## Decisions

### Use one Settings route, not a credential route

Add `/settings` and turn the disabled Settings placeholder into a normal
responsive navigation destination. The Settings page initially contains a
Repository credentials section and can host future administrative sections.
There is no `/credentials` route or credential-specific navigation item.

The alternative was a dedicated credential page. It was rejected because the
workflow is infrequent and administrative, and it would consume equal visual
weight to Projects and Pipelines.

### Extract credential UI by responsibility

Split the current nested `CredentialsCard` into reusable pieces:

- `useCredentials` loads metadata, exposes active entries, and refreshes after
  mutations;
- `CredentialFormDialog` handles create or rotate input and returns metadata on
  success;
- `RepositoryCredentialsSection` lists metadata and owns create, rotate, and
  revoke actions for Settings;
- `CredentialSelectField` renders public access plus active references and a
  compact `Add credential` action for project create/edit dialogs.

Project pages no longer render the full manager. Inline creation refreshes the
selector and selects the returned reference. Rotation and revocation remain in
Settings.

Keeping one shared form avoids fixing the type selector in one location while
leaving another variant broken. The alternative of navigating from an
incomplete project dialog to Settings was rejected because it would discard or
complicate preservation of partially entered project data.

### Use controlled MUI Select/MenuItem

Render type as `FormControl`, `InputLabel`, `Select`, and `MenuItem`, without
`native` mode. The controlled value is one of `token`, `basic`, or `ssh`.
Pointer selection and keyboard selection must both update conditional fields.
An invalid or absent type is normalized to `token` when the form opens.

When type changes, incompatible secret fields are cleared. Submission builds a
type-specific payload so hidden values from a previously selected type cannot
be sent accidentally.

### Add DOM interaction coverage without replacing pure Node tests

Keep existing `node:test` helper and API tests. Add a jsdom-based component
test setup with Testing Library and user-event for navigation, dialog, select,
keyboard, conditional-field, and callback behavior. `make ui-test` remains the
single source-of-truth target and runs both suites.

This adds frontend-only development dependencies, but gives direct regression
coverage for the user-visible click failure that pure helper tests cannot
observe.

### Security considerations

Credential list and selection components consume metadata only. Secret fields
use password inputs where appropriate, are cleared after success or dialog
close, and are excluded from errors and labels. Type-specific payload building
prevents stale hidden secret fields from crossing the API boundary. UI errors
display only backend messages already covered by redaction tests and never
interpolate submitted form values.

## Risks / Trade-offs

- [Five mobile destinations may crowd bottom navigation] → Keep labels concise,
  test at 375 px, and use the existing responsive navigation component rather
  than a separate unreachable Settings control.
- [Two consumers can hold separate credential metadata snapshots] → Refresh on
  dialog open and after every create, rotate, or revoke operation.
- [A shared form can accumulate complex conditional state] → Centralize default
  state, type normalization, payload construction, and reset behavior in one
  module with focused tests.
- [New DOM test dependencies increase install size] → Keep them in
  `devDependencies` and retain the existing production bundle unchanged.

## Migration Plan

1. Add the shared credential components and interaction tests while the old
   card remains available.
2. Add `/settings` and responsive navigation coverage.
3. Replace project-page cards with the compact selector/create action.
4. Remove the old nested card implementation and update documentation.
5. Run `make ui-test`, `make ui-build`, and the deployment E2E target.

Rollback is frontend-only: restore the previous route/navigation and embedded
card while leaving credential data and backend APIs untouched.

## Open Questions

None. Runtime configuration editing and other Settings sections remain future
changes.

# Proposal: project-dialog-form-unification

## Why

In MR !42, `WebhookSecretField` was implemented inline inside `Projects.jsx` and duplicated across create and edit dialogs. Furthermore, the create dialog manages individual fields as loose `useState` scalars, whereas the edit dialog manages state through an `editForm` object. This inconsistency violates the form extraction precedent established by `CredentialFormDialog.jsx` and complicates adding new project attributes. The review in MR !42 deferred this refactoring to prevent cascading test churn across five test files.

## What Changes

- Extract `WebhookSecretField` into a reusable component in `ui/src/components/projects/WebhookSecretField.jsx`.
- Unify project creation and editing dialogs into a shared form state hook or common dialog component conforming to the `CredentialFormDialog` shape.
- Update the corresponding frontend unit and component tests.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

None (pure architectural refactoring without requirement changes; `skip_specs: true`).

## Non-goals

- Adding new project attributes or modifying API payload contracts.
- Changing dialog visual layout or styles.

## Impact

- Refactors `ui/src/pages/Projects.jsx`.
- Creates `ui/src/components/projects/WebhookSecretField.jsx`.
- Updates UI component tests in `ui/src/pages/Projects.component.test.jsx` and `Projects.webhook.component.test.jsx`.

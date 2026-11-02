# Design: project-dialog-form-unification

## Context

`Projects.jsx` currently contains two modal dialogs: `ProjectCreateDialog` and `ProjectEditDialog`. The create dialog manages `name`, `repoUrl`, `repoRef`, `webhookSecret`, and `pipelineFile` through separate `useState` hooks. The edit dialog bundles them into a single `form` object. The `WebhookSecretField` (with show/hide visibility toggle and masking) is copy-pasted in both dialog definitions.

## Goals / Non-Goals

**Goals:**
- Extract `WebhookSecretField` into `ui/src/components/projects/WebhookSecretField.jsx`.
- Unify form state handling across both dialogs into a consistent object model.
- Maintain 100% component test coverage and identical DOM accessibility attributes.

**Non-Goals:**
- Altering the backend API schema or response structures.

## Decisions

### D1: Dedicated `WebhookSecretField` component
- **Choice:** Create a self-contained component handling password visibility toggling, input masking, and helper text.
- **Alternatives considered:** Keeping it inline. Rejected because it duplicates 40 lines of JSX and state management.

### D2: Unified `useProjectForm` hook or shared form schema
- **Choice:** Standardize create and edit dialogs on a single form dictionary structure `{ name, repo_url, repo_ref, webhook_secret, pipeline_file }`.
- **Alternatives considered:** Merging create and edit into one single polymorphic dialog. Rejected to avoid complex conditional submission branches.

## Risks / Trade-offs

- **[Risk]** Breaking existing React testing library selectors in `Projects.component.test.jsx`.  
  → *Mitigation:* Preserve exact `aria-label`, placeholder, and button labels so tests pass without selector alterations.

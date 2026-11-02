# Tasks: project-dialog-form-unification

## 1. Component Extraction

- [ ] 1.1 Create `ui/src/components/projects/WebhookSecretField.jsx` and unit test it in isolation; verify `npm run test` passes.
- [ ] 1.2 Replace inline webhook secret input in `ProjectCreateDialog` and `ProjectEditDialog` with the new component; verify dialogs render properly.

## 2. Form State Unification & Verification

- [ ] 2.1 Refactor `ProjectCreateDialog` state management to match the object-based form model used in `ProjectEditDialog`; verify create mutation succeeds.
- [ ] 2.2 Run full UI test suite `npm run test` and `npm run build` from `ui/` directory; verify clean build with zero test failures.

## Workflow follow-up

- Archive change via `openspec archive project-dialog-form-unification -y` upon implementation.

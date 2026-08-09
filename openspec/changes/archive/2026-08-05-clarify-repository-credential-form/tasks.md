## 1. Interaction and utility regressions

- [x] 1.1 Add failing utility tests for field descriptors, dynamic
  requiredness, token username payloads, and clearing username/secrets when the
  credential type changes.
- [x] 1.2 Add failing component tests proving every common field has visible
  helper text, an accessible tooltip, and an unambiguous required or optional
  state in create and rotate modes.
- [x] 1.3 Add failing pointer and keyboard tests for token, basic, and SSH
  guidance, including pinned-versus-TOFU known-hosts behavior and
  type-applicable request fields only.
- [x] 1.4 Add failing secret-redaction and 375 px component tests for the Git
  source-only scope notice, field guidance, and dialog actions.

## 2. Credential form guidance

- [x] 2.1 Introduce shared common and type-specific credential field
  descriptors containing labels, requiredness, helper text, tooltip text, and
  non-secret examples.
- [x] 2.2 Implement a reusable accessible field-label/information control that
  keeps concise guidance visible and exposes supplemental MUI tooltips to
  pointer, keyboard, touch, and assistive-technology users.
- [x] 2.3 Render descriptor-driven common and type-specific fields in
  `CredentialFormDialog`, including optional HTTPS username for token
  credentials and synchronized type-change clearing.
- [x] 2.4 Add the persistent encrypted/write-only and Git-source-only scope
  notice, and keep all labels, helpers, controls, and actions usable at 375 px.

## 3. Documentation and verification

- [x] 3.1 Document each credential field, required/optional matrix, HTTPS token
  username examples, SSH known-hosts behavior, and the separation between
  repository authentication and deployment secrets.
- [x] 3.2 Run `make ui-test` and maintain the enforced frontend coverage
  thresholds while reconciling every specification scenario with an automated
  test.
- [x] 3.3 Run `make ui-lint`, `make ui-build`, and `make quality`; fix regressions
  introduced by the form guidance change.
- [x] 3.4 Run `make docker-e2e` and strict OpenSpec validation to verify the
  production Settings/project credential flow and complete artifact integrity.

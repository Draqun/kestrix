## Context

`CredentialFormDialog` is shared by Settings and project onboarding. It marks
some secret inputs as required, but common fields have only labels and
placeholders, Type has no explanation, and the token variant hides the optional
username already supported by the Go API. Provider and Host are metadata only,
while Known hosts changes SSH verification from trust-on-first-use to pinned
host keys. Repository credentials are resolved only by source acquisition and
are not task/deployment secrets.

```text
Settings / Project dialog
          |
          v
CredentialFormDialog
  |-- shared field guidance (label, helper, tooltip, requiredness)
  |-- type-aware form state and secret clearing
  `-- existing create / rotate API
                         |
                         v
             encrypted repository credential
                         |
                         v
                Git source acquisition only
                (never task/deploy environment)
```

## Goals / Non-Goals

**Goals:**

- Make every field understandable before a user submits a secret.
- Keep requiredness, visible inputs, and request payloads synchronized by type.
- Support token-plus-username Git authentication already accepted by the API.
- Make repository-only scope and SSH host-key behavior explicit.
- Preserve keyboard accessibility, secret safety, and 375 px usability.

**Non-Goals:**

- Build a deployment secret store or inject repository secrets into jobs.
- Add remote connection testing or provider auto-detection.
- Change backend validation, encryption, persistence, or credential resolution.

## Decisions

### Centralize field guidance as frontend metadata

Define common and type-specific field descriptors close to the existing
credential form utilities. Each descriptor contains the label, required flag,
short helper text, tooltip text, and non-secret placeholder where appropriate.
The dialog renders descriptors instead of maintaining explanatory strings in
multiple branches.

This keeps create and rotate modes, Settings, project onboarding, payload
construction, and tests aligned. Duplicating inline `TextField` configuration
was rejected because requiredness and guidance would drift as types evolve.

### Keep helper text visible and use tooltips as supplemental detail

Every input receives concise `helperText`. A focusable information icon with an
explicit accessible name exposes longer guidance through MUI `Tooltip`.
Tooltip-only guidance was rejected because hidden hover content is weak on
touch devices and for assistive technology.

### Preserve provider-neutral behavior

Provider and Host remain optional metadata. Examples can mention GitHub,
GitLab, Bitbucket, and self-hosted Git, but selecting or typing them does not
change payload shape or authentication behavior. Provider-specific form modes
were rejected because the backend intentionally models Git transport rather
than vendor APIs.

### Expose token username without changing the API

The token branch adds an optional HTTPS username input and includes it in the
existing token payload. Type changes clear username together with incompatible
secret state. Basic credentials continue to require both username and password.

### Explain SSH verification exactly as implemented

Known hosts remains optional. Guidance recommends pinning and states that an
empty value uses `StrictHostKeyChecking=accept-new`; supplied content uses
strict checking against the temporary known-hosts file.

### State the security boundary in the dialog

A persistent informational notice says secrets are write-only and encrypted,
used only for Git clone/fetch/ref resolution, and never added to task or
deployment environments. Deployment-secret support requires a separate
proposal because it has different scoping, masking, rotation, and audit needs.

## Security Considerations

- Guidance strings and placeholders MUST contain examples only, never copied
  values from secret state or API errors.
- Secret inputs remain password/multiline controls and are reset on type change,
  successful submission, and close.
- Tooltips MUST NOT serialize form state or expose secret values in accessible
  names.
- Documentation MUST not recommend embedding credentials in repository URLs,
  pipeline HCL, or committed configuration.

## Risks / Trade-offs

- [Risk] Dense helper text makes the dialog visually heavy. → Keep one concise
  line visible and move nuance into accessible tooltips.
- [Risk] Provider examples may be mistaken for enforced configuration. → State
  next to Provider and Host that they are descriptive metadata only.
- [Risk] Trust-on-first-use may be interpreted as equivalent to pinning. → Label
  it as the fallback and recommend explicit known-hosts content.
- [Risk] Users still need deployment secrets. → Explain the boundary and track
  secure deployment credentials as a separate capability rather than reusing
  source credentials unsafely.

## Migration Plan

1. Add interaction tests for guidance, dynamic requiredness, token username,
   keyboard tooltips, redaction, and 375 px layout.
2. Introduce shared guidance descriptors and update the dialog.
3. Update user documentation and run UI coverage/build plus Docker E2E.

The change is frontend-compatible with existing credential records and APIs.
Rollback restores the previous dialog; no data migration or backend rollback is
needed.

## Open Questions

None for this change. Secure deployment-secret storage remains an explicitly
separate product decision.

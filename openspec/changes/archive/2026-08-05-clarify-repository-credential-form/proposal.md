## Why

The repository credential dialog exposes security-sensitive fields without
explaining their purpose, whether they are required, or how they affect Git
authentication. This blocks first-time setup and can incorrectly suggest that
repository credentials are also injected into deployment tasks.

## What Changes

- Add concise helper text and accessible information tooltips for every
  repository credential field, including provider-specific examples where they
  improve understanding without changing provider-neutral behavior.
- Mark required and optional inputs consistently, with requirements changing
  dynamically for token, username/password, and SSH credential types.
- Explain that Display name is a Kestrix label, while Provider and Host are
  optional descriptive metadata and do not select an authentication driver or
  restrict where the credential can be used.
- Expose the backend-supported optional HTTPS username for token credentials so
  users can satisfy Git hosts that require a conventional username alongside a
  token.
- Explain that Known hosts is optional but recommended for SSH host-key pinning,
  and describe the existing trust-on-first-use behavior when it is omitted.
- Add an explicit scope notice: repository credentials authenticate Git source
  acquisition only and are never injected into task or deployment environments.
- Cover guidance, dynamic requiredness, keyboard/pointer access, mobile layout,
  and secret-safe rendering with component tests and documentation.

## Capabilities

### New Capabilities

- `repository-credential-form-guidance`: Defines understandable, accessible,
  type-aware guidance and scope disclosure for repository credential forms.

### Modified Capabilities

None.

## Impact

- Frontend credential dialog, shared form utilities, and responsive layout.
- Frontend component tests and enforced coverage.
- Repository credential and remote-project documentation.
- No database schema, encryption, credential API, or source-acquisition contract
  changes are required; token username is already accepted by the API.

## Non-goals

- Adding a general-purpose deployment secret store or injecting repository
  credentials into pipeline tasks.
- Automatically detecting a Git provider or validating credentials against a
  remote repository from the credential dialog.
- Changing encryption, rotation, revocation, or credential matching semantics.

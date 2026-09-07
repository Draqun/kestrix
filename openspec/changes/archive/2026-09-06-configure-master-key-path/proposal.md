## Why

Repository credentials depend on an encryption master key. Today, when no
`master_key_path` is configured, Kestrix falls back to the desktop OS keyring
through `secret-tool`. This makes server and container deployments fail only
when credentials are used, requires a desktop-specific dependency, and leaves
the key source ambiguous for operators.

## What Changes

- Introduce `KESTRIX_MASTER_KEY_PATH` as the single supported runtime setting
  for the credential-encryption master-key file.
- **BREAKING** Remove YAML `master_key_path` configuration and the automatic OS
  keyring/`secret-tool` fallback.
- Validate the configured key file before the HTTP server starts, including
  its existence, restrictive permissions, and accepted key encoding.
- Fail closed with an actionable error when the variable is unset or the file
  cannot be used; errors MUST NOT reveal key material.
- Update local and production deployment guidance to mount or provision the
  key file separately from PostgreSQL and set the environment variable.
- Preserve the existing encrypted database format and credential API; existing
  credentials remain readable when operators provide the same master key.

## Non-goals

- Adding a cloud KMS, Vault integration, key rotation, or automatic key
  generation/recovery.
- Storing the master key in PostgreSQL, project configuration, pipeline files,
  or repository-credential records.
- Changing supported Git credential types or remote-source acquisition.

## Capabilities

### New Capabilities

- `master-key-configuration`: Explicit, validated runtime configuration of the
  credential-encryption master key.

### Modified Capabilities

- `repository-credentials`: Encrypted credential storage uses only the
  explicitly configured file-backed master key.

## Impact

- Affected Go configuration, server startup, credential key-provider, and
  PostgreSQL-store wiring.
- Deployment manifests and documentation need a secure key-file mount and
  `KESTRIX_MASTER_KEY_PATH`.
- Operators using encrypted credentials must preserve their current key file
  and export its path through the environment before upgrading.

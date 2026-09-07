# Repository credentials

Kestrix stores repository credentials as encrypted PostgreSQL records. Pipeline
files and project records contain only a `credential_ref`; API reads return
metadata and never return secret fields.

Supported forms are provider-neutral:

- `token` for GitHub/GitLab personal or deploy tokens and generic HTTPS tokens;
- `basic` for username/password and Bitbucket app passwords;
- `ssh` for a private key and optional `known_hosts` content.

## Manage credentials in the UI

Open **Settings** and use the **Repository credentials** section to list,
create, rotate, or revoke credentials. Credential management is an
application-wide setting; it is intentionally not a separate navigation item
and is not embedded in the Projects page.

The **Type** field is a controlled selector that supports both pointer and
keyboard selection. Every field also has visible required/optional guidance
and a focusable information control with more detail.

| Field | Required | Purpose |
|-------|----------|---------|
| Display name | Yes | A recognizable, non-secret name shown in Settings and project selectors. |
| Type | Yes | Selects HTTPS token, username/app password, or SSH private-key authentication. |
| Provider | No | Searchable descriptive metadata, such as GitHub, GitLab, or Bitbucket. It does not select an authentication implementation. |
| Host | No | Searchable descriptive hostname. It does not validate or restrict use to that remote. |

The selected type adds these fields:

| Type | Field | Required | Purpose and example |
|------|-------|----------|---------------------|
| HTTPS token | HTTPS username | No | Conventional username expected by some hosts, for example `x-access-token` for GitHub or `oauth2` for GitLab. |
| HTTPS token | Token | Yes | Personal, deploy, or app token with repository read access. |
| Username / app password | Username | Yes | HTTPS account or deploy-token username. |
| Username / app password | Password | Yes | Password or app password with repository read access. |
| SSH private key | Private key | Yes | Complete PEM or OpenSSH private key with repository read access. |
| SSH private key | Known hosts | No | Recommended pinned `known_hosts` entries, for example `git.example.com ssh-ed25519 AAAA…`. |

For SSH, supplied known-hosts content enables strict verification against the
pinned keys. If it is omitted, Kestrix uses
`StrictHostKeyChecking=accept-new`: the first key is trusted and a later
changed key is rejected. Pinning is recommended over this trust-on-first-use
fallback.

Changing the type clears incompatible secret fields. Secret values are
write-only: after create or rotate, the UI refreshes and displays metadata
only. Revocation requires confirmation and prevents the credential from being
selected for new project configuration.

Repository credentials are exclusively for Git source acquisition: clone,
fetch, and ref resolution. They are never injected into task processes or
deployment environments. Runtime or deployment secrets need a separate secure
configuration mechanism; do not reuse repository credentials or place secrets
in repository URLs, pipeline HCL, or committed configuration.

Project create and edit dialogs contain a compact credential selector. Use
**Add credential** there to create a credential without losing entered project
fields; the returned reference is selected automatically. Use **Manage** to
open the full section in Settings. Select **Public access** to clear an
optional project credential.

## Manage credentials through the API

Create a credential with `POST /api/credentials`, use the returned `ref` in a
repository declaration, and rotate with `PUT /api/credentials/{ref}`. Revoke
with `POST /api/credentials/{ref}/revoke`.

The encryption master key is not stored in PostgreSQL. Every Kestrix process
requires `KESTRIX_MASTER_KEY_PATH` to name a mounted or otherwise provisioned
raw 32-byte or 64-character hex key file with mode `0600`; there is no OS
keyring or `secret-tool` fallback. For a host-run server, for example:

```bash
export KESTRIX_MASTER_KEY_PATH=/run/secrets/kestrix_master_key
```

For Compose deployments, set the same `KESTRIX_MASTER_KEY_PATH` to an absolute
host path. Compose mounts that file read-only at the same path inside the
container. Back up the master key securely before creating credentials. Losing
or replacing it makes existing ciphertext unrecoverable.

This key is separate from `KESTRIX_JWT_SIGNING_KEY_PATH`, which authenticates
API sessions rather than credential storage - see
[Authentication](authentication.md). Every endpoint in this document also now
requires a signed-in account with the `admin` role.

## Providers

The provider and host fields are descriptive metadata only. Git always uses
standard HTTPS or SSH transport, so GitHub, GitLab, Bitbucket, self-hosted Git,
and arbitrary Git servers use the same credential model.

## Migrating old remote declarations

Remote `ci_repo` blocks without both `url` and `ref` are now invalid. Remove
them or update them explicitly, for example:

```hcl
ci_repo "service" {
  url = "https://git.example.test/team/service.git"
  ref = "main"
  credential_ref = "cred_..."
}
```

URLs containing userinfo or passwords must be replaced with a clean URL and a
credential record. Existing environment-backed credential references are not
migrated automatically; create a database credential and update the
`credential_ref` declaration.

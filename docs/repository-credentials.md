# Repository credentials

Kestrix stores repository credentials as encrypted PostgreSQL records. Pipeline
files and project records contain only a `credential_ref`; API reads return
metadata and never return secret fields.

Supported forms are provider-neutral:

- `token` for GitHub/GitLab personal or deploy tokens and generic HTTPS tokens;
- `basic` for username/password and Bitbucket app passwords;
- `ssh` for a private key and optional `known_hosts` content;
- `secret` for a run-time value injected into tasks as a pipeline variable -
  see [Runtime secrets](#runtime-secrets) below. It is not a Git credential.

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
| Type | Yes | Selects HTTPS token, username/app password, SSH private-key authentication, or a runtime secret. |
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
| Runtime secret | Value | Yes | One opaque value injected into tasks as a pipeline variable, referenced from HCL with `secret_ref`. |

A value shorter than 8 characters is still accepted and injected, but is not
guaranteed to be redacted from run output; the form warns when the value is
under that length.

For SSH, supplied known-hosts content enables strict verification against the
pinned keys. If it is omitted, Kestrix uses
`StrictHostKeyChecking=accept-new`: the first key is trusted and a later
changed key is rejected. Pinning is recommended over this trust-on-first-use
fallback.

Changing the type clears incompatible secret fields. Secret values are
write-only: after create or rotate, the UI refreshes and displays metadata
only. Revocation requires confirmation and prevents the credential from being
selected for new project configuration.

Repository credentials (`token`, `basic`, `ssh`) are exclusively for Git
source acquisition: clone, fetch, and ref resolution. They are never injected
into task processes or deployment environments, and a `secret` credential is
never accepted as one - creating or updating a project with a `repo_credential_ref`
naming a `secret` credential fails with `400`. Runtime or deployment secrets
use the `secret` credential type and a pipeline's `secret_ref`; see
[Runtime secrets](#runtime-secrets) below.

Project create and edit dialogs contain a compact credential selector. Use
**Add credential** there to create a credential without losing entered project
fields; the returned reference is selected automatically. Use **Manage** to
open the full section in Settings. Select **Public access** to clear an
optional project credential. The selector lists Git credentials only - a
`secret` credential is never offered there.

## Runtime secrets

A `secret` credential holds one opaque value for use inside a task, not for
Git authentication. It goes through the same lifecycle, encryption, master
key, `admin`-only authorization, and Settings section as a Git credential -
create, rotate, list, and revoke all work the same way - but it is referenced
from a pipeline definition with `secret_ref` on a `variable` block, not from a
`ci_repo`'s `credential_ref`. See
[Pipeline Definition: Runtime secrets](pipeline-definition.md#runtime-secrets-secret_ref)
for resolution timing, masking, and redaction.

Type isolation runs in both directions: source acquisition refuses to use a
`secret` credential for Git, and secret-variable resolution refuses to inject
a Git credential (`token`, `basic`, `ssh`) as a runtime value. A credential's
type is bound into its ciphertext's authenticated data, so rewriting a stored
row's `credential_type` column does not reinterpret it as the other kind - it
simply fails to decrypt.

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

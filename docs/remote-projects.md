# Remote projects and source cache

Registered Kestrix projects use a remote Git repository as their only source.
Host-local `repo_path` registration is intentionally unsupported. The local
CLI can still run an explicitly supplied pipeline file for development.

## Register a public repository

The repository URL, requested ref, and pipeline directory are required. The
pipeline directory and file are relative to the acquired repository root.

```bash
curl -X POST http://localhost:8080/api/projects \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "my-app",
    "repo_url": "https://github.com/example/my-app.git",
    "repo_ref": "main",
    "pipeline_dir": "ci",
    "pipeline_file": "pipeline.tf"
  }'
```

Kestrix authenticates, refreshes the remote, resolves the ref to an immutable
commit, and parses all pipeline HCL before the project record is committed. A
failed fetch, missing ref, or invalid pipeline leaves an existing project
unchanged. Mutable refs never fall back to stale cached state.

## Register a private repository

In the UI, open **Settings → Repository credentials** to create and manage
write-only credentials before registering a project. You can also choose
**Add credential** inside a project create or edit dialog; entered repository
fields remain intact and the new credential is selected automatically. The
adjacent **Manage** action opens Settings for rotation and revocation.

GitHub, GitLab, Bitbucket, self-hosted services, and generic Git servers use
the same provider-neutral HTTPS/basic/SSH model. The type selector supports
pointer and keyboard operation and reveals only fields relevant to the
selected credential type.

For API-based registration, create the credential first:

```bash
curl -X POST http://localhost:8080/api/credentials \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "GitHub deploy token",
    "type": "token",
    "provider": "github",
    "host": "github.com",
    "username": "x-access-token",
    "token": "REDACTED"
  }'
```

Use the returned `ref` as `repo_credential_ref`. The project and run APIs
return only the reference and credential metadata, never the secret. Select
**Public access** in the project form, or submit an empty
`repo_credential_ref`, to
clear an optional credential explicitly. Do not place userinfo, tokens, or
passwords in `repo_url`.

The encryption master key is separate from PostgreSQL. Mount a mode-`0600`
32-byte raw or 64-character hex key and set `KESTRIX_MASTER_KEY_PATH` to that
file. Compose deployments use the same absolute path as the bind-mount source
and the path visible to Kestrix inside the container. Back it up separately;
losing it makes encrypted credentials unrecoverable.

## Pipeline repository semantics

A local `ci_repo.path` in HCL means a clean relative subdirectory of the
acquired primary project checkout. Absolute paths and `..` escapes are
rejected. It does not grant access to the Kestrix host filesystem.

```hcl
ci_repo "app" {
  path = "services/app"
}

ci_repo "shared" {
  url            = "ssh://git@example.test/team/shared.git"
  ref            = "v2.4.0"
  credential_ref = "cred_..."
}
```

Additional repositories require both `url` and `ref`. Every source is acquired
before any task starts. Runs record requested refs and resolved SHAs; manual
approval rematerializes those exact SHAs from the run snapshot even if a branch
or project configuration has changed.

## Git is the pipeline source of truth

Kestrix displays raw HCL, the parsed definition, and repository metadata, but
registered project pipelines are read-only. `POST
/api/projects/{name}/pipeline` returns `405 Method Not Allowed`. Edit, review,
commit, and push pipeline changes through the Git host, then refresh Kestrix.

## Configure the source cache

The cache stores private bare Git mirrors only. Every operation gets a separate
mode-`0700` detached checkout, which is deleted after use. Cache keys include a
canonical non-secret URL and credential reference; no credential payload or
writable task state is stored there.

```yaml
source_cache:
  directory: /var/lib/kestrix/cache/git
  max_age: 720h
  max_bytes: 10737418240
```

| Setting | Environment | CLI |
|---|---|---|
| `directory` | `KESTRIX_SOURCE_CACHE_DIR` | `--source-cache-dir` |
| `max_age` | `KESTRIX_SOURCE_CACHE_MAX_AGE` | `--source-cache-max-age` |
| `max_bytes` | `KESTRIX_SOURCE_CACHE_MAX_BYTES` | `--source-cache-max-bytes` |

Precedence is CLI, explicit config file, canonical environment, then default.
Local defaults are `~/.kestrix/cache/git`, 30 days, and 10 GiB. Production
Compose uses `/var/lib/kestrix/cache/git` on the persistent `source_cache`
volume.

Protect that volume like a private source-code checkout: restrict it to the
service account, use encrypted disks/backups where required, and never share it
between security tenants. Cache data is disposable and can be rebuilt; it is
not a replacement for the Git remote or database backup.

Maintenance removes unlocked/unpinned entries older than `max_age`, then uses
least-recently-used order until `max_bytes` is satisfied. Commits needed by
pending/manual runs are pinned across restarts and are eligible only after the
run becomes terminal or is canceled.

## Troubleshooting

- **Ref not found:** verify the explicit branch, tag, or commit and remote
  permissions. Kestrix will not execute an older cached branch value.
- **Credential revoked or unavailable:** rotate/reactivate the credential and
  retry. A cache hit still performs current credential resolution and a remote
  refresh.
- **Master key unavailable:** set `KESTRIX_MASTER_KEY_PATH` to the separately
  backed-up key file; ciphertext cannot be recovered from PostgreSQL alone.
- **Cache directory rejected:** ensure it is a writable directory for the
  service account. Kestrix enforces private permissions and positive age/size
  limits.
- **Disk remains above the limit:** inspect incomplete/manual runs and active
  acquisitions. Pinned or locked entries are deliberately retained.
- **Corrupt mirror:** Kestrix quarantines and rebuilds it once. A second failure
  is reported; fix remote access or filesystem health rather than using stale
  objects.

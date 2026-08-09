# Migrating registered projects to remote-only sources

This upgrade is intentionally breaking. The schema migration refuses to start
while any project has an empty `repo_url` or `repo_ref`; it reports the affected
project names and changes nothing. It never guesses a remote from `repo_path`.

Integrate and archive the prerequisite OpenSpec changes in this order before
deploying this change:

1. `remote-repository-execution`
2. `secure-remote-source-credentials`
3. `complete-remote-project-workflow`

## 1. Back up before changing rows

Stop Kestrix writers and create a PostgreSQL backup that includes projects,
runs, source metadata, and encrypted credentials. Back up the credential master
key separately.

```bash
pg_dump --format=custom --file=kestrix-before-remote-only.dump "$DATABASE_URL"
```

The Git object cache is disposable and need not be part of the database
backup, although private-source handling policy still applies to its volume.

## 2. Run the preflight query

Run this against the prerequisite schema before starting the new binary:

```sql
SELECT name, repo_path
FROM projects
WHERE btrim(COALESCE(repo_url, '')) = ''
   OR btrim(COALESCE(repo_ref, '')) = ''
ORDER BY name;
```

If it returns no rows, proceed to the upgrade. Otherwise choose an explicit
replacement or deletion for every listed project.

## 3. Replace or delete each local-only project

For a public remote, update the complete source identity before the upgrade:

```sql
UPDATE projects
SET repo_url = 'https://git.example.test/team/my-app.git',
    repo_ref = 'main',
    repo_credential_ref = NULL,
    pipeline_dir = 'ci',
    pipeline_file = 'pipeline.tf'
WHERE name = 'my-app';
```

For a private remote, first create and verify the encrypted database credential
through the prerequisite Kestrix version, then set its non-secret reference in
the update. Never copy a token into the URL or SQL migration script.

If a project must not be retained, delete it explicitly only after confirming
the database's foreign-key behavior and backup contents:

```sql
DELETE FROM projects WHERE name = 'obsolete-local-project';
```

Deletion may also remove project-owned history through cascading constraints.
Export required run history before deleting.

Re-run the preflight query and verify every replacement by cloning the URL/ref
with the intended credential and locating the configured pipeline directory.

## 4. Deploy and verify

Start the new binary. Its transaction removes `repo_path`, requires non-empty
`repo_url` and `repo_ref`, applies the credential reference constraint, and
rolls back the whole migration if any statement fails.

After startup:

1. list projects and verify URL/ref/credential metadata;
2. open each parsed and raw pipeline definition;
3. run a representative project and confirm its recorded source SHA;
4. approve a test manual task if used;
5. verify webhook processing at an immutable event SHA;
6. verify the persistent cache volume and retention settings.

## Rollback

If verification fails, stop the new binary before restoring. Restore the
database dump and separately restore the matching master key, then deploy the
previous prerequisite-compatible binary. Do not point an older binary at the
new remote-only schema. The cache volume can be discarded and rebuilt, but
must still be handled as private source code when removed.

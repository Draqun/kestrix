# Migrating project configuration to PostgreSQL

The JSON project store is no longer supported. Import only projects that have
an explicit remote URL and ref; local `repo_path` values cannot be carried into
the current remote-only schema. Keep the original JSON and a PostgreSQL backup
until every imported project has been validated.

For each old entry, first add or verify `repo_url`, `repo_ref`,
`pipeline_dir`, and `pipeline_file`. Create private repository credentials
through the credential API and record only the returned reference. Never place
a token in the JSON, CSV, SQL, or repository URL.

```bash
chmod 600 ~/.kestrix/projects.json

jq -e '
  to_entries | all(
    (.value.repo_url // "" | length) > 0 and
    (.value.repo_ref // "" | length) > 0
  )
' ~/.kestrix/projects.json

jq -r '
  to_entries[] |
  [.value.name, .value.repo_url, .value.repo_ref,
   (.value.repo_credential_ref // ""),
   (.value.pipeline_dir // "ci"),
   (.value.pipeline_file // "pipeline.tf"),
   (.value.gitlab_project_id // 0),
   (.value.default_branch // "main"),
   (.value.webhook_secret // ""),
   (.value.created_at // "")] | @csv
' ~/.kestrix/projects.json > /tmp/kestrix-projects.csv
chmod 600 /tmp/kestrix-projects.csv
```

Import into a staging table and then the remote-only project table:

```sql
CREATE TEMP TABLE imported_projects (
  name text,
  repo_url text,
  repo_ref text,
  repo_credential_ref text,
  pipeline_dir text,
  pipeline_file text,
  gitlab_project_id integer,
  default_branch text,
  webhook_secret text,
  created_at timestamptz
);

\copy imported_projects FROM '/tmp/kestrix-projects.csv' WITH (FORMAT csv)

INSERT INTO projects (
  name, repo_url, repo_ref, repo_credential_ref, pipeline_dir, pipeline_file,
  gitlab_project_id, default_branch, webhook_secret, created_at
)
SELECT name, repo_url, repo_ref, NULLIF(repo_credential_ref, ''), pipeline_dir,
       pipeline_file, gitlab_project_id, default_branch, webhook_secret,
       COALESCE(created_at, now())
FROM imported_projects
ON CONFLICT (name) DO UPDATE SET
  repo_url = EXCLUDED.repo_url,
  repo_ref = EXCLUDED.repo_ref,
  repo_credential_ref = EXCLUDED.repo_credential_ref,
  pipeline_dir = EXCLUDED.pipeline_dir,
  pipeline_file = EXCLUDED.pipeline_file,
  gitlab_project_id = EXCLUDED.gitlab_project_id,
  default_branch = EXCLUDED.default_branch,
  webhook_secret = EXCLUDED.webhook_secret;
```

Start Kestrix with the same `DATABASE_URL`, open each project definition, and
run a representative pipeline before deleting the temporary CSV. For upgrades
from the prerequisite PostgreSQL schema that still contains local-only rows,
follow [Migrating registered projects to remote-only sources](remote-only-projects.md).

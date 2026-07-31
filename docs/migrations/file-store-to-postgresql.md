# Migrating project configuration to PostgreSQL

The JSON project store is no longer supported by Kestrix. Before upgrading,
export the existing project file and import it into the configured PostgreSQL
database. Keep the original file as a backup until the imported projects have
been verified.

The old file contains a JSON object keyed by project name. The following
procedure imports the fields used by the PostgreSQL project table:

```bash
export DATABASE_URL='postgres://kestrix:***@localhost:5432/kestrix?sslmode=disable'
chmod 600 ~/.kestrix/projects.json

jq -r '
  to_entries[] |
  [.value.name, .value.repo_path, (.value.repo_url // ""),
   (.value.pipeline_dir // ""), (.value.pipeline_file // "pipeline.tf"),
   (.value.gitlab_project_id // 0), (.value.default_branch // "main"),
   (.value.webhook_secret // ""), (.value.created_at // "")] | @csv
' ~/.kestrix/projects.json > /tmp/kestrix-projects.csv

psql "$DATABASE_URL" <<'SQL'
CREATE TEMP TABLE imported_projects (
  name text, repo_path text, repo_url text, pipeline_dir text,
  pipeline_file text, gitlab_project_id integer, default_branch text,
  webhook_secret text, created_at timestamptz
);
\copy imported_projects FROM '/tmp/kestrix-projects.csv' WITH (FORMAT csv)
INSERT INTO projects (
  name, repo_path, repo_url, pipeline_dir, pipeline_file,
  gitlab_project_id, default_branch, webhook_secret, created_at
)
SELECT name, repo_path, repo_url, pipeline_dir, pipeline_file,
       gitlab_project_id, default_branch, webhook_secret, created_at
FROM imported_projects
ON CONFLICT (name) DO UPDATE SET
  repo_path = EXCLUDED.repo_path,
  repo_url = EXCLUDED.repo_url,
  pipeline_dir = EXCLUDED.pipeline_dir,
  pipeline_file = EXCLUDED.pipeline_file,
  gitlab_project_id = EXCLUDED.gitlab_project_id,
  default_branch = EXCLUDED.default_branch,
  webhook_secret = EXCLUDED.webhook_secret;
SQL
```

Start Kestrix with the same `DATABASE_URL`, verify the project list and run a
pipeline, then remove the temporary CSV. Do not commit either export file.

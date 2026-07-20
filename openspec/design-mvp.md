# Design: Kestrix MVP — Deploy Pipeline

## Architecture

```
                                    MVP critical path (bold)
┌─────────────┐   push    ┌──────────────────┐   API    ┌───────────┐
│  Developer   │ ────────▶│   GitLab          │ ───────▶│  Runner   │
│  (git push)  │          │   (repo + CI)     │◀─────── │  (exec)   │
└─────────────┘          └────────┬───────────┘  logs    └───────────┘
                                  │ webhook                        │
                                  ▼                                │
                         ┌──────────────────┐                     │
                         │   Kestrix Server   │                     │
                         │                    │                     │
                         │  ┌──────────────┐ │   ┌─────────────┐  │
                         │  │ Webhook       │ │   │ Dispatch    │◀─┘
                         │  │ Receiver      │─┼──▶│ (GitLab API)│
                         │  └──────────────┘ │   └─────────────┘
                         │  ┌──────────────┐ │         │
                         │  │ HCL Parser    │ │   ┌─────▼──────────┐
                         │  └──────┬───────┘ │   │ gitlab-ci.yml  │
                         │         │          │   │ (generated)    │
                         │  ┌──────▼───────┐ │   └────────────────┘
                         │  │ DAG Engine    │ │
                         │  └──────┬───────┘ │
                         │         │          │
                         │  ┌──────▼───────┐ │
                         │  │ CI Generator  │ │
                         │  └──────────────┘ │
                         │                    │
                         │  ┌──────────────┐ │
                         │  │ File Store    │ │  (projects.json)
                         │  └──────────────┘ │
                         └──────────────────┘
```

## Critical Path (Webhook → Deploy)

```
1. GitLab sends POST /webhook with push payload
2. Webhook receiver:
   a. Verify signature (X-Gitlab-Token or HMAC)
   b. Parse PushEvent (repo, branch, files_changed)
   c. Look up project by repo_path / GitLab project ID
   d. Load pipeline.tf from project's configured path
3. HCL Parser → model.Project
4. DAG Engine → Resolve(changedRepos) → []Node (triggered tasks)
5. CI Generator → gitlab-ci.yml content
6. Dispatch:
   a. POST /projects/:id/pipeline with generated YAML
      OR
   b. Update .gitlab-ci.yml on branch via GitLab Files API + trigger pipeline
7. Return { status, triggered_tasks, pipeline_url }
```

### Dispatch Strategy (two options, MVP uses option A)

**Option A: Direct pipeline trigger (simpler)**
- Generate `gitlab-ci.yml` content
- POST to GitLab Pipeline API with `ref` and `variables`
- GitLab uses the existing `.gitlab-ci.yml` in repo + our trigger variables
- Pro: No file push needed, fast
- Con: Relies on repo having a base `.gitlab-ci.yml`

**Option B: File push + auto-trigger (more flexible)**
- Generate `gitlab-ci.yml` content
- PUT to GitLab Files API (`/projects/:id/repository/files/.gitlab-ci.yml`)
- Commit the generated file to the repo
- GitLab auto-triggers pipeline on push
- Pro: Full control over pipeline content, no pre-existing CI file needed
- Con: Creates commits in user repo, requires write access

**MVP uses Option A** (direct trigger) for simplicity. Option B is a follow-up for multi-environment support.

## Component Design

### 1. Project Store (`internal/store/filestore.go`)

```go
type FileStore struct {
    path string // ~/.metaci/projects.json
}

func (s *FileStore) Load() (map[string]*ProjectConfig, error)
func (s *FileStore) Save(projects map[string]*ProjectConfig) error
func (s *FileStore) Get(name string) (*ProjectConfig, error)
func (s *FileStore) Upsert(cfg *ProjectConfig) error
func (s *FileStore) Delete(name string) error
```

Atomic writes: write to temp file, then rename. No locking needed for single-process PoC.

### 2. Dispatch Client (`internal/dispatch/gitlab.go`)

```go
type GitLabClient struct {
    baseURL    string
    token      string
    httpClient *http.Client
}

// TriggerPipeline triggers a GitLab pipeline via API
func (c *GitLabClient) TriggerPipeline(projectID int, ref string, variables map[string]string) (*Pipeline, error)

// GetPipelineStatus returns pipeline status
func (c *GitLabClient) GetPipelineStatus(projectID int, pipelineID int) (*Pipeline, error)
```

GitLab API endpoints used:
- `POST /api/v4/projects/:id/pipeline` — trigger pipeline
- `GET /api/v4/projects/:id/pipelines/:pipeline_id` — check status
- `POST /api/v4/projects/:id/variables` — set trigger variables (optional)

### 3. Webhook Handler (modified `internal/web/webhook.go`)

```go
func (s *Server) handleWebhook(w http.ResponseWriter, r *http.Request) {
    // 1. Parse payload
    // 2. Verify signature
    // 3. Look up project (by repo path or GitLab project ID)
    // 4. Parse pipeline.tf
    // 5. Resolve triggers
    // 6. Generate CI config
    // 7. Dispatch to GitLab (if configured)
    // 8. Log result
    // 9. Return response
}
```

### 4. Config File (`~/.metaci/config.yaml`)

```yaml
server:
  addr: ":8080"
  pipeline_dir: "./examples"

gitlab:
  url: "https://gitlab.com"
  token: "${METACI_GITLAB_TOKEN}"

webhook:
  default_secret: "${METACI_WEBHOOK_SECRET}"
  method: "gitlab"  # or "hmac"

projects:
  - name: "demo-mono"
    repo_path: "/path/to/repo"
    gitlab_project_id: 12345
    default_branch: "main"
    webhook_secret: "optional-per-project-secret"
```

## Security Considerations

- **Per-project webhook secrets**: Each project can have its own HMAC secret. Fallback to global `METACI_WEBHOOK_SECRET`.
- **GitLab token scope**: Needs `api` scope (pipeline trigger + variable management). Stored in env var, never in config file or database.
- **No secrets in pipeline definitions**: Variables marked `masked=true` are injected at runtime, not stored in HCL.
- **Generated CI config is ephemeral**: The `gitlab-ci.yml` is sent via API, not committed to repo (Option A). No trace in git history.

## Migration Path

MVP builds on existing PoC without breaking changes:
- `Server.projects` (in-memory) → `FileStore` (persistent) — drop-in replacement
- `dispatch.Client` → `dispatch.GitLabClient` — extended interface
- Webhook handler gains post-resolve dispatch step
- All existing tests continue to pass

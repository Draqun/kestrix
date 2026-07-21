## 1. CI Generator (`specs/ci-generator`)

- [x] REQ-1: Generate gitlab-ci.yml from pipeline definition
- [x] REQ-2: Stages derived from task dependencies
- [x] REQ-3: Task-level script blocks
- [x] REQ-4: Image configuration override
- [x] REQ-5: Needs section ordering
- [x] REQ-6: Trigger rules from task triggers
- [x] REQ-7: Docs — `docs/ci-generator.md` does not exist

## 2. DAG Engine (`specs/dag-engine`)

- [x] REQ-1: Boolean trigger expressions (AND/OR/NOT/parentheses)
- [x] REQ-2: Cycle detection
- [x] REQ-3: Unknown reference validation
- [x] REQ-4: Manual task support
- [x] REQ-5: Documentation (`docs/dag-engine.md` exists)

## 3. Pipeline Definition (`specs/pipeline-definition`)

- [x] REQ-1: Project block parsing
- [x] REQ-2: CI repo definitions
- [x] REQ-3: CI task definitions
- [x] REQ-4: Trigger expressions referencing repos
- [x] REQ-5: Cross-repo dependency validation
- [x] REQ-6: Docs — `docs/pipeline-definition.md` does not exist

## 4. Dispatch Pipeline (`specs/dispatch-pipeline`)

- [x] REQ-1: Webhook → dispatch integration
- [x] REQ-2: GitLab API pipeline trigger
- [x] REQ-3: Pipeline variables injection (`METACI_*`)
- [x] REQ-4: Dispatch response shape (status, pipeline_url, etc.)
- [x] REQ-5: Dry-run mode

## 5. Project Registration (`specs/project-registration`)

- [x] REQ-1: GitLab binding fields (project_id, branch, secret)
- [x] REQ-2: Persistence (FileStore atomic save/load)
- [x] REQ-3: Lookup by GitLab project ID in webhook
- [x] REQ-4: Auto-create project from `--dir` on server start
- [x] REQ-5: API CRUD endpoints (POST/GET/PUT/DELETE)

## 6. Webhook Receiver (`specs/webhook-receiver`)

- [x] REQ-1: Parse GitLab push events
- [x] REQ-2: File classifier (code vs docs)
- [x] REQ-3: Repo matching from paths
- [x] REQ-4: Trigger execution from webhook
- [x] REQ-5: Response shape (project, tasks, status)
- [x] REQ-6: Docs — `docs/webhook-receiver.md` does not exist

## 7. Binary Identity (`specs/binary-identity`)

- [x] REQ-1: Binary named `kestrix`
- [x] REQ-2: CLI help and logs say "Kestrix"
- [x] REQ-3: Swagger title "Kestrix API"

## 8. Business Website (`specs/business-website`)

- [ ] REQ-1: Business website build step
- [ ] REQ-2: Content pages specific to business
- [ ] REQ-3: Deployment pipeline for business site

## 9. Project Docs (`specs/project-docs`)

- [x] REQ-1: Documentation site generation (`build-docs.sh` → `ui/dist/docs`)
- [x] REQ-2: Comprehensive project docs (`docs/` has all component files)

## Context

See proposal.md - Why. The constraints that shape this design:

- `internal/web/handler_webhook.go:88-99` is the only project lookup on the
  webhook path: `FindByGitLabID(event.GitLabProjectID)`, then a single
  `projCfg`. Lines 140-260 resolve one DAG, dispatch once, record one history
  row, and answer one JSON document. Everything below the lookup assumes
  exactly one project.
- `internal/web/webhook.go:29-33` decodes only `project.id`,
  `path_with_namespace`, and `default_branch`. The GitLab push payload also
  carries `project.git_http_url`, `project.git_ssh_url`, `project.web_url`,
  and `repository.url`; none is read today.
- `handleWebhook` loads the pipeline through
  `loadProjectPipeline(ctx, projCfg, event.CheckoutSHA, "webhook")`
  (`handler_project.go:250`), which opens the **primary** source at the given
  revision (`project_source.go:78-82`, `Primary: true`). Passing a SHA from a
  different repository fails the ref resolution; today that path is
  unreachable because only the primary repository can match.
- `webhook.go:150` `MatchRepos` already has the rule this change needs for a
  secondary source: a repo with a `url` and no `path` is "changed as a whole
  when any code file changed". What is missing is *restricting* the changed
  set to the one matching repo instead of evaluating every repo against the
  push's files.
- `internal/source/cache.go:94` `canonicalSourceURL` lowercases scheme and
  host, rejects embedded credentials, and strips a trailing slash. It keeps
  the scheme, so `https://h/a.git` and `git@h:a.git` produce different
  strings. The cache needs that distinction (different transports, different
  mirrors); routing does not.
- Schema is the `CREATE TABLE IF NOT EXISTS` block plus the idempotent
  `statements` slice in `internal/store/pgstore.go:60-150`. No migration tool.
- `domain.ProjectStore` (`ports.go:119-127`) is the seam every project lookup
  goes through; `internal/mocks` and `portscheck` must follow any change.
- `webhook-ingress-hardening` (sibling change) moves verification from the
  global `s.cfg.Signature.Secret` to a per-project secret and fails closed.
  This design assumes that lands first: once a delivery can match several
  projects, "which secret" is answered per matched project.

## Goals / Non-Goals

**Goals:**

- A push to any repository a pipeline declares reaches every project that
  declares it, with no new registration field to fill in.
- Routing data is a projection of data Kestrix already owns (project row +
  parsed HCL), so it can never disagree with the pipeline for long.
- Existing single-repository registrations behave exactly as before.

**Non-Goals:**

- Executing anything from the webhook. It resolves, records, dispatches.
- Provider-specific identifiers for secondary sources (`external_id` per
  provider). URL is the one identifier every Git host puts in its payload.
- A general `sources` write API. Derived data has no write API.

## Architecture

```
  POST /webhook ──▶ verify ──▶ ParseGitLabPushEvent
                                  │  RepoURLs, GitLabProjectID, CheckoutSHA, files
                                  ▼
                       matchKey(url) for each RepoURL
                                  │
                    ┌─────────────▼─────────────┐
                    │ ProjectStore              │
                    │  FindBySourceKey(keys)    │──▶ project_sources
                    │    [] ? FindByGitLabID(id)│──▶ projects.gitlab_project_id
                    └─────────────┬─────────────┘
                                  │ 0..n (project, source)
                 ┌────────────────┼────────────────┐
                 ▼                ▼                 ▼
          resolveOne()     resolveOne()      ignored → 1 history row
            │ primary?  yes ─▶ load pipeline @ event SHA, MatchRepos(files)
            │           no  ─▶ load pipeline @ repo_ref, changed = {source.Name}
            │                   (if HasCodeChanges)
            ├─▶ dag.Resolve ─▶ dispatch(gitlab_project_id) ─▶ history row
            └─▶ result{project, source_name, status, tasks, dispatch}
                                  │
                       response {status, results: [...]}

  writes that refresh sources:
    POST/PUT /api/projects/{name} ─┐
    GET .../definition (pipeline)  ─┼─▶ deriveSources(cfg, proj) ─▶ ReplaceSources
    webhook primary load           ─┘        primary + ci_repo{url}
```

## Decisions

### D1. Sources are a projection, replaced whole, never edited

`project_sources (project_name, match_key, source_name, repo_url, is_primary)`
with `PRIMARY KEY (project_name, match_key)`. `ReplaceSources(name, []Source)`
deletes and re-inserts inside one transaction. It runs after a successful
`CreateValidated`/`UpdateValidated` (the pipeline was just parsed there
anyway), after `loadProjectPipeline` on the definition route, and after the
primary-source load in the webhook path.

Alternative considered: a `sources` array on the registration payload,
maintained by the operator. Rejected - it would drift from the HCL the moment
someone adds a `ci_repo`, and the drift would be silent (a push that just
never triggers). Deriving from the parse means the same file that declares
the dependency also wires the webhook.

Alternative considered: no table, scan every project's cached pipeline on
each delivery. Rejected - it turns a webhook into N Git operations, and the
cache is not guaranteed to hold a pipeline for a project nobody has opened.

Consequence: a project registered before this change has no secondary rows
until something loads its pipeline. Startup backfills only the primary row
(D5); that keeps startup free of Git operations.

### D2. Match on a transport-neutral key, keep the cache's canonical URL as is

`sourceMatchKey(raw)` = lowercase `host + "/" + path`, with the scheme, an
SSH user (`git@`), the `.git` suffix, and the trailing slash removed;
scp-style `git@host:group/repo.git` is rewritten to `host/group/repo`. It
reuses `canonicalSourceURL` for the credential rejection and parsing, and
sits next to it in `internal/source`.

`canonicalSourceURL` is **not** changed: the cache key must keep transports
apart because the credential and the mirror differ per transport. Routing
must merge them because GitLab's payload lists both `git_http_url` and
`git_ssh_url`, and an operator registers whichever they clone with.

Alternative considered: match on `project.id` per provider (`provider,
external_id`). Rejected for now - the payload ID is provider-specific, a
`ci_repo` block has no place to declare it, and URL already identifies the
repository on every host. The `gitlab_project_id` fallback remains for
registrations that predate `repo_url`.

### D3. Secondary push: primary pipeline at `repo_ref`, event SHA recorded

The pipeline is the primary repository's file. A secondary push's SHA cannot
be checked out there, so `loadProjectPipeline` is called with an empty
revision, which resolves the configured `repo_ref` head (existing behaviour
of `openProjectSource`). The event SHA goes into the history row and into the
dispatch variables (`KESTRIX_SOURCE_NAME`, `KESTRIX_SOURCE_REVISION`), so a
GitLab job can pin the secondary checkout if it wants to.

Alternative considered: load the pipeline at the primary's SHA that was
current when the secondary SHA was pushed. Rejected - there is no such
relation between two repositories; the configured ref is the only honest
answer and it is what a manual run uses too.

The changed set for a secondary delivery is `{source.Name}` if
`HasCodeChanges(files)`, else empty - not `MatchRepos` over all repos. Running
`MatchRepos` would test the secondary repo's file paths against the primary
repo's subpath prefixes and produce false positives when directory names
coincide.

### D4. Fan-out is a loop over `(project, source)` pairs, not a new pipeline

The body of `handleWebhook` from the lookup down becomes `resolveOne(ctx,
event, projCfg, source) webhookResult`, called once per match. Each call
records its own history row and its own dispatch. The response becomes
`{status, results: [...]}` where `results` has one entry per project; the
single-project case is `results` of length one. `status` is `ok` when at
least one project resolved, `ignored` when none matched, `error` when every
matched project failed.

Order of processing is deterministic (project name ascending) so a test can
assert on positions. Failures are isolated: an error in one `resolveOne`
becomes that entry's `status: error`, never an early return.

Alternative considered: keep a single-result response and pick the first
match. Rejected - silently dropping the second project is exactly the class
of bug this change exists to remove.

Compatibility: the top-level `status`, `dispatched`, `pipeline_id`, and
`pipeline_url` keys are kept for the single-match case so the existing e2e
tests and any external caller that reads them keep working.

### D5. Startup backfill is one SQL statement

`INSERT INTO project_sources (project_name, match_key, source_name, repo_url,
is_primary) SELECT name, <key>, name, repo_url, true FROM projects ON CONFLICT
DO NOTHING` needs the match key computed in Go, so the backfill is a small
loop in `Migrate` over `Load()` that calls `ReplaceSources` only for projects
with **no** rows. It never touches Git. Secondary rows appear on first pipeline
load (D1).

### D6. Ordering against `webhook-ingress-hardening`

`webhook-ingress-hardening` is implemented first (see `openspec/roadmap.md`).
It reads the capped body once, probes the minimal `{"project":{"id":N}}`
fields, and calls a pure `verifyDelivery(method, headers, body,
projectSecret, globalSecret)` for the one project it resolved. This change
keeps that function and moves the call: the probe also reads the repository
URLs, `resolveOne` runs once per matched project, and each call verifies the
delivery against that project's `webhook_secret` before doing anything else.

The rules from the hardening change hold per match: a project whose secret
does not verify is skipped without a history row (unverified deliveries never
write rows), the other matches proceed, and a delivery that no matched project
and no global secret accepts answers `401`. When no project matches at all,
only the global secret can accept the delivery, and it is then recorded as
`ignored` exactly as today.

## Security considerations

- URL matching uses the same embedded-credential rejection as the cache, so a
  payload that smuggles `https://user:token@host/...` is neither matched nor
  stored nor logged.
- Fan-out does not widen what a delivery can do: each matched project is
  resolved with its own credential reference and dispatched to its own
  `gitlab_project_id`. A secondary repository cannot make a project dispatch
  anywhere it did not already dispatch.
- A shared repository declared by many projects lets one push trigger many
  pipelines. That is the feature. The per-project secret (D6) is what stops
  an attacker who can forge one project's delivery from reaching the others.
- `project_sources` stores repository URLs already present in `projects` and
  in pipeline HCL; nothing new becomes readable to `viewer`.
- History rows: the number of rows per delivery grows with the fan-out
  factor. Retention is by row count, so a busy shared repository evicts
  history faster; noted in docs, no change to the limit.

## Risks / Trade-offs

- [Stale secondary sources until first refresh after upgrade] → documented;
  `GET /api/projects/{name}` triggers the refresh, and the release note tells
  operators to open each project once.
- [Two projects register the same primary repository] → both match, both
  resolve; identical to today's behaviour if they shared a `gitlab_project_id`,
  and the response now shows both instead of the first.
- [Webhook response shape changes for multi-match] → single-match keeps the
  old top-level keys; only callers that parse `results` see the new field.
- [Match key collisions across hosts with the same path] → the key includes
  the host; only a literal same-host-same-path is treated as the same
  repository, which it is.
- [Test surface] → `internal/web` is at 100%; the fan-out loop, the ignored
  path, and the per-entry error path each need a table-driven case.

## Migration Plan

1. Deploy `webhook-ingress-hardening` first (D6).
2. Deploy this change. Startup creates `project_sources`, backfills primary
   rows, and adds `webhook_events.source_name`.
3. Open each project's definition once (UI or `GET
   /api/projects/{name}/definition`) to derive secondary sources.
4. Add a webhook on each secondary repository in the Git host pointing at the
   same `/webhook` URL and the project's secret.

Rollback: the previous binary ignores `project_sources` and `source_name`;
both can stay. No data is rewritten.

## Open Questions

- Should the Projects page offer a "refresh sources" button rather than
  relying on the definition view? Deferrable: the definition view already
  performs the refresh, and the button would call the same route.

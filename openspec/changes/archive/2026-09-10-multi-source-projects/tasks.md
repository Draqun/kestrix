## 1. Match key and payload URLs

- [x] 1.1 Add `SourceMatchKey(raw string) (string, error)` in `internal/source` beside `canonicalSourceURL` (`cache.go:94`), and verify unit tests cover `https://H/a/b.git`, `HTTPS://H/a/b/`, `ssh://git@H/a/b.git`, `git@H:a/b.git` all yielding `h/a/b`, a different path not matching, and a URL with userinfo returning an error.
- [x] 1.2 Extend `GitLabProject` in `internal/web/webhook.go:29` with `git_http_url`, `git_ssh_url`, and `web_url`, add `RepoURLs []string` to `PushEvent`, and verify `ParseGitLabPushEvent` tests assert the three URLs are collected in order, empty ones are dropped, and a payload without them still parses.

## 2. Storage

- [x] 2.1 Add `project_sources (project_name TEXT REFERENCES projects(name) ON DELETE CASCADE, match_key TEXT, source_name TEXT, repo_url TEXT, is_primary BOOLEAN, PRIMARY KEY (project_name, match_key))` plus `CREATE INDEX IF NOT EXISTS idx_project_sources_key ON project_sources(match_key)` to the schema block in `internal/store/pgstore.go`, and add `ALTER TABLE webhook_events ADD COLUMN IF NOT EXISTS source_name TEXT NOT NULL DEFAULT ''` to the `statements` slice; verify the integration test applies both to a fresh database and to one created by the current schema.
- [x] 2.2 Add `domain.ProjectSource{Name, URL, Primary}` and `Sources []ProjectSource \`json:"sources,omitempty"\`` on `ProjectConfig`, extend `ProjectStore` with `ReplaceSources(name string, sources []ProjectSource) error` and `FindBySourceKey(keys []string) ([]*ProjectConfig, error)`, update `internal/mocks` and `portscheck`; verify `go test ./internal/domain/... ./internal/mocks/...` passes.
- [x] 2.3 Implement both methods in `internal/store/pgstore_project.go` (`ReplaceSources` as delete+insert in one transaction, `FindBySourceKey` returning distinct projects ordered by name), and make `Get` populate `Sources` with the primary first; verify integration tests cover replace-all removing a dropped row, two projects sharing one key both returned, a duplicate key within one project collapsing, and `Get` ordering.
- [x] 2.4 Add `domain.WebhookEvent.SourceName` and persist/read it in `internal/store/pgstore_webhook.go`; verify the webhook integration test round-trips the field and `GET /api/logs` items include `source_name`.

## 3. Deriving sources

- [x] 3.1 Add `deriveSources(cfg *domain.ProjectConfig, proj model.Project) []domain.ProjectSource` in `internal/service` (primary from `cfg.RepoURL`, one per `ci_repo` with a `url`, deduplicated by match key, primary first), and verify unit tests cover no remote repos, two remote repos, a path-only repo excluded, and a duplicate URL collapsing.
- [x] 3.2 Call `ReplaceSources` after a successful `CreateValidated` and `UpdateValidated` in `internal/service/service.go:65-125`, passing the parsed project the validator already produced, and verify service tests assert the store receives the derived set and that a failed validation leaves sources untouched.
- [x] 3.3 Call `ReplaceSources` after `loadProjectPipeline` succeeds in `handleGetProjectDefinition` (`internal/web/handler_project.go:303`) and in the webhook primary-load path, and verify a handler test asserts a pipeline with a new remote `ci_repo` adds a source on the next definition load.
  - Superseded in part: a primary delivery no longer refreshes sources, because its immutable SHA need not be the head of the configured ref. Only the definition route and secondary deliveries refresh on the webhook path; see design.md D1.
- [x] 3.4 Ignore a client-supplied `sources` field in `decodeProjectConfig` (`handler_project.go:28`), and verify a test posts a payload with `sources` and asserts the stored set is the derived one.
- [x] 3.5 Backfill in `PGStore.Migrate`: for every project with zero `project_sources` rows, insert the primary row; verify an integration test creates a project row without sources, runs `Migrate`, and finds exactly one primary row, and that a second `Migrate` changes nothing.

## 4. Webhook routing and fan-out

- [x] 4.1 Extract the body of `handleWebhook` below the lookup (`internal/web/handler_webhook.go:110-260`) into `resolveOne(ctx, event *PushEvent, cfg *domain.ProjectConfig, source domain.ProjectSource) webhookResult` that loads the pipeline, computes the changed set, resolves the DAG, dispatches, and records one history row with `SourceName`; verify existing single-project webhook tests still pass unchanged.
- [x] 4.2 Replace the lookup with: match keys from `event.RepoURLs` → `FindBySourceKey`; if empty and `GitLabProjectID > 0` → `FindByGitLabID` treated as a primary match; if still empty → the existing `ignored` response and history row. Verify tests cover URL match, ID fallback, URL precedence over a conflicting ID, and nothing matching.
- [x] 4.3 In `resolveOne`, for a primary match load at `event.CheckoutSHA` and use `MatchRepos`; for a secondary match load with revision `""` and set changed to `{source.Name}` only when `HasCodeChanges`. Verify tests cover a secondary push touching `lib/util.go` (changed exactly `[shared]`, primary subpath repo `lib` not changed), a secondary push touching only `README.md` (status `skipped`), and that the primary source was never asked for the secondary SHA.
- [x] 4.4 Add `KESTRIX_SOURCE_NAME` and `KESTRIX_SOURCE_REVISION` to the dispatch variables, and verify the dispatch e2e test asserts them for both a primary and a secondary delivery.
- [x] 4.5 Build the response as `{status, results: [...]}` with the top-level `project`, `changed`, `tasks`, `count`, `dispatched`, `pipeline_id`, `pipeline_url` kept for a single match; verify tests assert the single-match document is byte-compatible with the previous one and a two-project match has two `results` in project-name order.
- [x] 4.6 Make a `resolveOne` failure produce a `status: error` entry and an `error` history row without stopping other matches; verify a test with two matched projects where one pipeline fails to parse asserts both history rows and both result entries.
- [x] 4.7 Call `verifyDelivery` (from `webhook-ingress-hardening`) at the top of `resolveOne` with the matched project's `webhook_secret` (design D6), and verify tests cover: one of two matched projects failing verification is skipped with no history row while the other proceeds; no match verifying answers `401`; no match at all falls back to the global secret and records `ignored`.

## 5. API and UI

- [x] 5.1 Verify `GET /api/projects/{name}` returns `sources` with the primary first and no credential fields, and that `GET /api/projects` still passes its existing pagination tests with the field omitted.
- [x] 5.2 Extend `normalizeProject` in `ui/src/api/client.js` for `sources`, render a read-only "Sources" list in the project detail of `ui/src/pages/Projects.jsx` (name, URL, primary chip), and verify a component test renders two sources and marks the primary.
- [x] 5.3 Show `source_name` in the Webhook Logs table when it differs from the project name; verify the `WebhookLogs` component test covers a secondary-source row.

## 6. Documentation and rollout

- [x] 6.1 Update `docs/webhook-receiver.md` (project resolution, fan-out, secondary-push semantics, `source_name`, new dispatch variables), `docs/remote-projects.md` (sources section and the "open each project once after upgrade" note), and `docs/pipeline-definition.md` (`ci_repo url` now also wires the webhook); verify `make ui-build` copies them.
- [x] 6.2 Add a CHANGELOG entry covering the new table, the backfill, the multi-result response, and the requirement to register a webhook on each secondary repository; verify it names `webhook-ingress-hardening` as the prerequisite.
- [x] 6.3 Add a Docker E2E case in `scripts/docker-deployment-e2e.sh` that registers a project whose pipeline declares a second bare remote, pushes to the second remote, and asserts the delivery resolves the project with `source_name` set; verify with `bash -n` and, where Docker is available, a full run.

## 7. Verification

- [x] 7.1 Run `make quality` and verify `internal/web` and `internal/service` stay at their 100% thresholds.
- [x] 7.2 Run the integration suite against a database created by the previous binary and verify the backfill and the new column apply without data loss.
- [x] 7.3 Run `npx -y @fission-ai/openspec@latest validate multi-source-projects --strict` and verify it passes.

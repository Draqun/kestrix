## Why

Kestrix promises a project that sits above its repositories, but the webhook
receiver can only hear one of them. `internal/web/handler_webhook.go:88-99`
resolves a delivery by `projects.gitlab_project_id` alone
(`FindByGitLabID`, `internal/store/pgstore_project.go:72`). A push to a
repository declared as `ci_repo "billing" { url = ... }` never reaches the
project, so `repo.billing.changed` can only ever be true from a manual run.
Cross-repo triggers - the reason the product exists - do not fire from Git.

## What Changes

- Every project gains a derived, read-only set of **sources**: its primary
  repository plus every remote `ci_repo` in its parsed pipeline. Sources are
  recomputed whenever the project is created, updated, or its pipeline is
  refreshed; they are never entered by hand.
- `POST /webhook` resolves the target project(s) by matching the repository
  URLs in the payload against those sources. The `gitlab_project_id` lookup
  stays as a fallback, so existing registrations keep working unchanged.
- One repository may be a source of several projects. A delivery then fans
  out: each matched project is resolved and recorded separately.
- A push to a secondary source marks that `ci_repo` changed as a whole, and
  the pipeline definition is loaded from the **primary** source at its
  configured ref - the event SHA belongs to a different repository and is
  recorded, not checked out as the pipeline.
- `GET /api/projects/{name}` returns `sources`; the Projects page lists them.
  Webhook history records which source a delivery matched.

## Non-goals

- Changing where a pipeline is dispatched. Dispatch stays bound to the
  project's single `gitlab_project_id`.
- Starting a Kestrix run from a webhook, or pinning a secondary repository to
  the event SHA inside a run. The webhook only resolves, records, and
  dispatches today; that stays.
- Hand-managed sources, per-source webhook secrets, or per-source
  credentials. A secondary source is authenticated with the `credential_ref`
  its `ci_repo` already declares.
- GitHub or Bitbucket payload formats. The URL match is provider-neutral, but
  only the GitLab push payload is parsed.
- Redesigning webhook verification. `webhook-ingress-hardening` makes
  verification per-project and fail-closed; this change must land after it
  and use each matched project's secret during fan-out.

## Capabilities

### New Capabilities

- `project-sources`: the derived source set of a project - how it is
  computed, when it is refreshed, how it is exposed, and how a repository URL
  is matched to it.

### Modified Capabilities

- `webhook-receiver`: "Push Event Parsing" extracts the repository URLs;
  "Pipeline Loading" loads from the primary source at its configured ref when
  the event came from a secondary source, and the receiver may resolve more
  than one project per delivery.
- `project-registration`: REQ-3 lookup is by source URL first, GitLab project
  ID second.

## Impact

- `internal/store/pgstore.go`: new `project_sources` table in the idempotent
  schema block; `pgstore_project.go`: replace-all upsert and URL lookup.
- `internal/domain/ports.go`: `ProjectStore` gains `ReplaceSources` and
  `FindBySourceURL`; `ProjectConfig` gains `Sources`.
- `internal/web/webhook.go`: `PushEvent.RepoURLs`; `handler_webhook.go`: loop
  over matched projects; `handler_project.go`: refresh sources after
  create/update and on definition load.
- `internal/source/cache.go:94` `canonicalSourceURL` is reused; a new
  scheme-agnostic match key sits beside it.
- `webhook_events` gains a `source_name` column; `GET /api/logs` items carry it.
- UI: `Projects.jsx` detail view lists sources. No request payload changes.
- `scripts/test.sh` keeps `internal/web` and `internal/service` at 100%.

## Why

Kestrix accepts pushes only from GitLab. `POST /webhook` probes and parses
the body as a GitLab push event (`probeWebhook`, `ParseGitLabPushEvent`,
`internal/web/`). A GitHub push fails verification under the default
`gitlab` method (401, no `X-Gitlab-Token`). Under `hmac` it verifies against
the global secret only, because the probe finds no GitLab `project` to
match. It then fails parsing (400, no `object_kind`).

The signature side is half there: the `hmac` method already verifies
`X-Hub-Signature-256` exactly as GitHub signs. The UI setup panel even
mentions it. But the method is instance-wide, so one
instance cannot take signed GitHub deliveries and token-authenticated GitLab
ones together.

If the go-to-market targets GitHub users, this is a sales blocker (the wiki
lists it as the first deferred decision). Repository credentials already
cover GitHub (`token` with username `x-access-token`, or `ssh`), and source
match keys already normalise GitHub clone, SSH and web URLs.

## What Changes

- A delivery with an `X-GitHub-Event` header is a GitHub delivery. It is
  always verified by HMAC-SHA256 over the raw body (`X-Hub-Signature-256`),
  whatever `--method` says. Every other delivery keeps the configured method.
- A GitHub `push` event is parsed into the same `PushEvent` a GitLab push
  produces:
  - `repository.full_name`, `clone_url`, `ssh_url`, `html_url`,
    `default_branch`;
  - `ref`, normalised as today, keeping branch vs tag;
  - `after` as the checkout SHA, and `deleted` as the deletion signal;
  - the union of `commits[].added/modified/removed`, and the commit count.

  Matching, classification, secondary pushes, history and (with
  `webhook-starts-run`) run start behave as for GitLab.
- A GitHub `ping` (sent when a webhook is created) answers `200` after
  verification and is not recorded. Any other GitHub event answers `400`.
  A form-encoded body answers `400` naming `application/json` as the
  required content type.
- The project's webhook setup panel shows GitHub steps next to the GitLab
  ones: payload URL, content type `application/json`, secret, push events
  only.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `webhook-receiver`: GitHub deliveries are verified, parsed and recorded.
- `ui-project-webhook-setup`: GitHub setup steps.

## Non-goals

- GitHub App installation, commit statuses or check runs. Kestrix receives
  pushes, it does not report back.
- `pull_request` and other GitHub events.
- Bitbucket, Gitea and other hosts. The provider split is where they would
  plug in later.
- GitLab's signed-webhook header. GitLab deliveries keep the configured
  method.
- Pushes too large for the Git host's payload. GitHub lists at most 2048
  commits, and changed files beyond those are not detected.

## Impact

- **Depends on** `webhook-starts-run`: without it a GitHub push is matched
  and recorded but runs nothing, because dispatch targets GitLab CI only.
- API: `POST /webhook` accepts GitHub push and ping deliveries. No response
  shape change.
- No schema change.
- Code: `internal/web/webhook.go`, `webhook_verify.go`, `handler_webhook.go`,
  `ui/src/components/projects/WebhookSetupPanel.jsx`,
  `docs/webhook-receiver.md`.

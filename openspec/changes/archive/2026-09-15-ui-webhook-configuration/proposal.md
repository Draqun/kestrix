## Why

The API already treats a project's `webhook_secret` as a real, write-only control (`project-registration` REQ-5, `internal/domain/ports.go:364-373`), and `POST /webhook` refuses every delivery that does not verify against it or the global secret. But `ui/src` never sends, sets, or displays it (`grep webhook_secret ui/src` is empty), and no page tells the user which URL to paste into GitLab. A customer who registers a project from the UI cannot make a single webhook arrive without reading Swagger and using `curl`.

## What Changes

- The project create and edit dialogs gain a **Webhook secret** field. On create it is sent as `webhook_secret`. On edit the field is blank by default and the payload omits `webhook_secret` unless the user types a new value, so saving other fields never clears a stored secret (matching the API's keep-unchanged rule).
- The project detail view shows whether a secret is stored (`has_webhook_secret`) without revealing it.
- A **Webhook setup** panel on the project detail view shows the delivery URL (`<origin>/webhook`), the verification method in effect (`X-Gitlab-Token` or HMAC, from the server's configured method), a copy button, and the three GitLab steps: Settings → Webhooks, URL, Secret token, Push events.
- The edit dialog also exposes `pipeline_file` (already sent by `remoteProjectPayload` but not editable), so a repository whose pipeline is not `pipeline.tf` can be registered from the UI.
- `GET /api/version` (or a new small field on `GET /api/health`) is **not** extended; the webhook method is read from a new read-only field on the project list response only if it already exists server-side, otherwise the panel shows both header names with a note. See design D2.

## Non-goals

- Generating or rotating the secret server-side (the user pastes one they created; `openssl rand -hex 32` is suggested inline).
- Registering the webhook in GitLab on the customer's behalf via the GitLab API.
- Any change to how `POST /webhook` verifies deliveries.
- Showing the global `KESTRIX_WEBHOOK_SECRET`.

## Capabilities

### New Capabilities

- `ui-project-webhook-setup`: what the project UI shows and collects so a user can wire a Git host webhook to a project without the API.

### Modified Capabilities

(none — `project-registration` REQ-5 already defines the write-only secret and `has_webhook_secret`; this change only consumes it)

## Impact

- `ui/src/lib/projectForm.js` (`remoteProjectPayload` gains conditional `webhook_secret` and editable `pipeline_file`), `ui/src/pages/Projects.jsx` (two fields, status chip, setup panel), possibly a new `ui/src/components/projects/WebhookSetupPanel.jsx`.
- `ui/src/api/client.js`: no new endpoint.
- `docs/fe.md`, `docs/webhook-receiver.md` (point to the panel), `CHANGELOG.md`.
- No Go change unless the webhook method is exposed (design D2 decides against it for this change).

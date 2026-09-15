## Context

See proposal.md - Why. `remoteProjectPayload` (`ui/src/lib/projectForm.js`) builds the create/update body from `repo_url`, `repo_ref`, `repo_credential_ref`, `pipeline_dir`, `pipeline_file`. `Projects.jsx` renders create (line ~410) and edit (line ~292) dialogs with `TextField`s for URL, ref, pipeline directory. The API keeps a stored secret when the update omits or blanks `webhook_secret` (`project-registration` REQ-5), which is exactly the semantics the edit dialog needs.

```
Projects.jsx ─ create dialog ─▶ remoteProjectPayload({..., webhook_secret}) ─▶ POST /api/projects
             ─ edit dialog   ─▶ remoteProjectPayload({...})  (secret only if typed) ─▶ PUT /api/projects/{name}
             ─ detail view   ─▶ has_webhook_secret chip + WebhookSetupPanel(origin + "/webhook")
```

## Goals / Non-Goals

**Goals:**
- Zero backend change; consume the existing contract.
- Never render a stored secret; never accidentally clear one.

**Non-Goals:**
- Exposing the server's webhook method through the API (see D2).
- Managing the global secret.

## Decisions

- **D1: Conditional field in the payload, not a "clear secret" checkbox.** The API has no "clear" semantics today (empty keeps the stored value), so offering a clear action would lie. Alternative: add a `clear_webhook_secret` flag to the API - out of scope, and the CLI/API user can achieve it by rotating.
- **D2: Show both header forms instead of adding a `webhook_method` field to the API.** The user's CLAUDE.md forbids UI elements the backend cannot currently supply; the method is server configuration (`KESTRIX_WEBHOOK_METHOD`), not project data. The panel explains: "Token mode (default) verifies `X-Gitlab-Token`; if your operator enabled HMAC, GitLab's Secret token is used as the HMAC key and `X-Hub-Signature-256` is verified." If this proves confusing, a follow-up can expose the method via `GET /api/health`.
- **D3: URL from `window.location.origin`.** The server does not know its public URL (no `KESTRIX_PUBLIC_URL`). The browser's origin is the best available proxy and is correct in the common single-host reverse-proxy case; the panel says so.
- **D4: Secret field is `type="password"` with a reveal toggle while typing.** Stops shoulder-surfing without hiding typos; MUI `TextField` already supports this pattern used in `Login.jsx`.
- **D5: Panel lives in its own component under `ui/src/components/projects/`.** `Projects.jsx` is already ~450 lines; a separate component keeps its tests small.

## Security considerations

- The secret is held only in dialog state and sent once over the existing authenticated, CSRF-protected mutation path; it is never written to local storage, logs, or the project list cache.
- The presence chip is derived from `has_webhook_secret`, which the API already exposes; no new disclosure.

## Risks / Trade-offs

- [User assumes the shown URL is reachable by GitLab when the instance is on a private network] → The panel states the Git host must reach this host; the Webhook Logs page shows deliveries, so a missing delivery is diagnosable.
- [User pastes a weak secret] → Inline hint suggests `openssl rand -hex 32`; not enforced, matching the API.

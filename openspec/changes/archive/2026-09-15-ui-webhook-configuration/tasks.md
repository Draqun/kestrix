## 1. Payload builder

- [x] 1.1 Extend `remoteProjectPayload` in `ui/src/lib/projectForm.js` to include `webhook_secret` only when `form.webhook_secret` is a non-empty trimmed string, and to pass through an edited `pipeline_file`; verify unit tests (`node --test`) cover: secret present on create, omitted when blank on edit, included when typed on edit, non-default `pipeline_file`.

## 2. Dialogs

- [x] 2.1 Add a password-type **Webhook secret** field with reveal toggle and a **Pipeline file** field to both the create and edit dialogs in `ui/src/pages/Projects.jsx`; the edit dialog initialises the secret field empty and never pre-fills from the project; verify component tests assert the request bodies for the three secret scenarios and that no element in the DOM ever contains a stored secret value.
- [x] 2.2 Show a `has_webhook_secret` chip ("Secret set" / "No per-project secret — uses global secret") in the project detail header; verify a component test for both states.

## 3. Setup panel

- [x] 3.1 Create `ui/src/components/projects/WebhookSetupPanel.jsx` rendering the delivery URL from `window.location.origin + '/webhook'`, a copy button using `navigator.clipboard.writeText`, the GitLab steps, and the token/HMAC header explanation from design D2; verify component tests for the URL text, the clipboard call, and rendering under a mocked non-default origin.
- [x] 3.2 Mount the panel in the project detail view below the sources list; verify the Projects page test suite still passes.

## 4. Docs and gate

- [x] 4.1 Update `docs/fe.md` (project dialogs, setup panel) and `docs/webhook-receiver.md` (point to the panel for per-project setup); add a `CHANGELOG.md` entry under Unreleased/Added.
- [x] 4.2 Run `make ui-test` and `make ui-lint`; confirm coverage thresholds in `ui/vitest.coverage.config.js` still pass.

## 5. Verification

- [x] 5.1 Manual smoke against `bin/kestrix serve --static ui/dist`: create a project with a secret from the UI, confirm `GET /api/projects/{name}` reports `has_webhook_secret: true`, edit the ref without touching the secret and confirm it stays `true`, then send a test delivery with `X-Gitlab-Token` and see it in Webhook Logs.

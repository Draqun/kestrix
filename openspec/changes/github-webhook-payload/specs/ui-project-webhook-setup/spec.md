## MODIFIED Requirements

### Requirement: Webhook setup instructions are shown per project

The project detail view SHALL show a setup panel containing the delivery URL built from the page origin and the `/webhook` path, a one-click copy action, and the Git host steps required (webhook URL, secret token, push events). The panel SHALL name both accepted header forms (`X-Gitlab-Token` for token mode, `X-Hub-Signature-256` for HMAC mode) and state that the server's configured method decides which one is verified for GitLab. The panel SHALL also show GitHub steps: the payload URL, content type `application/json`, the secret, and push events only, and SHALL state that GitHub deliveries are always verified by `X-Hub-Signature-256`.

#### Scenario: User copies the delivery URL
- **WHEN** the UI is served at `https://ci.example.com` and the user opens a project's setup panel
- **THEN** the panel shows `https://ci.example.com/webhook` and copying places exactly that string on the clipboard

#### Scenario: Reverse proxy origin
- **WHEN** the UI is reached through a reverse proxy on a different public host
- **THEN** the panel shows the URL as seen by the browser and notes that the Git host must reach that same host

#### Scenario: GitHub steps are shown
- **WHEN** the user opens a project's setup panel
- **THEN** the panel SHALL show GitHub steps naming content type `application/json` and push events only
- **AND** SHALL state that GitHub deliveries are verified by `X-Hub-Signature-256`

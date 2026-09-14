## Purpose

Lets a user wire a Git host webhook to a registered project from the browser: set the per-project secret, see whether one is stored, and copy the exact URL and settings the Git host needs.

## ADDED Requirements

### Requirement: Webhook secret is settable from the project dialogs

The project create dialog SHALL offer an optional webhook secret field sent as `webhook_secret`. The project edit dialog SHALL offer the same field, empty by default; the update payload SHALL include `webhook_secret` only when the user typed a non-empty value, so an edit that leaves the field empty keeps the stored secret unchanged. The UI SHALL never display a stored secret.

#### Scenario: Create with a secret
- **WHEN** the user creates a project and fills the webhook secret field
- **THEN** the create request carries `webhook_secret` with that value and the detail view afterwards shows the secret as set

#### Scenario: Edit without touching the secret
- **WHEN** the user edits a project that has a stored secret, changes the repository ref, and leaves the secret field empty
- **THEN** the update request does not contain `webhook_secret` and the project still reports `has_webhook_secret: true`

#### Scenario: Replace the secret
- **WHEN** the user types a new value in the edit dialog's secret field and saves
- **THEN** the update request carries the new `webhook_secret`

### Requirement: Secret presence is visible

The project detail view SHALL indicate whether a webhook secret is stored, derived from `has_webhook_secret`, and SHALL warn when none is stored and the user therefore depends on the global secret.

#### Scenario: Project without a secret
- **WHEN** a project reports `has_webhook_secret: false`
- **THEN** the detail view shows a visible "no per-project secret" state with a hint to set one

### Requirement: Webhook setup instructions are shown per project

The project detail view SHALL show a setup panel containing the delivery URL built from the page origin and the `/webhook` path, a one-click copy action, and the Git host steps required (webhook URL, secret token, push events). The panel SHALL name both accepted header forms (`X-Gitlab-Token` for token mode, `X-Hub-Signature-256` for HMAC mode) and state that the server's configured method decides which one is verified.

#### Scenario: User copies the delivery URL
- **WHEN** the UI is served at `https://ci.example.com` and the user opens a project's setup panel
- **THEN** the panel shows `https://ci.example.com/webhook` and copying places exactly that string on the clipboard

#### Scenario: Reverse proxy origin
- **WHEN** the UI is reached through a reverse proxy on a different public host
- **THEN** the panel shows the URL as seen by the browser and notes that the Git host must reach that same host

### Requirement: Pipeline file is editable

The create and edit dialogs SHALL expose `pipeline_file`, defaulting to `pipeline.tf`, so a project whose pipeline definition has another file name can be registered without the API.

#### Scenario: Non-default pipeline file
- **WHEN** the user sets the pipeline file to `ci.hcl` and saves
- **THEN** the request carries `pipeline_file: "ci.hcl"` and the detail view shows it

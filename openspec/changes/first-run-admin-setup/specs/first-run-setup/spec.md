## Purpose

Lets the operator of a freshly installed Kestrix instance create the first administrator from the browser, and guarantees that this path closes permanently once any account exists.

## ADDED Requirements

### Requirement: Setup status is discoverable without credentials

`GET /api/setup` SHALL be reachable without authentication and SHALL answer `200` with a JSON body containing `required: true` while no user account exists and `required: false` once at least one account exists. The response SHALL NOT include account names, counts, or any other account data.

#### Scenario: Fresh instance
- **WHEN** the account table is empty and a client calls `GET /api/setup`
- **THEN** the response is `200` with `{"required": true}`

#### Scenario: Instance already has an account
- **WHEN** at least one account exists, created through the API, the CLI, or setup itself
- **THEN** `GET /api/setup` answers `200` with `{"required": false}`

### Requirement: First administrator can be created once

`POST /api/setup` SHALL accept `{name, password}` without authentication and create an account with the `admin` role only while no account exists. Once any account exists the endpoint SHALL answer `409 Conflict` and SHALL NOT create, modify, or delete anything, regardless of payload. Name and password validation SHALL match `POST /api/users` (both required, `400` otherwise). Under concurrent requests on an empty instance, exactly one SHALL succeed and every other SHALL answer `409`.

#### Scenario: Setup on an empty instance
- **WHEN** no account exists and a client posts a valid `{name, password}` to `/api/setup`
- **THEN** the response is `201` naming the account and role `admin`, and the account can immediately log in through `POST /api/auth/login`

#### Scenario: Setup after an account exists
- **WHEN** an account exists and a client posts to `/api/setup`
- **THEN** the response is `409`, no account is created, and existing accounts are unchanged

#### Scenario: Two setup requests race
- **WHEN** two clients post to `/api/setup` at the same time on an empty instance
- **THEN** exactly one receives `201` and one account exists afterwards; the other receives `409`

#### Scenario: Setup is throttled
- **WHEN** a client address exceeds the configured login attempt limit against `/api/setup` within the window
- **THEN** further requests from that address answer `429` with `Retry-After`, the same way login does

### Requirement: The UI offers setup instead of a dead login form

While setup is required, the UI SHALL present a first-administrator form in place of the login form, SHALL submit it to `POST /api/setup`, and on success SHALL sign the new account in and navigate to the dashboard. Once setup is no longer required the UI SHALL show the ordinary login form and SHALL NOT expose the setup form.

#### Scenario: First visit to a fresh instance
- **WHEN** a browser opens the UI and `GET /api/setup` reports `required: true`
- **THEN** the user sees a form to create the first administrator, not the login form

#### Scenario: Setup completed from the UI
- **WHEN** the user submits a valid name and password
- **THEN** the account is created, the user is signed in, and the dashboard is shown without a second manual login

#### Scenario: Setup completed elsewhere meanwhile
- **WHEN** the user submits the form but an account was created in the meantime (`409`)
- **THEN** the UI shows an explanatory message and switches to the login form

### Requirement: CLI bootstrap remains available

`kestrix user create --role admin <name>` SHALL continue to work as an equivalent, scriptable way to create the first account, and using it SHALL close `POST /api/setup` the same way a UI-created account does.

#### Scenario: Operator bootstraps from the CLI
- **WHEN** the operator creates an admin account with the CLI before anyone opens the UI
- **THEN** `GET /api/setup` reports `required: false` and `POST /api/setup` answers `409`

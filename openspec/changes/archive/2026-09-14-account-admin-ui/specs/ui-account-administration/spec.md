## Purpose

Gives an administrator a page to create, list, promote, grant, and delete
accounts, so `access-control`'s "Account administration API" is usable
without scripting HTTP calls or opening the Swagger UI.

## ADDED Requirements

### Requirement: Accounts page lists every account

The Settings area SHALL include an Accounts section, visible only to an
authenticated admin, that lists every account with its name, global role,
and project grants.

#### Scenario: Non-admin cannot see the Accounts section

- **WHEN** a user with a non-admin global role opens Settings
- **THEN** the Accounts section SHALL NOT be shown, matching how the Credentials section already hides itself

#### Scenario: Admin sees every account

- **WHEN** an admin opens the Accounts section
- **THEN** every account SHALL be listed with its name, global role, and project grants
- **AND** no password hash or password value SHALL appear anywhere on the page

### Requirement: Admin can create, promote, grant, and delete an account

The Accounts section SHALL provide a form to create an account (name,
password, initial global role), a control to change an existing account's
global role, a control to add or remove a project-scoped grant, and a
control to delete an account — each calling the corresponding existing
`/api/users*` endpoint.

#### Scenario: Create an account

- **WHEN** an admin submits the create-account form with a name, password, and role
- **THEN** the new account SHALL appear in the list without a page reload

#### Scenario: Change global role

- **WHEN** an admin changes an account's global role from the list
- **THEN** the account's row SHALL reflect the new role once the request succeeds

#### Scenario: Edit project grants

- **WHEN** an admin adds a project + role grant to an account
- **THEN** the account's row SHALL show the new grant once the request succeeds

#### Scenario: Delete an account

- **WHEN** an admin deletes an account from the list
- **THEN** the account SHALL no longer appear in the list once the request succeeds

#### Scenario: Backend rejection is surfaced

- **WHEN** any of the above requests fails (for example, a duplicate account name)
- **THEN** the page SHALL show the server's error message
- **AND** SHALL NOT apply the change optimistically before the request succeeds

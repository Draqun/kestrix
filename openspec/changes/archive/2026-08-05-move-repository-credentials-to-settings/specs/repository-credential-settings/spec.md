## ADDED Requirements

### Requirement: Settings owns repository credential management

Kestrix SHALL provide repository credential creation, rotation, revocation,
and metadata listing within a Repository credentials section of Settings. It
SHALL NOT expose Repository credentials as a separate top-level navigation
destination or duplicate the full manager on project list or detail pages.

#### Scenario: Open repository credential settings

- **WHEN** a user opens Settings
- **THEN** the page SHALL display repository credential metadata and lifecycle
  actions in a Repository credentials section
- **AND** credential values SHALL not appear in the list

#### Scenario: Credential lifecycle is absent from project pages

- **WHEN** a user views the project list or a project detail page outside a
  create or edit dialog
- **THEN** the page SHALL not display the full create, rotate, and revoke
  credential manager

### Requirement: Credential type selection is operable and type-safe

The credential form SHALL provide a controlled, accessible type selector with
token, username/password, and SSH choices. Pointer and keyboard input SHALL
change the selected type, show only its applicable secret fields, and produce
a payload containing only fields applicable to that type.

#### Scenario: Select username and password with a pointer

- **WHEN** a user opens the type selector and clicks Username / app password
- **THEN** the form SHALL select the basic credential type
- **AND** SHALL display username and password fields instead of token or SSH
  private-key fields

#### Scenario: Select SSH with the keyboard

- **WHEN** a user focuses the type selector and chooses SSH private key using
  keyboard controls
- **THEN** the form SHALL select the SSH credential type
- **AND** SHALL display private-key and known-hosts fields

#### Scenario: Change credential type after entering a secret

- **WHEN** a user enters a secret and then changes to another credential type
- **THEN** incompatible hidden secret fields SHALL be cleared
- **AND** the write request SHALL contain only fields for the selected type

### Requirement: Credential operations preserve secret-safe feedback

Credential forms SHALL treat secrets as write-only, clear secret state after a
successful operation or dialog close, refresh metadata after every successful
mutation, and show errors without echoing submitted credential values.

#### Scenario: Create a credential in Settings

- **WHEN** a valid credential is created successfully
- **THEN** Settings SHALL close or reset the form and refresh the metadata list
- **AND** the submitted secret SHALL not be rendered in the result

#### Scenario: Revoke a credential

- **WHEN** a user confirms revocation of an active credential
- **THEN** Settings SHALL refresh it as revoked
- **AND** project credential selectors SHALL no longer offer it as active

#### Scenario: Credential operation fails

- **WHEN** create, rotate, or revoke returns an error
- **THEN** the UI SHALL show an actionable redacted message
- **AND** SHALL not interpolate any submitted token, password, or private key

## MODIFIED Requirements

### Requirement: Project onboarding UI matches remote registration

The project create and edit UI SHALL collect repository URL, required ref,
optional active credential reference, and relative pipeline directory. It SHALL
not request a local repository path and SHALL present field-specific,
secret-redacted validation failures. Project dialogs SHALL provide a compact
credential selector and inline creation action, but project list and detail
pages SHALL not embed the full credential lifecycle manager.

#### Scenario: Create project from the UI

- **WHEN** a user opens the new-project dialog
- **THEN** the form SHALL require project name, repository URL, and ref
- **AND** SHALL offer only non-revoked credential metadata without exposing
  credential values

#### Scenario: Incomplete form

- **WHEN** URL or ref is empty
- **THEN** the create action SHALL remain unavailable or show an inline required
  field error
- **AND** the UI SHALL not submit `repo_path`

#### Scenario: First private project needs a new credential

- **WHEN** a fresh installation has no suitable active credential
- **THEN** project onboarding SHALL allow the user to open a compact write-only
  credential creation dialog without selecting an existing project
- **AND** SHALL preserve the entered project fields, refresh credential
  metadata, and select the returned credential reference after creation

#### Scenario: Manage an existing credential

- **WHEN** a user needs to rotate or revoke an existing credential
- **THEN** project onboarding SHALL direct the user to the Repository
  credentials section in Settings
- **AND** SHALL not duplicate those lifecycle controls in the project dialog

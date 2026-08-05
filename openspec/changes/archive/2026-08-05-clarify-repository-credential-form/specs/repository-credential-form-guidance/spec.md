## ADDED Requirements

### Requirement: Every credential field explains its purpose and requiredness

The repository credential form SHALL present a concise visible explanation and
an accessible information tooltip for every field. It SHALL identify Display
name and Type as required, identify Provider and Host as optional descriptive
metadata, and SHALL state that Provider and Host neither choose an
authentication implementation nor restrict credential use to a remote.

#### Scenario: User reviews common credential fields

- **WHEN** a user opens the create or rotate credential dialog
- **THEN** Display name and Type SHALL be visibly identified as required
- **AND** Provider and Host SHALL be visibly identified as optional
- **AND** each field SHALL expose purpose text without requiring secret entry

#### Scenario: User asks what Provider and Host do

- **WHEN** a user opens the Provider or Host information tooltip
- **THEN** the UI SHALL describe the field as searchable descriptive metadata
- **AND** SHALL NOT imply that it detects a provider, selects an authentication
  driver, validates a remote, or limits credential use

### Requirement: Credential guidance follows the selected authentication type

The repository credential form SHALL dynamically show only inputs applicable to
the selected type, SHALL identify each visible secret input as required or
optional, and SHALL provide examples that explain the expected value without
placing a real secret in the page or payload defaults.

#### Scenario: Configure an HTTPS token

- **WHEN** a user selects HTTPS token
- **THEN** Token SHALL be required and HTTPS username SHALL be optional
- **AND** the username guidance SHALL explain that some Git hosts require a
  conventional username alongside the token
- **AND** a supplied username SHALL be included in the token credential request

#### Scenario: Configure username and app password

- **WHEN** a user selects Username / app password
- **THEN** Username and Password SHALL both be required
- **AND** token and SSH-only fields SHALL not be displayed or submitted

#### Scenario: Configure an SSH private key

- **WHEN** a user selects SSH private key
- **THEN** Private key SHALL be required and Known hosts SHALL be optional
- **AND** the guidance SHALL recommend pinned known-hosts content
- **AND** SHALL explain that omission uses the existing trust-on-first-use
  behavior

#### Scenario: Change the authentication type

- **WHEN** a user changes Type after entering authentication material
- **THEN** guidance and requiredness SHALL update for the selected type
- **AND** incompatible secret and username values SHALL be cleared and omitted
  from the request

### Requirement: Repository credential scope is explicit and secret-safe

The credential dialog SHALL state that repository credentials authenticate Git
source acquisition only, are stored as write-only encrypted values, and are not
injected into task or deployment environments. Guidance and error content MUST
remain static or metadata-based and MUST NOT echo entered secret values.

#### Scenario: User intends to configure deployment authentication

- **WHEN** a user reads the credential dialog before saving
- **THEN** the UI SHALL explain that the credential can authenticate Git clone,
  fetch, and ref resolution
- **AND** SHALL explain that deployment or task secrets require a separate
  runtime configuration mechanism

#### Scenario: Credential operation fails

- **WHEN** create or rotate returns an error after secret entry
- **THEN** helper text, tooltips, and the error message SHALL not contain the
  submitted token, password, or private key

### Requirement: Guidance is keyboard-accessible and responsive

Information controls SHALL have accessible names and SHALL be operable with
pointer and keyboard input. The dialog SHALL preserve readable labels, helper
text, type-specific fields, and actions without horizontal overflow at a 375 px
viewport.

#### Scenario: Open field guidance with a keyboard

- **WHEN** a keyboard user focuses a field information control and activates it
- **THEN** the corresponding explanation SHALL become available to assistive
  technology
- **AND** focus SHALL remain within the credential dialog workflow

#### Scenario: Create a credential on mobile

- **WHEN** the credential dialog is rendered at 375 px
- **THEN** field labels, requiredness, helper text, tooltips, and actions SHALL
  remain readable and operable without horizontal overflow

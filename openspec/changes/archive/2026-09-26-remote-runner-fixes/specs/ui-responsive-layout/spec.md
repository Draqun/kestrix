## ADDED Requirements

### Requirement: Unknown route

The UI SHALL render a not-found page with a link back to the dashboard for any
path it does not define, instead of an empty content area.

#### Scenario: Mistyped URL
- **WHEN** a signed-in user opens a path the UI does not define
- **THEN** the page SHALL say the page was not found
- **AND** SHALL offer a link to the dashboard

## ADDED Requirements

### Requirement: Settings navigation is responsive

The application SHALL expose one Settings destination at `/settings` across
desktop, tablet, and mobile layouts. The destination SHALL use the established
active-state behavior and SHALL contain Repository credentials as a section,
not as its own navigation item.

#### Scenario: Open Settings from the drawer

- **WHEN** a desktop or tablet user activates Settings in the navigation drawer
- **THEN** the application SHALL navigate to `/settings`
- **AND** SHALL mark Settings as active

#### Scenario: Open Settings on mobile

- **WHEN** the viewport is narrower than 600 px
- **THEN** Settings SHALL remain reachable through the responsive navigation
- **AND** the Settings page SHALL render without horizontal overflow at 375 px

#### Scenario: Navigation contains no credential destination

- **WHEN** navigation items are rendered at any supported viewport
- **THEN** no standalone Repository credentials item SHALL be present
- **AND** credential management SHALL be discoverable from Settings

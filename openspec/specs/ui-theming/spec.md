# Spec: UI Theming

## Purpose
Add dark/light mode toggle with system preference detection and persistent user preference.

## Requirements

### Requirement: Mode Toggle
The UI SHALL provide a toggle control in the AppBar that switches between light and dark themes.

#### Scenario: Toggle in AppBar
- **WHEN** the user clicks the theme toggle icon in the AppBar
- **THEN** the UI SHALL switch from dark to light mode (or vice versa)
- **AND** the icon SHALL change to reflect the active mode

### Requirement: System Preference Detection
On first visit (no stored preference), the UI SHALL detect the user's system preference via `prefers-color-scheme`.

#### Scenario: First visit respects system
- **WHEN** a user visits for the first time
- **AND** their OS is set to light mode
- **AND** no localStorage preference exists
- **THEN** the UI SHALL render in light mode

### Requirement: Persistent Preference
The user's choice SHALL be persisted in `localStorage` and restored on subsequent visits.

#### Scenario: Preference survives navigation
- **WHEN** a user switches to light mode
- **AND** navigates to another page
- **THEN** the UI SHALL remain in light mode

### Requirement: No Flash of Wrong Theme
The theme SHALL be applied synchronously before React renders to prevent a flash of the wrong mode.

#### Scenario: SSR guard
- **WHEN** the page loads
- **THEN** the correct theme SHALL be applied before the first paint

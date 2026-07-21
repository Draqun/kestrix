# Spec: UI Brand Identity

## Purpose
Define a cohesive visual identity for Kestrix: orchestra/conductor‑inspired iconography and a refined dark colour palette.

## Requirements

### Requirement: App Icon
The AppBar SHALL display an icon related to orchestra/conductor instead of the current ScienceIcon (beaker).

#### Scenario: Music note icon
```
WHEN the app loads
THEN the AppBar SHALL show a MusicNoteIcon (or similar) instead of ScienceIcon
AND the icon SHALL be the same in both light and dark mode
```

### Requirement: Dark Palette Refresh
The dark theme palette SHALL use a premium, easy-on-the-eyes colour scheme with amber/gold primary accent.

#### Scenario: Dark palette values
```
WHEN the theme mode is "dark"
THEN background.default SHALL be #121212 or similar dark slate
AND primary.main SHALL be amber (e.g. #ffb300 or amber[400])
AND the AppBar SHALL have a subtle gradient overlay
```

### Requirement: Geek-Friendly Touches
The UI SHALL include power‑user amenities: keyboard shortcut hints on nav items and a copy‑to‑clipboard button on run output blocks.

#### Scenario: Keyboard hints
```
WHEN the user hovers over a nav item
THEN a tooltip SHALL show the keyboard shortcut (e.g. "Dashboard (g+d)")
AND pressing g then d SHALL navigate to Dashboard
```

#### Scenario: Copy output
```
WHEN the user hovers over a run output <pre> block
THEN a copy icon SHALL appear in the top-right corner
AND clicking it SHALL copy the output text to the clipboard
```

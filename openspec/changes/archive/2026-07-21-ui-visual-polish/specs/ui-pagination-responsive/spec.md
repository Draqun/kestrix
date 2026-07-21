# Spec: UI Pagination Responsive

## Purpose
Make the Pagination component usable on mobile viewports without overflow or broken layout.

## Requirements

### Requirement: Mobile Pagination
The pagination controls SHALL render without horizontal overflow on viewports as narrow as 375px.

#### Scenario: Compact pagination on mobile
```
WHEN the viewport is < 600px wide
THEN the "Previous" and "Next" buttons SHALL show icons only (no text labels)
AND the "Page X of Y" text SHALL use compact format (e.g. "1/5")
AND the "Per page" selector MAY be hidden behind a chip that opens a popover
```

#### Scenario: Default pagination on desktop
```
WHEN the viewport is >= 600px wide
THEN pagination SHALL render in its current horizontal layout
AND all controls SHALL be visible
```

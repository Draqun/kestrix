## Why

The dark mode colour palette is unpolished (harsh navy blues), the app icon (ScienceIcon/beaker) doesn't reflect the name "Kestrix" (derived from Kestra = orchestra), and the pagination component overflows on mobile screens. These visual issues hurt first impressions and usability.

## What Changes

- Replace the AppBar icon (ScienceIcon) with an orchestra/conductor-themed icon.
- Redesign the dark theme palette for a more premium, easier-on-the-eyes look. Add subtle gradient to the AppBar and card accents.
- Make Pagination component responsive — collapses to a compact layout on mobile (< 600px) with icon-only buttons and hidden "Per page" behind a dialog or inline toggle.
- Add geek-friendly touches: keyboard shortcut hints (e.g., `g` + `d` for Dashboard, `g` + `p` for Pipelines), copy-to-clipboard on run output blocks, a small `λ` indicator for pipeline runtime.

## Capabilities

### New Capabilities
- `ui-brand-identity`: Kestrix brand iconography and colour system with orchestra/conductor motif.
- `ui-pagination-responsive`: Mobile-friendly pagination with compact layout.

### Modified Capabilities
- *(No spec-level requirement changes — existing behaviour preserved)*

## Impact

- **Files changed**: `ui/src/theme/theme.js`, `ui/src/components/Layout.jsx`, `ui/src/components/Pagination.jsx`, `ui/src/pages/Dashboard.jsx`.
- **New files**: `ui/src/hooks/useKeyboardNav.js`.
- **Dependencies**: No new npm packages.
- **Non-goals**: Not changing light palette. Not rewriting React Flow. Not changing backend.

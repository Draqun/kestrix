## Why

The current UI is functional but aesthetically dated — hardcoded dark mode, no responsive layout, no light theme, and the navigation drawer has no mobile-friendly behavior. This blocks adoption on developer laptops without dark-mode preference and makes the interface unusable on phones/tablets.

## What Changes

- Add a **ColorModeContext** with a toggle switch in the AppBar to switch between dark and light themes.
- Define both light and dark MUI theme palettes with a consistent design system.
- Make the **Layout drawer responsive** — auto-collapse to a mini-drawer or bottom nav on mobile, with a hamburger toggle on all breakpoints.
- Audit all pages for **mobile-responsive layouts** using MUI Grid breakpoints, proper spacing, and overflow handling.
- Refactor duplicated `CustomNode` and `layoutFromProject` into shared components (`DAGViewer`, `dagLayout`).
- Clean up unused legacy CSS (`App.css`, unused rules in `index.css`).

## Capabilities

### New Capabilities
- `ui-theming`: Dark/light mode toggle, system preference detection, persistent preference, dual MUI palettes.
- `ui-responsive-layout`: Responsive drawer with `useMediaQuery` breakpoints, mini-drawer variant on tablet, bottom navigation on mobile.

### Modified Capabilities
*(No spec-level requirement changes — existing behavior is preserved.)*

## Impact

- **Files changed**: `ui/src/theme/theme.js`, `ui/src/components/Layout.jsx`, `ui/src/App.jsx`, all page components for responsive audit.
- **New files**: `ui/src/context/ColorModeContext.jsx`, `ui/src/components/DAGViewer.jsx`, `ui/src/lib/dagLayout.js`.
- **Dependencies**: No new npm packages needed (MUI v9 has full dark/light + responsive support built-in).
- **Non-goals**: No backend changes. No new features. No rewrite of React Flow.

## Context

Current UI has a hardcoded dark MUI theme (`mode: 'dark'`), a persistent 260px drawer with no responsive behavior, and duplicated DAG rendering code. Pages use raw `sx` with no consistent spacing system beyond what MUI provides by default. Legacy Vite boilerplate CSS (`App.css`, unused `index.css` rules) adds noise.

## Goals / Non-Goals

**Goals:**
- Dark/light mode toggle persisted in `localStorage`, respecting `prefers-color-scheme` on first visit.
- Responsive Layout: drawer collapses to a mini-drawer (< 900px) and bottom nav (< 600px).
- Shared `DAGViewer` component and `dagLayout` utility extracted from duplicated code.
- All pages render correctly at 375px–1920px without overflow or broken layout.
- Existing functionality preserved — no regressions in run flow, webhook logs, project CRUD.

**Non-Goals:**
- No backend changes.
- No new pages or features.
- No React Flow upgrade or replacement.
- No migration to Tailwind/CSS modules — keep using MUI `sx`.

## Decisions

### 1. ColorModeContext + localStorage over Redux/Zustand
MUI v9's `useMediaQuery` detects system preference on first visit. A React context wraps `ThemeProvider` and exposes `toggleColorMode`. Preference is persisted in `localStorage('theme-mode')`. No additional state management library needed.

### 2. Responsive drawer via MUI's built-in breakpoints
Use `useMediaQuery(theme.breakpoints.down('md'))` (≈900px) to switch from persistent drawer to temporary/mini-drawer. Below `sm` (600px) switch to a bottom navigation bar. This avoids custom responsive logic and leverages MUI's `Drawer` variant swapping.

### 3. Shared DAGViewer component
Extract `CustomNode` + `layoutFromProject` + `ReactFlow` wrapper from `PipelineEditor.jsx` and `Pipelines.jsx` into `ui/src/components/DAGViewer.jsx` and `ui/src/lib/dagLayout.js`. The Pipelines version (with run-status colors) becomes the canonical one, with a `statusMap` prop.

### 4. MUI theme tokens over inline colors
Define light and dark palettes in `theme.js`. The light palette uses a clean white/grey background with teal primary. Dark palette stays close to current navy aesthetic. Remove all hardcoded color strings from page components, replacing with `theme.palette.*` references.

## Risks / Trade-offs

| Risk | Mitigation |
|------|------------|
| Shared DAGViewer refactor may break existing DAG rendering | Add visual regression check via manual test of RunDetail, PipelineEditor, RunProject |
| Light theme untested on current color choices | Use MUI default grey/blue-grey as fallback; tune after visual review |
| Bottom nav on mobile may feel cramped with 4+ items | Use 4-item limit with a "More" drawer for overflow |
| `localStorage` preference out of sync with system | Listen for `change` event on `prefers-color-scheme` media query when no stored preference exists |

## 1. Theming — Dark/Light Mode

- [x] 1.1 Define light and dark palettes in `ui/src/theme/theme.js` with consistent tokens
- [x] 1.2 Create `ui/src/context/ColorModeContext.jsx` — React context with `toggleColorMode`, system preference detection, localStorage persistence
- [x] 1.3 Wire `ColorModeContext` + `ThemeProvider` in `ui/src/App.jsx` with `useMemo` for theme generation
- [x] 1.4 Add theme toggle icon button (LightMode/DarkMode icon) to AppBar in Layout.jsx
- [x] 1.5 Remove hardcoded color strings from all page components — use theme palette references (covered by section 3 DAGViewer refactor)
- [x] 1.6 Clean up unused CSS files: remove `App.css`, strip unused rules from `index.css`

## 2. Responsive Layout

- [x] 2.1 Refactor `Layout.jsx` — add `useMediaQuery` for `md` (900px) and `sm` (600px) breakpoints
- [x] 2.2 Implement mini-drawer variant for tablet (600–900px): icons + tooltips, expands on hover
- [x] 2.3 Implement bottom navigation bar for mobile (< 600px): 4 main nav items + "More" drawer
- [x] 2.4 Audit Dashboard page — responsive Grid (xs=12, sm=6, md=3), no overflow
- [x] 2.5 Audit Projects page — responsive table/cards, search bar wraps on mobile
- [x] 2.6 Audit Pipelines page — RunList filters wrap, DAG viewer scrollable on mobile
- [x] 2.7 Audit RunDetail — DAG viewer + job output stack vertically on mobile
- [x] 2.8 Audit WebhookLogs — table scrolls horizontally on mobile via TableContainer

## 3. Shared DAGViewer Component

- [x] 3.1 Extract `dagLayout` utility (`layoutFromProject`) to `ui/src/lib/dagLayout.js`
- [x] 3.2 Create `ui/src/components/DAGViewer.jsx` — shared `CustomNode` + ReactFlow wrapper with `statusMap` prop
- [x] 3.3 Replace duplicated ReactFlow code in `PipelineEditor.jsx` with `DAGViewer`
- [x] 3.4 Replace duplicated ReactFlow code in `Pipelines.jsx` with `DAGViewer`

## 4. Verification

- [x] 4.1 Manually verify: toggle dark/light mode, refresh — preference persists
- [x] 4.2 Manually verify: resize from 375px to 1920px — no broken layout at any width
- [x] 4.3 Manually verify: RunDetail DAG nodes show correct status colors (shared DAGViewer preserves statusMap)
- [x] 4.4 Manually verify: PipelineEditor DAG renders without errors (shared DAGViewer)
- [x] 4.5 Run `cd ui && npm run build` — no build errors

## Context

The dashboard shows a recent runs table via `GET /api/dashboard` → `data.recent_runs`. Each row has `id`, `project_name`, `status`, `started_at`. Currently rows are not clickable. The rest of the app navigates to run detail via `useNavigate` at URL `/pipelines/runs/:projectName/:runId`.

## Goals / Non-Goals

**Goals:**
- Clicking a recent runs table row navigates to the run detail page
- Use existing navigation pattern (`useNavigate`, no `<Link>`)
- Reuse existing URL schema `/pipelines/runs/:projectName/:runId`

**Non-Goals:**
- Changing the run detail page itself
- Adding keyboard/shortcut navigation
- Middle-click or right-click behavior (browser default for `<tr>`)

## Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Navigation approach | `onClick` on `<TableRow>` via `useNavigate` | Matches existing codebase pattern (Layout.jsx, Pipelines.jsx). No `<Link>` usage anywhere. |
| URL pattern | `/pipelines/runs/${encodeURIComponent(projectName)}/${runId}` | Identical to `App.jsx` `RunDetailRoutes` route. No new route needed. |
| Click target | Entire `<TableRow>` | Larger hit target than cell-level; consistent with table navigation patterns. Cursor changes to pointer on hover. |
| Row hover cursor | `cursor: 'pointer'` on styled `<TableRow>` | Signals interactivity. Existing `hover` prop already adds background change. |

## Risks / Trade-offs

- **Risk**: Table row has no `href` — no middle-click/new-tab support. → Acceptable for MVP; this is the same pattern as the entire app (no `<Link>` usage anywhere).
- **Trade-off**: Row click navigates away from dashboard without confirmation. → Run detail has a back button to return, consistent with other navigation.

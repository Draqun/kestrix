## Context

The dashboard stat grid has 6 slots (xs=2 each). Currently: Projects, Total Runs, Passed, Failed, Running, Pending. The `run_stats` map from `GET /api/dashboard` already includes `canceled` and `skipped` counts (returned by `CountRunsByStatus` GROUP BY). The `recent_runs` response includes `finished_at` from the Run model.

## Goals / Non-Goals

**Goals:**
- Add Canceled stat card (between Running and Pending)
- Add Skipped stat card (after Pending, making 8 cards → xs=1.5 or reflow to xs=3 rows)
- Add Finished column in recent runs table

**Non-Goals:**
- Changing the API response shape
- Backend changes

## Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Grid layout for 8 cards | `xs={6} sm={3} md={1.5}` | 8 items × 1.5 = 12 columns, or use 2 rows of 4 (xs=6 sm=3 md=3). Simpler: keep `md={2}` with 8 cards extending to 2 rows. Actually 8 × md=2 = 16 > 12. Use `md={3}` for 4 cards per row (total 2 rows) OR use `md={1.5}` for single row. |
| Sort order of stats | Projects, Total Runs, Passed, Failed, Running, Canceled, Pending, Skipped | Logical: total first, then results (passed/failed), then in-flight (running/canceled/pending/skipped) |
| Finished column format | Same as Started — `finished_at ? new Date().toLocaleString() : '—'` | Consistent formatting |

## Trade-offs

- 8 stat cards won't fit in one row on `md` breakpoint with `md={2}` (16 > 12). Use `md={1.5}` or switch to 2 rows via `md={3}`. Two rows is safer visually (keeps labels readable).

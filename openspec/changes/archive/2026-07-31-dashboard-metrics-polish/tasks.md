## 1. Frontend

- [x] 1.1 Add Canceled stat card in the grid (between Running and Pending) with `BlockIcon` and `stats.canceled ?? 0`
- [x] 1.2 Add Skipped stat card (after Pending) with `RadioButtonUncheckedIcon` and `stats.skipped ?? 0`
- [x] 1.3 Add Manual stat card with `PendingActionsIcon` and `stats.manual ?? 0`
- [x] 1.4 Add Finished `<TableCell>` in `<TableHead>` after Started
- [x] 1.5 Add Finished `<TableCell>` in each `<TableRow>` after Started — show `finished_at` formatted or '—'
- [x] 1.6 Run `cd ui && npm run build` to confirm no errors

## 2. Verify

- [x] 2.1 Dashboard shows Canceled and Skipped stat cards with correct counts
- [x] 2.2 Recent runs table has Finished column
- [x] 2.3 Stat cards render cleanly at md breakpoint (4 per row)

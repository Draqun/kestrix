## 1. Backend — Dashboard API

- [x] 1.1 Add `CountRunsByStatus() (map[string]int, error)` to PGStore — single SQL `SELECT status, COUNT(*)` GROUP BY query
- [x] 1.2 Add `handleDashboard` handler in server.go at `GET /api/dashboard` — aggregates project count from store, run stats from PGStore (or zeros for FileStore), recent runs from `ListAllRuns(10)`
- [x] 1.3 Register `GET /api/dashboard` route in router
- [x] 1.4 Run `go build` and `go test` to confirm no errors

## 2. Frontend — Dashboard Page

- [x] 2.1 Add `getDashboard()` method to `ui/src/api/client.js` — calls `GET /api/dashboard`, returns parsed JSON
- [x] 2.2 Replace Dashboard.jsx contents — fetch `/api/dashboard` on mount, poll every 30s, display stat cards (projects, total runs, passed/failed counts) and recent runs table
- [x] 2.3 Keep Quick Start and Architecture cards below the data section
- [x] 2.4 Handle FileStore mode gracefully — show limited stats with a note about PostgreSQL
- [x] 2.5 Run `cd ui && npm run build` to confirm no errors

## 3. Verify

- [x] 3.1 With PG: navigate to `/` — dashboard shows project count, run stats, and recent runs
- [x] 3.2 With FileStore: dashboard shows project count and "database required" notice for runs
- [x] 3.3 Confirm polling updates stats after a run completes

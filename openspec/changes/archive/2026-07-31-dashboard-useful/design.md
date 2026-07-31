## Context

Dashboard currently useless — shows fake stats (webhook count as pipelines, repos always 0), no aggregate data. Server has a PG store with projects and runs but no endpoint exposes summary queries.

## Goals / Non-Goals

**Goals:**
- New `GET /api/dashboard` endpoint returning real aggregate stats
- Dashboard UI shows: project count, run counts by status, recent runs table, server status
- Works with both PGStore (full) and FileStore (limited: no runs data)

**Non-Goals:**
- No real-time WebSocket or SSE
- No historical time-series charts
- No user-specific dashboards

## Decisions

1. **Dedicated `/api/dashboard` endpoint over enhancing `/api/health`** — Health should stay lightweight. Dashboard data is richer and has different polling needs.
2. **PGStore-only features degrade gracefully** — If FileStore is used (no runs), the dashboard shows project count and server status only, with a "database required" hint for runs data.
3. **FE polls dashboard every 30s** — Same pattern as current health check. No SSE complexity for MVP.
4. **Recent runs fetched from existing `ListAllRuns(limit)`** — No new DB query needed; just format the response.

## Design

```
┌─────────────────────────────────────────────────────┐
│  Dashboard Page                                      │
│  ┌──────┐ ┌──────┐ ┌──────┐ ┌──────┐               │
│  │Projects│ │ Runs  │ │Passed│ │Failed│              │
│  │   12   │ │  156  │ │  89% │ │  8%  │              │
│  └──────┘ └──────┘ └──────┘ └──────┘               │
│  ┌──────────────────────────────────────────────┐   │
│  │ Recent Runs                                    │   │
│  │ main  build    passed  2m ago  ▶ View          │   │
│  │ main  test     failed  5m ago  ▶ View          │   │
│  │ dev   lint     running 1m ago  ▶ View          │   │
│  └──────────────────────────────────────────────┘   │
│  ┌──────────────┐  ┌─────────────────────────┐     │
│  │ Quick Start  │  │ Architecture Overview    │     │
│  └──────────────┘  └─────────────────────────┘     │
└─────────────────────────────────────────────────────┘
```

### API Response Shape

```json
GET /api/dashboard

{
  "projects": 12,
  "runs": {
    "total": 156,
    "passed": 129,
    "failed": 12,
    "running": 3,
    "canceled": 5,
    "pending": 7
  },
  "recent_runs": [
    {
      "id": 45,
      "project": "metaci",
      "status": "passed",
      "started_at": "2026-07-21T10:00:00Z",
      "finished_at": "2026-07-21T10:05:00Z"
    }
  ],
  "server": {
    "status": "ok",
    "service": "kestrix",
    "uptime_secs": 3600
  }
}
```

### Backend

- New handler `handleDashboard` in server.go at `GET /api/dashboard`
- PGStore gets new method `CountRunsByStatus()` — single SQL query grouping by status
- FileStore returns zero for all run stats (graceful degradation)
- Recent runs: reuse existing `ListAllRuns(10)`

### Frontend

- New `getDashboard()` in api/client.js
- Dashboard.jsx: replace hardcoded mock data with fetched data
- Stat cards in a responsive Grid
- Recent runs as a simple MUI Table
- Quick Start and Architecture cards remain below the fold

## Risks / Trade-offs

- [FileStore has no runs] → Dashboard shows limited data with a hint to switch to PG. Acceptable for MVP.
- [DB query per dashboard refresh] → Minimal — one `CountRunsByStatus` + reuse `ListAllRuns`. Acceptable at current scale.
- [No caching] → Adding polling caching would add complexity. Skip for now.

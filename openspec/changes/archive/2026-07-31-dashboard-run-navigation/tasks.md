## 1. Frontend Change

- [x] 1.1 Add `useNavigate` import and hook call in Dashboard.jsx
- [x] 1.2 Add `onClick` handler on `<TableRow>` for recent runs — navigate to `/pipelines/runs/${encodeURIComponent(run.project_name)}/${run.id}`
- [x] 1.3 Add `sx={{ cursor: 'pointer' }}` on the clickable `<TableRow>`
- [x] 1.4 Run `cd ui && npm run build` to confirm no errors

## 2. Verify

- [x] 2.1 Dashboard recent runs rows show pointer cursor on hover
- [x] 2.2 Clicking a row navigates to `/pipelines/runs/:projectName/:runId`
- [x] 2.3 Run detail page loads with correct run data

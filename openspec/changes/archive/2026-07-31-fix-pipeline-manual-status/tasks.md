## 1. Backend

- [x] 1.1 Remove `hasManual` check and status override in `handleRunProject` (`server.go:917-932`)
- [x] 1.1b Remove same `manual` status override in `FinishRun` (`pgstore.go:479-486`)
- [x] 1.2 Run `go build` and `go test` to confirm no errors

## 2. Frontend

- [x] 2.1 Remove Manual stat card from Dashboard.jsx
- [x] 2.2 Change grid breakpoints back from `md={4}` to `md={3}` (8 cards, 4 per row)
- [x] 2.3 Run `cd ui && npm run build` to confirm no errors

## 3. Verify

- [x] 3.1 Pipeline with manual jobs shows as "passed" in run detail
- [x] 3.2 Dashboard stats sum matches Total Runs

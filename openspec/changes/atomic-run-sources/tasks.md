# Tasks

## 1. Domain Store Interface and PostgreSQL Store

- [ ] 1.1 Update `domain.RunStore.CreateRun` signature to accept `initialSources []domain.AcquiredSource`.
- [ ] 1.2 Update `PGStore.CreateRun` in `internal/store/pgstore_run.go` to marshal `initialSources` directly into the `INSERT INTO runs` query.
- [ ] 1.3 Update mocks in `internal/mocks/mocks.go` and store unit/integration tests.

## 2. Service Layer Integration

- [ ] 2.1 Update `RunService.createRun` and callers in `internal/service/service.go` to pass `opts.InitialSources`.
- [ ] 2.2 Remove the conditional `UpdateRunSources` call in `RunService.Start`.
- [ ] 2.3 Verify all tests pass with 100% statement coverage across `internal/service`, `internal/store`, and `internal/web`.

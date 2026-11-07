# Tasks: stale-job-reclaim-telemetry

## Tasks

- [x] 1. Update `domain.RemoteJobStore.ReclaimStaleJobs` signature and `PGStore` implementation to return terminated run IDs. <!-- id: 1 -->
- [x] 2. Update `MockRemoteJobStore` and unit tests in `internal/store/` to align with new signature. <!-- id: 2 -->
- [x] 3. In `RunService.ReclaimStaleRemoteJobs`, iterate over terminated run IDs, acquire run lock, and invoke `finalizeRemoteRun`. <!-- id: 3 -->
- [x] 4. Add unit and integration tests covering metric recording and event broadcasting during stale job reclaim. <!-- id: 4 -->

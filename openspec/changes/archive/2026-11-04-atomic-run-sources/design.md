# Design: atomic run creation with initial sources

## Context

See `proposal.md` for background.
When a pipeline run starts, `RunService.Start` invokes `s.createRun(ctx, projectName, pipelineDef, opts.Actor, opts.PipelineDefHash)`.
Currently, `CreateRun` executes:
```sql
INSERT INTO runs (project_name, status, pipeline_def, pipeline_def_hash, created_by, sources)
VALUES ($1, 'running', $2, $3, $4, '[]'::jsonb)
```
When `opts.InitialSources` are known upfront (e.g. from a webhook push or manual trigger specifying a branch), writing `'[]'` first and issuing `UPDATE runs SET sources = $2 WHERE id = $1` afterwards introduces a race window and requires an additional database round-trip.

## Decisions

- **D1: Pass initial sources to CreateRun.**
  - `domain.RunStore.CreateRun` will accept `sources []domain.AcquiredSource` (or a creation struct) and serialize it into the `sources` JSONB column in the initial `INSERT`.
  - If `sources` is empty or nil, it defaults to `'[]'::jsonb`.

- **D2: Remove gated UpdateRunSources from RunService.Start.**
  - By persisting sources in `CreateRun`, the gated call in `RunService.Start` is removed entirely.
  - Initial sources are guaranteed to be stored for all runs, not just those where `cancel_in_progress` is true.

- **D3: Cancel lookup exclusion semantics.**
  - `FindActiveRunsByRef` continues to find non-terminal runs. Because cancellation in `RunService.Start` happens before `CreateRun`, the new run's ID does not exist yet at lookup time. Once created, subsequent lookups immediately match it.

## Migration Plan

- Backward-compatible update to the `domain.RunStore` interface and mock implementations.
- No database schema migrations required (the `runs.sources` column already supports arbitrary JSONB arrays).

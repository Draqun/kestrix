## Context

Task output reaches PostgreSQL through six writes:

| Write | Columns | Fed by |
| ----- | ------- | ------ |
| `FlushRunOutput` | `runs.output` | in-process buffer, after each task |
| `FinishRun` | `runs.output`, `runs.error`, `runs.task_results` | end of an in-process run |
| `ApplyTaskResult` | the same three | approved manual task, single-task re-run |
| `AppendTaskResult` | `runs.task_results` | in-process task start and end, remote `pending` |
| `AppendJobTrace` | `runs.output`, `remote_jobs.output` | runner trace |
| `CompleteJob` | `runs.output`, `runs.error`, `runs.task_results` | runner completion |

What PostgreSQL does with the bad input (checked on Postgres 16 with these
writes, UTF8 database):

- A byte that is not valid UTF-8, whether a Latin-1 byte or a multi-byte
  character cut after its first byte: the TEXT columns reject it (22021). The
  JSONB column accepts it, because `json.Marshal` already replaces the byte with
  U+FFFD.
- A NUL byte: the TEXT columns reject it (22021) and so does JSONB (22P05,
  `\u0000`).
- A runner's JSON can carry a NUL but never an invalid byte; the decoder
  replaces it.

A rejected `FinishRun` is discarded by `finishRun`, so the run keeps the status
it had. The per-task flush and append discard theirs as well.

```
in-process task ─▶ redact ─▶ bound ─▶ buffer ──┐
                                               ├─▶ storable() ─▶ store write ─▶ PostgreSQL
runner JSON ─▶ redact (handler) ───────────────┘      (new)
```

## Decisions

### D1. The store makes the text storable

Each of the six writes passes the text it receives through one function before
it reaches SQL. The limit is the database's; the store is the last step on
every path and covers a writer added later, where a fix in the writers would
cover only the ones it names.

Rejected: cleaning in the in-process writer chain. It leaves
`ApplyTaskResult`, the task results, the error text and the whole remote path
as they are.

Rejected: `bytea` columns. A migration, every reader changes, and JSONB still
cannot hold a NUL.

Rejected: failing the task. It did its job; the format of its output is not a
failure of it.

### D2. One rule: unstorable bytes become U+FFFD

`strings.ToValidUTF8(strings.ReplaceAll(s, "\x00", "\xff"), "�")`. A NUL
first becomes an invalid byte, so it merges with the invalid bytes around it
and each consecutive run yields one U+FFFD. Text that is already storable is
returned as it is, without a copy. U+FFFD is the character `json.Marshal`
already puts for an invalid byte, so the TEXT and JSONB columns agree.

### D3. Task results are cleaned on a copy

The `Output` and `Error` of every `TaskResult` the store receives go through the
same rule: JSONB cannot hold a NUL. The caller's slice and structs are not
modified; the other fields are Kestrix's own words.

### D4. A failed write of the outcome is logged

`finishRun`, and `OnTaskComplete` in `prepareRunExecution` for its
`AppendTaskResult` and `FlushRunOutput`, log a warning with the run id and the
error passed through `redact.Err` with the run's resolved secrets, the way
`finishStartedTask` does. No retry and no fallback finish: with D1 the cause
that is known is gone, and a fallback that finished the run with its output
dropped would show a loss as a success.

## Security Considerations

- Replacement must come after redaction. A secret that contains a NUL or an
  invalid byte would no longer match its value once replaced, and would be
  stored in clear. The store is the last step, so every redaction (the writer
  chain, `redactTaskResults`, the runner handlers) has already run.
- The database error that D4 logs carries the first offending bytes in hex. It
  goes through `redact.Err` like every other store error.
- No new input, endpoint or permission.

## Risks / Trade-offs

- The replacement is lossy: raw bytes cannot be recovered from stored output.
  They could not be stored before either.
- In-process output is bounded before it reaches the store, so replacing bytes
  can make it larger than `max_output_bytes`: a byte becomes three, at most
  doubling pathological output (an invalid byte after every valid one).
  Remote output is bounded in the store, after the replacement, and cannot.
- Each write scans its text once. `FlushRunOutput` writes the whole buffer, up
  to the 10 MiB default bound, after each task; a scan of that size is small
  next to the write.

## Migration Plan

None. No schema change, and no stored row is affected: such writes failed. The
previous image simply fails on such output again.

# Spec Delta: remote-runner-execution

## ADDED Requirements

### Requirement: Runner offline trace spooling
A runner SHALL queue outbound trace chunks in a bounded local trace spool whenever network delivery fails due to connection loss, network timeout, or server error. The runner SHALL retry delivery in strict sequential offset order using backoff. Before reporting job completion, the runner SHALL attempt to flush all spooled trace chunks. If the spool exceeds the configured capacity, the runner SHALL truncate oldest buffered chunks while maintaining valid sequential byte offsets.

#### Scenario: Network interruption spools trace chunks
- **WHEN** delivery of a trace chunk fails due to a network connection loss
- **THEN** the runner SHALL buffer the unsent chunk in its local spool
- **AND** subsequent output writes SHALL append to the spool in consecutive byte-offset order
- **AND** the running task script SHALL NOT be terminated

#### Scenario: Reconnected runner flushes spool in sequential order
- **WHEN** the runner regains network connectivity to Kestrix
- **THEN** the runner SHALL send all buffered trace chunks in increasing offset order
- **AND** the server SHALL accept and record the contiguous trace output without offset mismatch errors

#### Scenario: Completion flushes pending spooled chunks
- **WHEN** a task script exits while uncommitted chunks remain in the trace spool
- **THEN** the runner SHALL flush all spooled chunks to Kestrix before sending the job completion request

#### Scenario: Bounded spool limit truncation
- **WHEN** unsent trace chunks exceed the runner spool limit `KESTRIX_RUNNER_TRACE_SPOOL_MAX_BYTES`
- **THEN** the runner SHALL drop the oldest buffered chunks, insert a truncation marker, and preserve monotonic byte offsets

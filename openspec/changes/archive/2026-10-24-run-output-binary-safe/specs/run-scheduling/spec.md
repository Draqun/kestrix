## ADDED Requirements

### Requirement: Run output is stored as valid text

Kestrix SHALL store the output and the error of a run, and the output and the
error of each of its task results, as valid UTF-8 without NUL bytes, whichever
path wrote them: an in-process run, an approved manual task, a single-task
re-run, or the trace and completion of a remote job. Each consecutive run of
bytes that are not valid UTF-8 or are NUL SHALL be stored as one U+FFFD, after
secret redaction. Such bytes SHALL NOT change the status of the run or of the
task that printed them, and SHALL NOT keep a run from reaching its terminal
status.

#### Scenario: A task prints a byte that is not UTF-8
- **WHEN** an in-process task prints `caf` followed by the byte `0xE9`, then a newline, and passes
- **THEN** the run SHALL become `passed`
- **AND** its output SHALL contain `caf`, U+FFFD and the newline

#### Scenario: A task prints a NUL byte
- **WHEN** a task prints `a`, a NUL byte and `b`
- **THEN** the output of the run and of the task result SHALL contain `a`, U+FFFD and `b`

#### Scenario: The output bound cuts a multi-byte character
- **WHEN** `max_output_bytes` falls inside a multi-byte character of a task's output
- **THEN** the run SHALL still reach its terminal status
- **AND** the stored output SHALL end with the truncation marker

#### Scenario: A remote runner reports a NUL byte
- **WHEN** a runner's trace append or completion for a job carries a NUL byte
- **THEN** Kestrix SHALL accept it
- **AND** store the text with U+FFFD in place of the NUL byte

#### Scenario: A secret is redacted before replacement
- **WHEN** a resolved secret contains a byte that is not valid UTF-8 and a task prints it
- **THEN** the stored output SHALL NOT contain the secret

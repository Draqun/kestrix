## MODIFIED Requirements

### Requirement: Pipeline Loading (implemented)

For a webhook matched to a registered project, the server SHALL acquire the
project's remote source at the immutable checkout SHA from the event and load
all pipeline `.tf` files from its configured relative directory. It SHALL not
load the pipeline from `repo_path`, the server's current working directory, or
a newer branch head. The acquired session SHALL be cleaned after webhook
processing.

#### Scenario: Load pipeline from webhook revision

- **WHEN** a push webhook contains a checkout SHA for a registered project
- **THEN** Kestrix SHALL fetch and materialize that exact commit
- **AND** SHALL merge all pipeline `.tf` files from that source before resolving
  changed repositories and tasks

#### Scenario: Event revision is unavailable

- **WHEN** the webhook checkout SHA cannot be authenticated, fetched, or
  resolved
- **THEN** webhook processing SHALL report a redacted source error
- **AND** SHALL not resolve or dispatch tasks from another revision

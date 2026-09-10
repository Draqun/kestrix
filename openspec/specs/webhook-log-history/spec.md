# webhook-log-history Specification

## Purpose
Webhook log history gives operators a durable, queryable record of every webhook event Kestrix has processed, so that failed, ignored, and dispatched pushes can be inspected after the fact and across server restarts.

## Requirements

### Requirement: Durable Event Storage

Kestrix SHALL persist each processed webhook event to the runtime database. A recorded event SHALL survive a server restart and SHALL contain:

- the receive timestamp,
- the project name: the registered project's name when the event was matched to one, otherwise the name in the parsed pipeline,
- the source repository identifier and branch,
- the checkout revision,
- the commit count,
- the list of triggered task names,
- a terminal status of `triggered`, `skipped`, `ignored`, or `error`,
- the error message when the status is `error`,
- the dispatch outcome, including the pipeline ID and pipeline URL when a pipeline was dispatched.

#### Scenario: Event survives restart

- **WHEN** a webhook event is processed and the server is restarted
- **THEN** a subsequent query for webhook history SHALL return that event with its original status and triggered tasks

#### Scenario: Dispatch outcome recorded

- **WHEN** an event triggers tasks and a GitLab pipeline is dispatched successfully
- **THEN** the recorded event SHALL have status `triggered` and SHALL carry the dispatched pipeline ID and pipeline URL

#### Scenario: Failure recorded

- **WHEN** processing an event fails because the pipeline definition cannot be loaded
- **THEN** the recorded event SHALL have status `error` and SHALL carry the failure message

#### Scenario: Matched project recorded

- **WHEN** an event is matched to a registered project
- **THEN** the recorded event SHALL carry that project's registered name

#### Scenario: Dispatch failure recorded

- **WHEN** an event triggers tasks and the GitLab pipeline dispatch fails
- **THEN** the recorded event SHALL have status `error` and SHALL carry the dispatch failure message

#### Scenario: Parsed project recorded

- **WHEN** an event is processed in single-pipeline mode, where no registered project is looked up and the pipeline is parsed from the configured directory
- **THEN** the recorded event SHALL carry the project name declared in that pipeline

#### Scenario: Unmatched project recorded

- **WHEN** an event arrives for a GitLab project ID that is not registered
- **THEN** the event SHALL be recorded with status `ignored` and no project name

### Requirement: History Query API

Kestrix SHALL expose `GET /api/logs`, returning recorded webhook events newest first in a paginated envelope containing the page items, the total number of matching events, and next/previous page links. The endpoint SHALL accept `limit` and `offset` pagination parameters and SHALL support filtering by `project`, by `status`, and by a `since`/`until` timestamp range. Filters SHALL combine conjunctively.

#### Scenario: Default page

- **WHEN** a client requests `GET /api/logs` without parameters
- **THEN** the response SHALL contain the most recent events newest first, the total count of stored events, and pagination links

#### Scenario: Filter by project and status

- **WHEN** a client requests `GET /api/logs?project=web&status=error`
- **THEN** the response SHALL contain only events recorded for project `web` whose status is `error`

#### Scenario: Filter by time range

- **WHEN** a client requests `GET /api/logs` with `since` and `until` timestamps
- **THEN** the response SHALL contain only events received within that range

#### Scenario: Invalid pagination rejected

- **WHEN** a client requests `GET /api/logs` with a non-numeric or negative `limit`
- **THEN** the server SHALL respond with 400 Bad Request and SHALL NOT return events

### Requirement: Retention Bound

Kestrix SHALL bound webhook history growth by a configurable retention limit. When the number of stored events exceeds the limit, the oldest events SHALL be discarded so that recording a new event never fails because of accumulated history.

#### Scenario: Oldest events discarded

- **WHEN** the retention limit is reached and a new event is recorded
- **THEN** the new event SHALL be stored and the oldest stored event SHALL no longer be returned by the history query

#### Scenario: Retention limit configurable

- **WHEN** an operator configures a retention limit
- **THEN** Kestrix SHALL enforce that limit instead of its default

### Requirement: History User Interface

The webhook history page SHALL render only events returned by the server. It SHALL never display placeholder or mock events, and it SHALL reach the API through the same base URL as the rest of the frontend so that it works in both local development and container deployment. The page SHALL offer project, status, and date-range filters together with pagination.

#### Scenario: Empty history

- **WHEN** the server returns no recorded events
- **THEN** the page SHALL show an explicit empty state and SHALL NOT show fabricated rows

#### Scenario: Server unreachable

- **WHEN** the history request fails
- **THEN** the page SHALL show an error state and SHALL NOT substitute placeholder events

#### Scenario: Filtering from the UI

- **WHEN** an operator selects a status filter on the history page
- **THEN** the page SHALL request the filtered page from the server and render the returned events

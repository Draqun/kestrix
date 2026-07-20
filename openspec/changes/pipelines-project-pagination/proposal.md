## Why

Current project and pipeline listing endpoints return all items in a single response, which can cause performance issues and large payloads as the number of projects and pipelines grows. Adding pagination will improve scalability, reduce latency, and provide a better consumer experience.

## What Changes

- Introduce pagination parameters (`limit`, `offset`) to the `GET /api/projects` and `GET /api/projects/{name}/runs` endpoints.
- Set a default `limit` of **25** items per page.
- Allow configurable limit values of **10, 25, 50, or 100** via a query parameter `limit`.
- Return pagination metadata (`total`, `limit`, `offset`, `next`, `prev`) in the JSON response.
- Update internal services and database queries to respect pagination constraints.

## Capabilities

### New Capabilities
- `project-pipeline-pagination`: Adds paginated list support for projects and pipeline runs.

### Modified Capabilities
- *(none)*

## Impact

- Modifies the HTTP API surface for project and pipeline listings.
- Updates database query logic in `internal/store` to apply `LIMIT`/`OFFSET`.
- Adjusts frontend UI components that consume these endpoints to handle pagination controls.
- Adds unit tests for pagination logic and updates existing tests where necessary.

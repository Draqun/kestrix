## Context

The current `GET /api/projects` and `GET /api/projects/{name}/runs` endpoints return full lists of projects and pipeline runs without pagination. As the number of projects and runs grows, responses become large, increasing latency and memory usage for both the server and clients. Adding pagination will limit payload size, improve performance, and provide a standard pattern for list endpoints.

## Goals / Non-Goals

**Goals:**
- Introduce pagination parameters `limit` and `offset` to the project and pipeline list APIs.
- Default `limit` = **25** items per page.
- Allowed `limit` values: **10**, **25**, **50**, **100** (validated on input).
- Return pagination metadata (`total`, `limit`, `offset`, `next`, `prev`) in the JSON response.
- Update database queries to use `LIMIT`/`OFFSET` efficiently.
- Adjust UI components to consume paginated data and display navigation controls.

**Non-Goals:**
- Changing the default sorting order (remains by creation date).
- Implementing advanced filtering or search beyond pagination.
- Supporting cursor‑based pagination (we use offset‑based for simplicity).

## Decisions

- **Endpoint signature:** Add optional query parameters `?limit=<int>&offset=<int>` to `GET /api/projects` and `GET /api/projects/{name}/runs`.
- **Limit validation:** Reject values not in the allowed set with `400 Bad Request` and a clear error message.
- **Database layer:** Extend `internal/store` query functions to accept `limit` and `offset` arguments and apply them using SQL `LIMIT` and `OFFSET` clauses. Ensure appropriate indexes on primary keys for efficient pagination.
- **Response shape:** JSON will include a top‑level `pagination` object:
  ```json
  {
    "items": [...],
    "pagination": {
      "total": 123,
      "limit": 25,
      "offset": 0,
      "next": "/api/projects?limit=25&offset=25",
      "prev": null
    }
  }
  ```
- **Error handling:** If `offset` exceeds `total`, return an empty `items` array with appropriate `next`/`prev` URLs.
- **UI changes:** In the React UI, add a `Pagination` component that reads `pagination` metadata and renders navigation buttons. The component will request the next page using the provided URLs.
- **Testing:** Add unit tests for the new query functions and integration tests for the API endpoints covering all allowed limits and edge cases (e.g., limit=10, offset beyond total).
- **No external dependencies:** All changes are confined to existing Go code and the React UI; no third‑party packages are required.

## Risks / Trade-offs

- **Performance with large offsets:** Offset‑based pagination can become slower on very large tables. Mitigation: recommend using modest offsets and adding appropriate indexes; consider cursor‑based pagination in a future iteration if needed.
- **Backward compatibility:** Existing clients without pagination parameters will continue to receive the default paginated response (limit = 25). This change is non‑breaking.
- **UI complexity:** Adding pagination UI increases component complexity but improves user experience for large data sets.

## Migration Plan

1. Implement backend changes (request parsing, validation, DB query updates).
2. Update API response structs to include pagination metadata.
3. Add unit and integration tests.
4. Deploy the backend changes.
5. Implement React `Pagination` component and integrate it into the Projects and Pipelines pages.
6. Update UI tests and run end‑to‑end verification.
7. Release new version.

## Open Questions

- Should we expose a maximum `limit` to protect against abuse (e.g., cap at 100)? – Yes, the allowed set already caps at 100.
- Do we need to support sorting by other fields? – Out of scope for this change.

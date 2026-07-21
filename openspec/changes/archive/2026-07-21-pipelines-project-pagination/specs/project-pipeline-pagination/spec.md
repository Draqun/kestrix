# Spec: project-pipeline-pagination

## Overview
The pagination capability adds support for limiting and offsetting results when listing projects and pipeline runs via the API. It enforces a default limit of 25 items per page and restricts the selectable limits to 10, 25, 50, or 100.

## Requirements

### Requirement: Pagination parameters
The API endpoints `GET /api/projects` and `GET /api/projects/{name}/runs` SHALL accept optional query parameters `limit` (int) and `offset` (int).

#### Scenario: Valid pagination request
- **WHEN** a client calls `GET /api/projects?limit=25&offset=50`
- **THEN** the server SHALL return at most 25 project items starting from the 51st record.

#### Scenario: Missing parameters (defaults)
- **WHEN** a client calls `GET /api/projects` without `limit` or `offset`
- **THEN** the server SHALL use `limit=25` and `offset=0`.

### Requirement: Allowed limit values
The `limit` parameter SHALL be one of the allowed values: **10**, **25**, **50**, **100**. Any other value SHALL result in a `400 Bad Request` response with an explanatory error message.

#### Scenario: Invalid limit
- **WHEN** a client calls `GET /api/projects?limit=30`
- **THEN** the server SHALL respond with status `400` and error `"limit must be one of [10,25,50,100]"`.

### Requirement: Pagination metadata in response
Responses SHALL include a top‑level `pagination` object with the fields:
- `total` (int): total number of items available.
- `limit` (int): limit used for the current page.
- `offset` (int): offset used for the current page.
- `next` (string|null): URL for the next page or `null` if none.
- `prev` (string|null): URL for the previous page or `null` if none.

#### Scenario: Response includes metadata
- **WHEN** a client receives a paginated list of projects
- **THEN** the JSON payload SHALL contain a `pagination` object with correct `total`, `limit`, `offset`, `next`, and `prev` values.

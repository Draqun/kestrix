## 1. Backend Updates

- [x] 1.1 Add `limit` and `offset` query parsing in project list and pipeline runs handlers
- [x] 1.2 Validate `limit` against allowed values (10, 25, 50, 100) and default to 25
- [x] 1.3 Update DB query functions to apply `LIMIT`/`OFFSET` correctly
- [x] 1.4 Add pagination metadata (`total`, `limit`, `offset`, `next`, `prev`) to API responses

## 2. Frontend Updates

- [x] 2.1 Implement a React `Pagination` component for the Projects view
- [x] 2.2 Wire the component to request next/previous pages using the URLs from the API response
- [ ] 2.3 Add unit tests for the Pagination component

## 3. Testing & Documentation

- [ ] 3.1 Add integration tests for API pagination (valid limits, defaults, invalid limits)
- [ ] 3.2 Update Swagger/OpenAPI documentation to include `limit` and `offset` parameters and pagination response schema
- [ ] 3.3 Verify UI pagination works end‑to‑end (e.g., Cypress test)

## 4. Deployment

- [ ] 4.1 Ensure CI pipeline runs all new tests and lint checks
- [ ] 4.2 Deploy updated backend and UI to a staging environment for verification

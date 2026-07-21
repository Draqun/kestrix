## 1. Filter State and Data Preparation

- [x] 1.1 Add `statusFilter` and `changeFilter` state to the `RunList` component
- [x] 1.2 Derive the list of unique changed repositories from loaded runs for the change filter dropdown
- [x] 1.3 Compute `filteredRuns` by applying `statusFilter` and `changeFilter` to `allRuns`

## 2. Filter UI

- [x] 2.1 Add a status multi-select filter using MUI `Select` with `Chip` render value
- [x] 2.2 Add a changed-repository single-select filter populated from derived repository names
- [x] 2.3 Add a "Clear filters" button or option to reset both filters
- [x] 2.4 Reset pagination to page 1 whenever a filter value changes

## 3. List Updates

- [x] 3.1 Wire the paginated list to use `filteredRuns` instead of `allRuns`
- [x] 3.2 Update the empty-change label from `Changed: none` to `no changes`

## 4. Testing and Verification

- [x] 4.1 Add unit tests for the filter logic (status, change, combined)
- [x] 4.2 Verify the UI builds with `npm run build`
- [x] 4.3 Run `make test` to ensure no regressions

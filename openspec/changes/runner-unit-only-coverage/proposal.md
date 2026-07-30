## Why

One hundred percent unit-test statement coverage is a baseline quality requirement for the project. `internal/runner` currently mixes orchestration with external execution, so its coverage must be made deterministic without credit from integration or end-to-end tests.

## What Changes

- Establish **100% unit-only statement coverage** as the mandatory runner quality gate
- Cover runner helpers, orchestration, mode dispatch, adapters, and error paths with pure Go tests and minimal test seams
- Calculate unit coverage without executing Docker, bash, `gitlab-runner`, external services, or e2e flows
- Keep integration and e2e tests as required, separate regression layers for difficult and business-critical behavior
- Preserve runtime behavior while making external boundaries replaceable by unit-test doubles

## Capabilities

### New Capabilities

- `runner-unit-only-coverage`: Enforces complete runner statement coverage through unit tests while keeping integration and e2e validation outside the coverage metric.

### Modified Capabilities

None.

## Impact

- Runner tests and small dependency seams in `internal/runner`
- Makefile and CI quality gates for unit-only coverage
- Test-suite classification: unit coverage is measured independently from integration and e2e execution
- No public API or runtime semantic changes

## Non-goals

- Removing, weakening, or replacing integration and e2e regression tests
- Counting integration or e2e execution toward the unit coverage percentage
- Expanding this change's implementation beyond `internal/runner`; the project-wide policy is established here and can be rolled out to other packages separately
- Adding product features or authentication changes

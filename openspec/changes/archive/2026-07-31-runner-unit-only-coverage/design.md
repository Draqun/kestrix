## Context

The runner package combines deterministic pipeline logic with operating-system and external-executor boundaries. The project requires 100% unit-test statement coverage, while integration and e2e suites remain separate regression layers and must never inflate that metric.

```text
                  unit-only quality gate
                           |
             +-------------+-------------+
             |                           |
       pure runner logic          external boundaries
       direct unit tests          injected test doubles
             |                           |
             +-------------+-------------+
                           |
                 100.0% statement coverage

       integration / e2e suites (separate results)
             real executors and critical flows
```

## Goals / Non-Goals

**Goals:**

- Reach and enforce exactly 100.0% unit-only statement coverage for `internal/runner`
- Make every production statement deterministic and reachable without external binaries or services
- Keep the unit coverage profile independent from host capabilities
- Retain integration and e2e tests for difficult, high-risk, and critical regression scenarios
- Preserve runtime behavior and public interfaces

**Non-Goals:**

- Replacing integration or e2e tests with unit tests
- Crediting integration or e2e execution toward unit coverage
- Requiring Docker, bash, `gitlab-runner`, databases, or networks for the unit gate
- Applying the implementation to packages outside `internal/runner` in this change

## Decisions

### 1. Separate the coverage gate from integration execution

`make runner-unit-coverage` runs the runner suite in an explicit unit-only mode and produces a dedicated profile. Integration-capable tests are excluded even when their dependencies are installed.

**Rationale:** The metric must represent isolated unit testability and remain identical on developer machines and CI workers.

**Alternative considered:** Run every test and accept combined coverage. Rejected because integration execution could hide missing unit tests and make results environment-dependent.

### 2. Inject only external and failure-prone boundaries

Command construction, selected filesystem operations, DAG resolution, and `PipelineExecutor` backend calls use minimal package-level seams whose production defaults retain the original implementations.

**Rationale:** This makes success and error statements reachable without external processes or destructive filesystem manipulation while avoiding a broad abstraction layer.

**Alternative considered:** Move each executor into a separate package or introduce a full dependency-injection framework. Rejected as unnecessary architectural expansion.

### 3. Test orchestration through fakes

The task loop uses a fake executor to verify ordering, pass/fail behavior, manual and dependency skips, fatal errors, and callbacks. Adapter tests use the Go test helper process rather than real executor binaries.

**Rationale:** Fakes validate runner-owned behavior quickly and deterministically while real-system compatibility remains the responsibility of integration and e2e suites.

### 4. Keep regression layers independent

Unit, integration, and e2e suites have distinct purposes and results. A 100% unit result does not waive critical integration or e2e coverage, and those suites do not affect the unit percentage.

## Security Considerations

- Unit tests SHALL NOT invoke arbitrary host commands, external services, or privileged container operations.
- Test seams default to production implementations and are unexported, preventing API exposure.
- Fake command inputs and outputs contain no credentials; secrets continue to enter runtime code only through configured variable resolvers.
- Integration and e2e environments remain responsible for isolating real executor access and test credentials.

## Risks / Trade-offs

- **Global test seams could be mutated concurrently** → runner coverage tests must not use parallel execution while replacing them, and each replacement must be restored with test cleanup.
- **Statement coverage can encourage low-value assertions** → tests must assert observable behavior and error semantics, not merely execute lines.
- **Unit fakes can diverge from real tools** → retain focused integration and e2e regression tests for critical executor contracts.
- **The policy is project-wide but this change implements only runner** → roll out equivalent package-level gates through separately scoped changes.

## Migration Plan

1. Establish the deterministic runner unit-test seams.
2. Add behavioral unit tests until every runner statement is covered.
3. Add the dedicated Makefile target and enforce the exact 100.0% result.
4. Keep existing integration and e2e workflows separate from the unit coverage profile.
5. Roll back by removing the gate and seams together if an unforeseen runtime regression occurs; no data migration is required.

## Open Questions

- Which package should adopt the project-wide 100% unit-only gate next?
- Should CI aggregate per-package unit-only profiles while retaining package-specific failure reporting?

## 1. Baseline the unit-only gap

- [x] 1.1 Run runner coverage with integration-capable tests explicitly disabled
- [x] 1.2 List every uncovered production statement from the unit-only profile
- [x] 1.3 Classify each gap as pure logic, orchestration, adapter behavior, or deterministic error handling

## 2. Cover pure runner behavior

- [x] 2.1 Add direct unit tests for `order`, `assignStage`, and `repoDir`
- [x] 2.2 Cover success, cycle, missing dependency, escape, missing path, and invalid path branches
- [x] 2.3 Confirm the pure helper surface reaches 100.0% statement coverage

## 3. Cover orchestration and adapters with fakes

- [x] 3.1 Add the minimum command, filesystem, resolver, and backend seams needed for deterministic unit tests
- [x] 3.2 Unit-test task ordering, pass/fail results, dependency skips, manual skips, fatal errors, and callbacks with a fake executor
- [x] 3.3 Unit-test Docker, local, and GitLab runner adapter behavior without executing their external binaries
- [x] 3.4 Unit-test all `PipelineExecutor` modes and nil/error branches

## 4. Enforce and document the quality gate

- [x] 4.1 Add `make runner-unit-coverage` as the source-of-truth unit-only coverage command
- [x] 4.2 Make `make runner-unit-coverage` fail when statement coverage is below `100.0%`
- [x] 4.3 Verify integration and e2e-capable tests do not contribute to the unit coverage profile
- [x] 4.4 Run the complete repository test suite and build independently from the unit coverage gate
- [x] 4.5 Record the final unit-only coverage result and the separate role of integration/e2e tests in the change documentation

## 5. Preserve separate critical regression coverage

- [x] 5.1 Inventory difficult, high-risk, and business-critical runner flows that require real external or cross-component validation
- [x] 5.2 Map each identified critical flow to an integration or e2e regression test, adding missing coverage where necessary
- [x] 5.3 Verify integration and e2e suites run as separate CI results and never write to the unit-only coverage profile

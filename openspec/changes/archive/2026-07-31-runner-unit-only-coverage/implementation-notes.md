## Implementation notes

### Unit-only coverage result

The `internal/runner` package reaches **100.0% statement coverage** with ordinary Go unit tests:

```text
make runner-unit-coverage
26 unit tests passed (integration-backed tests skipped)
total: (statements) 100.0%
```

No Docker daemon, bash script, or `gitlab-runner` binary is executed by the added coverage tests. External commands are replaced with the Go test helper process through a minimal command-construction seam. Filesystem and DAG-resolution error seams cover deterministic error behavior without manipulating global machine state.

### Covered behavior

- task ordering, cycles, and missing dependencies
- stage assignment and repository path validation
- manual and dependency skips, successful/failed tasks, fatal setup failures, and callbacks
- Docker, local, and GitLab runner command construction and result handling
- `PipelineExecutor` local/default, Docker, GitLab runner, unsupported-mode, missing-generator, and single-task dispatch
- work-directory, temporary-directory, generated-file, and DAG-resolution failures

Runtime execution semantics are unchanged. The injected hooks default to the original standard-library and runner functions in production.

### Critical regression inventory

The separate integration/e2e layer covers the behaviors that require real executors or cross-component interaction:

| Layer | Command | Critical scenarios |
| --- | --- | --- |
| Runner integration | `make runner-integration` | Docker task ordering, manual skips, untriggered dependencies, local bash execution, manual task approval, task failures, and GitLab-runner setup |
| Web e2e/integration | `make web-e2e` | End-to-end webhook parsing, project lifecycle, GitLab dispatch with mocked API, no-dispatch behavior, and signature rejection |

These targets do not create or consume the unit coverage profile. CI reports them as separate test jobs (`runner-integration` and `web-e2e`), while the dedicated `runner-unit-coverage` job is the sole source of the 100.0% unit metric.

### Default test behavior

`make test` and `make test-coverage` now run `scripts/test.sh unit`, which passes Go's `-short` flag and excludes PostgreSQL, Docker, runner-external, subprocess CLI, mock support, and webhook/e2e tests from the default run and coverage profile. The unit gate now requires **100.0% global statement coverage**. The successful PostgreSQL constructor return is an integration-only adapter statement and is excluded while its injected seam is covered by unit tests. Use `make test-integration` or `make test-e2e` when those regression layers are explicitly required.

The complete unit-testable backend profile reports **100.0%** for service, web, and file-store packages; the runner's dedicated unit gate also remains fixed at 100.0%. Integration coverage retains a lower aggregate floor because external adapters may be unavailable.

## Context

The runner package mixes pure pipeline orchestration with external command execution. Some of the logic can be tested with ordinary unit tests, but Docker- or binary-backed paths are not part of the coverage target.

## Goals / Non-Goals

**Goals:**
- Increase `internal/runner` unit coverage
- Keep tests deterministic and fast
- Avoid counting integration/e2e tests toward coverage
- Preserve the current public behavior of the runner package

**Non-Goals:**
- Using Docker as part of the coverage strategy
- Expanding scope beyond runner testability
- Changing the product architecture outside the runner package

## Approach

1. First, identify the remaining uncovered branches in `internal/runner`.
2. Cover pure helper logic directly with unit tests.
3. If orchestration is still unreachable without external binaries, introduce the smallest test seam needed to inject a fake executor or command runner.
4. Keep runtime behavior stable and verify with `go test` and coverage output.

## Risks / Trade-offs

- The runner still needs external binaries in production paths.
- Over-abstraction would make the code harder to read, so any test seam must stay minimal.
- Some branches may remain intentionally untested by unit coverage if they are purely external execution plumbing.

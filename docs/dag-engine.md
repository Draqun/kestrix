# DAG Engine

The DAG (Directed Acyclic Graph) engine resolves which tasks should run for a pipeline and in what order. It evaluates trigger expressions against a set of changed repositories, validates the dependency graph, and orders tasks so that dependencies always run first.

## Overview

A pipeline definition contains:

- **ci_repo** blocks – repositories that can trigger tasks.
- **task** blocks – units of work with a trigger expression and optional dependencies.

When a webhook or a manual run is triggered, the engine receives:

- The parsed pipeline definition.
- A set of changed repository names.

The engine returns a resolved list of tasks that should run, respecting both triggers and `depends_on` ordering.

## Trigger Expressions

A trigger expression is a boolean expression written in plain text. It decides whether a task is eligible to run based on which repositories changed.

### Syntax

- `repo.<name>.changed` – true when repository `<name>` is in the changed set.
- `&&` – logical AND.
- `||` – logical OR.
- `!` – logical NOT.
- `(...)` – grouping.

### Examples

```
repo.auth.changed
repo.auth.changed && repo.gateway.changed
repo.auth.changed || repo.gateway.changed
!repo.docs.changed
(repo.auth.changed || repo.gateway.changed) && !repo.docs.changed
```

If a task has no trigger expression, it is considered triggered by default.

## Dependency Ordering

The `depends_on` attribute declares tasks that must complete before the current task starts. The engine builds a graph from these declarations and produces a topological order.

- A task is only included in the resolved set if it is triggered.
- Triggered tasks are ordered so that every dependency runs before its dependents.
- Independent tasks may run in parallel, depending on the runner.

## Cycle Detection

The engine validates the dependency graph before resolution. If task A depends on task B and task B depends on task A, the graph contains a cycle and resolution fails with a descriptive error.

## Validation

Before running, the engine checks that:

- Every `depends_on` reference points to a declared task.
- Every `execute.repo` reference points to a declared `ci_repo`.

Missing references cause validation to fail so that broken pipelines are caught early.

## Manual Tasks

A task with `manual = true` is included in the resolved set when its trigger is satisfied, but it pauses before execution and waits for human approval. Manual tasks are marked clearly in run details and the UI.

## Further Reading

See `openspec/specs/dag-engine/spec.md` for the full specification and test scenarios.

# Spec: DAG Engine

## Purpose
The DAG engine resolves task execution order based on trigger expressions and validates the dependency graph.

## Overview

The DAG engine evaluates trigger expressions against a set of changed repositories and determines which tasks should run. It also validates the dependency graph for cycles and unknown references.
## Requirements
### Requirement: Boolean Expression Evaluation (implemented)

The engine SHALL evaluate trigger expressions using the following operators:
- `&&` (AND): both sides must be true
- `||` (OR): at least one side must be true
- `!` (NOT): negates the following expression
- Parentheses for grouping

#### Scenario: Simple trigger

```
WHEN trigger = "repo.auth.changed"
AND auth is in the changed set
THEN the expression SHALL evaluate to true
```

#### Scenario: AND operator

```
WHEN trigger = "repo.auth.changed && repo.gateway.changed"
AND only auth is changed
THEN the expression SHALL evaluate to false
```

#### Scenario: OR operator

```
WHEN trigger = "repo.auth.changed || repo.gateway.changed"
AND only auth is changed
THEN the expression SHALL evaluate to true
```

#### Scenario: NOT operator

```
WHEN trigger = "!repo.docs.changed"
AND docs is NOT in the changed set
THEN the expression SHALL evaluate to true
```

#### Scenario: Complex expression with grouping

```
WHEN trigger = "(repo.auth.changed || repo.gateway.changed) && !repo.docs.changed"
AND auth is changed and docs is changed
THEN the expression SHALL evaluate to false (docs blocks it)
```

### Requirement: Cycle Detection (implemented)

The engine SHALL detect cycles in the depends_on dependency graph. If a cycle is found, resolution SHALL fail with a descriptive error.

#### Scenario: Direct cycle

```
WHEN task A depends on task B
AND task B depends on task A
THEN resolution SHALL fail with a cycle detection error
```

### Requirement: Unknown Reference Validation (implemented)

The engine SHALL verify that all depends_on references point to declared tasks, and all execute.repo references point to declared ci_repo entries.

#### Scenario: Unknown task reference

```
WHEN a task has depends_on = ["nonexistent"]
AND "nonexistent" is not a declared task
THEN validation SHALL fail
```

### Requirement: Manual Task Support (implemented)

Tasks with `manual = true` SHALL be included in the resolved set if their trigger is satisfied, but SHALL be annotated as requiring human approval.

#### Scenario: Manual deploy

```
WHEN a task has manual = true
AND its trigger expression is satisfied
THEN the task SHALL appear in the resolved set with manual = true
```

### Requirement: REQ-1: Documentation inclusion

The DAG engine documentation SHALL be included in the project-docs site and SHALL be kept up-to-date with any behaviour changes.

#### Scenario: Documentation included in UI build

- **WHEN** the documentation build runs,
- **THEN** the DAG engine documentation SHALL be present in `ui/dist/docs`.

#### Scenario: Documentation page is reachable from overview

- **WHEN** a user opens the project-docs overview page,
- **THEN** a link to the DAG engine documentation page SHALL be available.


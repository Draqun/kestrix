# Delta Spec: dag-engine

## MODIFIED Requirements

### Requirement: Manual Task Support (implemented)

Tasks with `manual = true` SHALL be included in the resolved set if their trigger is satisfied, but SHALL be annotated as requiring human approval. The project resolve API response SHALL include a `manual` boolean field for each triggered task.

#### Scenario: Manual deploy

```
WHEN a task has manual = true
AND its trigger expression is satisfied
THEN the task SHALL appear in the resolved set with manual = true
```

#### Scenario: Project resolve API exposes the manual flag

- **WHEN** a client calls `POST /api/projects/{name}/resolve` and a manual task is triggered,
- **THEN** the response SHALL contain a triggered task object with `name`, `reason`, and `manual: true`.

#### Scenario: Automatic tasks have manual set to false

- **WHEN** a client calls `POST /api/projects/{name}/resolve` and an automatic task is triggered,
- **THEN** the triggered task object SHALL have `manual: false`.

# pipeline-visualization Specification

## Purpose
Pipeline visualization renders project repositories, tasks, triggers, and
dependencies as an interactive DAG on the New Run page.
## Requirements
### Requirement: Pipeline graph nodes represent repositories and tasks

Each repository and task in the pipeline definition SHALL be rendered as a
node in the graph. Repository nodes SHALL display the repository name, path,
and kind. Task nodes SHALL display the task name, manual indicator, trigger
expression, and script preview. Node IDs SHALL use `repo:{Name}` and
`task:{Name}` formats.

#### Scenario: Repository and task nodes are displayed

- **WHEN** a valid pipeline definition is loaded
- **THEN** every declared repository and task SHALL have a corresponding graph node
- **AND** repository and task labels SHALL display their relevant metadata

### Requirement: Pipeline graph edges represent relationships

Dependencies SHALL be rendered as directed edges. Trigger edges from
repository to task SHALL be animated orange edges. Execution edges from task
to repository SHALL be animated blue edges. Dependency edges from task to task
SHALL be static purple edges.

#### Scenario: Pipeline relationships are displayed

- **WHEN** a pipeline contains triggers, execution targets, or task dependencies
- **THEN** the graph SHALL render the corresponding directed edges
- **AND** each edge type SHALL use its defined color and animation behavior

### Requirement: Pipeline graph uses automatic left-to-right layout

The graph SHALL use Dagre via `dagre.js` for automatic left-to-right layout
with rank direction `LR`, minimum node separation of 80px, and minimum rank
separation of 120px.

#### Scenario: Nodes receive automatic positions

- **WHEN** graph nodes and edges are created
- **THEN** Dagre SHALL calculate their positions in left-to-right rank order
- **AND** the renderer SHALL not require manually assigned coordinates

### Requirement: Pipeline graph displays runtime status colors

When viewing a running or completed run, task nodes SHALL reflect execution
status using border color and opacity: passed green, failed red, running
orange, pending or skipped gray with reduced opacity, canceled orange with
reduced opacity, and manual waiting blue.

#### Scenario: Task status is reflected in the graph

- **WHEN** a task has a runtime status
- **THEN** its node SHALL use the corresponding status border color and opacity
- **AND** a task waiting for manual approval SHALL use the manual waiting style

### Requirement: Pipeline graph supports React Flow interactions

The graph SHALL support pan, zoom, minimap navigation, task node selection,
fit-to-view on initial load, and custom node types registered with React Flow.

#### Scenario: User navigates and selects the graph

- **WHEN** the graph is displayed
- **THEN** the user SHALL be able to pan, zoom, use the minimap, fit the graph to view,
  and click a task node for selection

### Requirement: Frontend transforms pipeline definitions into graph data

The frontend SHALL fetch the pipeline definition from
`GET /api/projects/{name}/definition`, normalize the parsed project into the
shape expected by the graph layout, and transform it with
`layoutFromProject(project)` into `{nodes, edges}` consumed by `DAGViewer`.

#### Scenario: Pipeline definition reaches the graph renderer

- **WHEN** the New Run page loads a project
- **THEN** it SHALL fetch the project definition from the project definition endpoint
- **AND** it SHALL render the resulting nodes and edges in `DAGViewer`

### Requirement: New Run page displays and controls the pipeline graph

The New Run page at `/pipelines/new-run/:projectName` SHALL display the pipeline
graph before execution, allow Resolve to preview triggered tasks, and allow Run
to execute the pipeline.

#### Scenario: User previews and runs a pipeline

- **WHEN** a user opens the New Run page for a valid project
- **THEN** the complete graph SHALL be visible before execution
- **AND** Resolve SHALL highlight tasks whose trigger expressions are satisfied
- **AND** Run SHALL start execution using the selected change input

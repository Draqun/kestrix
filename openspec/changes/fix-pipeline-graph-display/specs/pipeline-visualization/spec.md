# Pipeline Visualization Specification

## Overview
The pipeline visualization capability provides a directed acyclic graph (DAG) representation of a Kestrix project's pipeline definition. It shows repositories (trigger sources), tasks (execution units), and their dependencies via edges.

## Requirements

### REQ-VIZ-001: Node Representation
**Description**: Each repository and task in the pipeline definition must be rendered as a node in the graph.
- **Repo nodes**: Display repository name, path, and kind (monorepo/multi-repo)
- **Task nodes**: Display task name, manual indicator, trigger expression, and script preview
- **Node IDs**: Use format `repo:{Name}` and `task:{Name}` for consistent identification

### REQ-VIZ-002: Edge Representation
**Description**: Dependencies must be rendered as directed edges between nodes.
- **Trigger edges** (repo → task): Animated orange edges showing which repository changes trigger which task
- **Execution edges** (task → repo): Animated blue edges showing which repository a task executes in
- **Dependency edges** (task → task): Static purple edges showing task-to-task dependencies

### REQ-VIZ-003: Layout Algorithm
**Description**: Use Dagre (via dagre.js) for automatic left-to-right (LR) layout.
- Rank direction: LR (left to right)
- Node separation: 80px minimum
- Rank separation: 120px minimum
- Nodes positioned automatically, no manual coordinates required

### REQ-VIZ-004: Runtime Status Colors
**Description**: When viewing a running/completed run, task nodes must reflect execution status via border color and opacity.
- **Passed**: Green border (#4caf50), full opacity
- **Failed**: Red border (#f44336), full opacity
- **Running**: Orange border (#ff9800), full opacity
- **Pending**: Gray border (#616161), 50% opacity
- **Skipped**: Gray border (#757575), 50% opacity
- **Canceled**: Orange border (#ff9800), 50% opacity
- **Manual waiting**: Blue border (#42a5f5), full opacity

### REQ-VIZ-005: Interactive Features
**Description**: The graph must support standard React Flow interactions.
- Pan, zoom, and minimap navigation
- Node click handler for task selection
- Fit-to-view on initial load
- Custom node types via React Flow's nodeTypes registry

### REQ-VIZ-006: Data Flow
**Description**: Frontend must fetch pipeline definition from API and transform to graph nodes/edges.
- **API endpoint**: `GET /api/projects/{name}/definition`
- **Response**: Parsed `model.Project` with `Repos` and `Tasks` arrays
- **Transform**: `layoutFromProject(project)` in `dagLayout.js` converts to `{nodes, edges}`
- **Renderer**: `DAGViewer` React component consumes nodes/edges

### REQ-VIZ-007: New Run Page Integration
**Description**: The "New Run" page (`/pipelines/new-run/:projectName`) must display the pipeline graph before execution.
- Load project definition on mount
- Show graph with all nodes/edges (no runtime status)
- Allow "Resolve" to preview which tasks would trigger
- Allow "Run" to execute the pipeline

## Non-Requirements
- Real-time updates during execution (polling is handled by RunDetail page)
- Manual node positioning (layout is automatic)
- Export to image/PDF (out of scope for MVP)

## Acceptance Criteria
1. Navigating to `/pipelines/new-run/kestrix` shows a complete DAG with all repos and tasks
2. Nodes have correct labels, types, and styling
3. Edges correctly represent triggers, execution targets, and dependencies
4. "Resolve" highlights triggered tasks with colored borders
5. Graph renders without console errors
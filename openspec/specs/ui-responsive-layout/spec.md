# Spec: UI Responsive Layout

## Purpose
Make the application layout responsive across desktop, tablet, and mobile viewports.
## Requirements
### Requirement: Responsive Drawer
The navigation drawer SHALL adapt to different screen sizes:
- ≥ 900px: Persistent drawer (current behavior, 260px wide)
- 600px–900px: Mini-drawer (icons only, expands on hover)
- < 600px: Bottom navigation bar

#### Scenario: Desktop drawer
- **WHEN** the viewport is ≥ 900px wide
- **THEN** the drawer SHALL be visible and persistent
- **AND** main content SHALL respect the 260px drawer offset

#### Scenario: Mobile bottom nav
- **WHEN** the viewport is < 600px wide
- **THEN** the drawer SHALL be replaced by a bottom navigation bar
- **AND** main content SHALL use full viewport width

### Requirement: Page Content Responsiveness
All existing pages SHALL render legibly at widths from 375px to 1920px without horizontal overflow, clipped content, or broken layout.

#### Scenario: Dashboard grid
- **WHEN** the viewport is < 600px wide
- **THEN** health check cards SHALL stack vertically (1 column)

#### Scenario: Run detail
- **WHEN** the viewport is < 600px wide
- **THEN** the DAG viewer SHALL scroll horizontally if needed
- **AND** the job output panel SHALL be full width below the DAG

### Requirement: Shared DAGViewer Component
The duplicated `CustomNode` and `layoutFromProject` code SHALL be extracted into a shared `DAGViewer` component with a `statusMap` prop for run-status coloring.

#### Scenario: Run detail uses DAGViewer
- **WHEN** viewing a run with status data
- **THEN** the DAG SHALL show green/passed, red/failed, orange/running nodes

#### Scenario: Pipeline editor uses DAGViewer
- **WHEN** viewing the pipeline editor without run data
- **THEN** the DAG SHALL render all tasks as pending (blue/grey)
- **AND** no status colors SHALL be applied

### Requirement: Unknown route

The UI SHALL render a not-found page with a link back to the dashboard for any
path it does not define, instead of an empty content area.

#### Scenario: Mistyped URL
- **WHEN** a signed-in user opens a path the UI does not define
- **THEN** the page SHALL say the page was not found
- **AND** SHALL offer a link to the dashboard


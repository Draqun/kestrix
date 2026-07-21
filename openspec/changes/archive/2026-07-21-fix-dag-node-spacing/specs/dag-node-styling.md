## ADDED Requirements

### Requirement: DAG node children have consistent spacing
The DAG node container SHALL use flex column layout with a uniform gap between all children.

#### Scenario: Label and status have spacing
- **WHEN** a DAG node renders with label and status
- **THEN** there SHALL be at least 4px gap between the two elements

#### Scenario: Label, trigger, and status have spacing
- **WHEN** a DAG node renders with label, trigger, and status
- **THEN** all three elements SHALL be separated by a uniform gap

#### Scenario: Path caption occupies its own line
- **WHEN** a DAG node renders with a path
- **THEN** the path caption SHALL be on its own line (display block)

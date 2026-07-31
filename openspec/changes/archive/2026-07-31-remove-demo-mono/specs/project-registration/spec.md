## MODIFIED Requirements

### Requirement: Auto-load from pipeline_dir

The server MUST NOT create or update a registered project as a side effect of startup or server construction based solely on `--dir` or `cfg.PipelineDir`.

#### Scenario: Server startup does not register a pipeline directory
- **WHEN** the server starts with `--dir` or a configured pipeline directory containing a valid `pipeline.tf`
- **THEN** the server MUST NOT create or update a project row for the HCL project name

#### Scenario: Deleted project remains deleted after restart
- **WHEN** a project is deleted through the project API and the server restarts with a pipeline directory configured
- **THEN** the deleted project MUST remain absent from `GET /api/projects`

#### Scenario: Project registration is explicit
- **WHEN** a client sends `POST /api/projects` with a project name and repository/pipeline configuration
- **THEN** the server MUST persist the project and return it through `GET /api/projects`

#### Scenario: Pipeline directory contains multiple HCL files
- **WHEN** the server starts with a configured pipeline directory containing multiple `.tf` files
- **THEN** startup MUST NOT select any file for implicit project registration

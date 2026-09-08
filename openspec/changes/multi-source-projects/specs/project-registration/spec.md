## MODIFIED Requirements

### Requirement: REQ-3: Project lookup by GitLab project ID (implemented)
The webhook handler SHALL find projects first by matching the delivery's
repository URLs against every project's derived sources, and SHALL fall back
to the GitLab project ID when no source matches. A URL match MAY return more
than one project; an ID match returns at most one.

#### Scenario: Lookup by GitLab project ID
- **WHEN** a webhook payload contains `project.id`
- **AND** no project source matches the payload's repository URLs
- **THEN** the server SHALL look up the project by `gitlab_project_id`
- **WHEN** no project matches
- **THEN** the server SHALL return HTTP 200 with `{"status": "ignored", "reason": "unknown project"}`

#### Scenario: Lookup by source URL takes precedence
- **WHEN** a webhook payload's repository URL matches a source of project `checkout`
- **AND** the payload's `project.id` equals a different project's `gitlab_project_id`
- **THEN** the server SHALL resolve the delivery to `checkout`
- **AND** SHALL NOT also resolve the project matched by ID

**WHEN** a webhook payload contains `project.id`  
**THEN** the server SHALL look up the project by `gitlab_project_id` only after the source URL match found nothing

**WHEN** no project matches  
**THEN** the server SHALL return HTTP 200 with `{"status": "ignored", "reason": "unknown project"}`

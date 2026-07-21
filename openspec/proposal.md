# Proposal: Kestrix — Repo-Agnostic CI/CD Control Plane

## Why

Standard CI/CD tools (GitLab CI, GitHub Actions) are deeply coupled to individual repositories. In multirepo setups, each service has its own pipeline with no native way to express cross-repo conditional triggers. This leads to duplicated pipeline configurations, manual coordination between teams, and fragile integration testing workflows.

Kestrix decouples pipeline definitions from repositories, treating them as data sources rather than pipeline owners. This enables a single, centralized pipeline definition that orchestrates builds, tests, and deployments across an entire system.

## What Changes

- **HCL-based pipeline definition**: Pipelines are defined in HCL (HashiCorp Configuration Language), leveraging the same ecosystem as Terraform/OpenTofu
- **OR/AND/NOT trigger logic**: Boolean expressions like `(repo.auth.changed || repo.gateway.changed) && !repo.docs.changed` — impossible in standard GitLab CI
- **Cross-repo execution**: Tasks can reference scripts in any declared repository, with explicit permission control
- **Webhook receiver**: HTTP endpoint that receives push events, evaluates the DAG, and dispatches to GitLab Runners
- **gitlab-ci.yml generator**: Produces valid GitLab CI configuration from the resolved task graph

## Capabilities

### New Capabilities

- **pipeline-definition**: HCL-based project/repo/task model with trigger expressions
- **dag-engine**: Boolean expression evaluator with AND/OR/NOT logic, cycle detection, validation
- **webhook-receiver**: HTTP server for GitLab push events with file classification
- **ci-generator**: Produces gitlab-ci.yml from resolved DAG nodes

### Modified Capabilities

None — this is a greenfield project.

## Impact

- **Code**: New Go project with `cmd/kestrix`, `internal/{model,dag,parser,web}`
- **Dependencies**: `hashicorp/hcl/v2` (HCL parsing), `go-chi/chi` (HTTP router), `go-cty` (HCL value types)
- **Infrastructure**: Requires a GitLab Runner (user-supplied) and a GitLab webhook pointing to the Kestrix server
- **Security**: Webhook payloads are untrusted; pipeline definitions are trusted (user-managed)

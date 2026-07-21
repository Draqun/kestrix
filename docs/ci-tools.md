# CI Tools

Kestrix includes tooling for generating GitLab CI configuration, dispatching pipelines, and running tasks across multiple execution environments.

## CI Generator

Converts a resolved Kestrix pipeline into a standard `gitlab-ci.yml` file, enabling integration with GitLab's CI/CD runner ecosystem.

```bash
kestrix generate path/to/pipeline.tf
```

## Dispatch Client

Triggers GitLab pipelines from the command line or from within a Kestrix task execution. Uses the GitLab API to start a pipeline with the generated configuration.

## Runner

Executes pipeline tasks in one of three modes:

| Mode | Description |
|------|-------------|
| `local` (default) | Runs tasks as bash subprocesses on the host |
| `docker` | Executes each task in a Docker container |
| `gitlab-runner` | Delegates execution to a GitLab Runner |

## Validation

```bash
kestrix validate pipeline.tf   # validate HCL syntax and structure
kestrix resolve pipeline.tf    # resolve triggers for a set of changed repos
```

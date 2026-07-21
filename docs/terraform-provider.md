# Terraform Provider

Kestrix offers a Terraform provider that allows teams to manage projects and pipeline configurations as infrastructure‑as‑code.

## Resources

| Resource | Purpose |
|----------|---------|
| `kestrix_project` | Register a project with its repository path, pipeline directory, and GitLab integration settings |

## Example

```hcl
provider "kestrix" {
  endpoint = "http://localhost:8080"
}

resource "kestrix_project" "my_app" {
  name              = "my-app"
  repo_path         = "./my-app"
  pipeline_dir      = "ci"
  gitlab_project_id = 12345
  default_branch    = "main"
}
```

## Status

The provider is in early development. See the [project registration spec](../openspec/specs/project-registration/spec.md) for detailed requirements.

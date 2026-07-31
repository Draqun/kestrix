# Terraform Provider

Kestrix offers a Terraform provider that allows teams to manage projects and pipeline configurations as infrastructure‑as‑code.

## Resources

| Resource | Purpose |
|----------|---------|
| `kestrix_project` | Register a remote project with URL/ref, pipeline directory, optional credential reference, and GitLab integration settings |

## Example

```hcl
provider "kestrix" {
  endpoint = "http://localhost:8080"
}

resource "kestrix_project" "my_app" {
  name              = "my-app"
  repo_url          = "https://git.example.test/team/my-app.git"
  repo_ref          = "main"
  credential_ref    = "cred_example"
  pipeline_dir      = "ci"
  gitlab_project_id = 12345
  default_branch    = "main"
}
```

## Status

The provider is in early development. See the [project registration spec](../openspec/specs/project-registration/spec.md) for detailed requirements.

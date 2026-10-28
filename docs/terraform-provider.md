# Terraform & OpenTofu Provider

Kestrix's Terraform and OpenTofu provider manages Kestrix project registration as
infrastructure-as-code.

- **Terraform Registry:** [`Draqun/kestrix`](https://registry.terraform.io/providers/Draqun/kestrix/latest)
- **OpenTofu Registry:** [`Draqun/kestrix`](https://search.opentofu.org/provider/Draqun/kestrix)
- **Source Repository:** [terraform-provider-kestrix on GitLab](https://gitlab.com/kestrix/terraform-provider-kestrix) (mirror on [GitHub](https://github.com/Draqun/terraform-provider-kestrix))
- **Authentication:** the same bearer token as [`docs/authentication.md`](authentication.md), so
  the same `viewer`/`operator`/`admin` roles apply to a Terraform run.

## Usage

Add the provider configuration to your Terraform or OpenTofu root module:

```hcl
terraform {
  required_providers {
    kestrix = {
      source  = "Draqun/kestrix"
      version = "~> 0.1.0"
    }
  }
}

provider "kestrix" {
  endpoint        = "http://localhost:8080"
  api_token       = var.kestrix_api_token
  timeout_seconds = 30
}

resource "kestrix_project" "app" {
  name                = "app"
  repo_url            = "https://gitlab.com/acme/app.git"
  repo_ref            = "main"
  repo_credential_ref = var.repo_credential_ref
  pipeline_dir        = "ci"
  pipeline_file       = "pipeline.tf"
}
```

Resource schema, examples, and release status live in the provider's own
repository.

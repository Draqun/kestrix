# Remote-source example. The Git URL is provider-neutral and may point at
# GitHub, GitLab, Bitbucket, a self-hosted service, or any Git server.
project "remote-services" {
  ci_repo "public_service" {
    url = "https://github.com/example/service.git"
    ref = "main"
  }

  ci_repo "private_service" {
    url            = "ssh://git@git.example.test/team/private-service.git"
    ref            = "v1.4.0"
    credential_ref = "build-auth"
  }

  ci_task "test" {
    trigger = "repo.public_service.changed || repo.private_service.changed"
    execute {
      repo   = "public_service"
      script = "bash scripts/test.sh"
    }
  }
}

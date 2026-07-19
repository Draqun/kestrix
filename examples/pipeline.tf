# Example MetaCI project: monorepo with two services and integration tests.
#
# This file demonstrates the core MetaCI concepts:
#   1. ci_repo definitions (path-based for monorepo)
#   2. ci_task with AND/OR/NOT trigger logic
#   3. Cross-repo execution (integration tests run in a separate repo)

project "demo-mono" {
  default_image = "golang:1.26"

  ci_repo "auth_service" {
    path = "src/auth"
    kind = "subpath"
  }

  ci_repo "api_gateway" {
    path = "src/gateway"
    kind = "subpath"
  }

  ci_repo "docs" {
    path = "docs"
    kind = "subpath"
  }

  ci_repo "integration_tests" {
    path = "tests"
    kind = "subpath"
  }

  # Build auth_service when its own code changes
  ci_task "build_auth" {
    trigger   = "repo.auth_service.changed"
    manual    = false

    execute {
      repo   = "auth_service"
      script = "./scripts/build.sh"
      image  = "golang:1.26"
    }
  }

  # Build api_gateway when its own code changes
  ci_task "build_gateway" {
    trigger   = "repo.api_gateway.changed"
    depends_on = []

    execute {
      repo   = "api_gateway"
      script = "./scripts/build.sh"
    }
  }

  # Integration tests run when EITHER service changes, but NOT when only docs change.
  # This is the key use case: OR/AND/NOT logic across repos.
  ci_task "test_integration" {
    trigger    = "(repo.auth_service.changed || repo.api_gateway.changed) && !repo.docs.changed"
    depends_on = ["build_auth", "build_gateway"]

    execute {
      repo   = "integration_tests"
      script = "./run.sh"
    }

    manual = false
  }

  # Manual deploy task — requires human approval even if triggered.
  ci_task "deploy_staging" {
    trigger    = "repo.auth_service.changed || repo.api_gateway.changed"
    depends_on = ["test_integration"]
    manual     = true

    execute {
      repo   = "auth_service"
      script = "./scripts/deploy.sh staging"
    }
  }
}

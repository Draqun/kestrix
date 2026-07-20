# Example 1: Simple single-webservice pipeline
#
# A monolithic web application with a full CI/CD lifecycle:
#   lint → static analysis → test → build → deploy
#
# All stages run in the same repo. Triggers are chained via depends_on
# so each stage waits for the previous one to finish.

project "webapp-single" {
  default_image = "node:20"

  ci_repo "webapp" {
    path = "."
    kind = "subpath"
  }

  ci_repo "docs" {
    path = "docs"
    kind = "subpath"
  }

  # ─── Lint ───────────────────────────────────────────────────
  ci_task "lint" {
    trigger = "repo.webapp.changed"
    execute {
      repo   = "webapp"
      script = "npm run lint"
    }
  }

  # ─── Static analysis (SonarQube / ESLint security) ─────────
  ci_task "static_analysis" {
    trigger    = "repo.webapp.changed"
    depends_on = ["lint"]
    execute {
      repo   = "webapp"
      script = "npm run analyze"
    }
  }

  # ─── Unit tests ────────────────────────────────────────────
  ci_task "test" {
    trigger    = "repo.webapp.changed"
    depends_on = ["static_analysis"]
    execute {
      repo   = "webapp"
      script = "npm test -- --coverage"
    }
  }

  # ─── Build (Docker image) ──────────────────────────────────
  ci_task "build" {
    trigger    = "repo.webapp.changed"
    depends_on = ["test"]
    execute {
      repo   = "webapp"
      script = "docker build -t webapp:latest ."
    }
  }

  # ─── Deploy to staging (only on main) ─────────────────────
  ci_task "deploy_staging" {
    trigger    = "repo.webapp.changed && !repo.docs.changed"
    depends_on = ["build"]
    manual     = true
    execute {
      repo   = "webapp"
      script = "kubectl apply -f k8s/staging/"
    }
  }
}

project "kestrix" {
  default_image = "golang:1.25-alpine"

  ci_repo "kestrix" {
    path = "."
    kind = "subpath"
  }

  # ─── Backend ────────────────────────────────────────────────

  ci_task "lint" {
    trigger = "repo.kestrix.changed"
    execute {
      repo   = "kestrix"
      script = "bash scripts/lint.sh"
    }
  }

  ci_task "test" {
    trigger    = "repo.kestrix.changed"
    depends_on = ["lint"]
    execute {
      repo   = "kestrix"
      script = "bash scripts/test.sh"
    }
  }

  ci_task "build" {
    trigger    = "repo.kestrix.changed"
    depends_on = ["test"]
    execute {
      repo   = "kestrix"
      script = "bash scripts/build.sh"
    }
  }

  # ─── Frontend ───────────────────────────────────────────────

  ci_task "lint_fe" {
    trigger = "repo.kestrix.changed"
    execute {
      repo   = "kestrix"
      script = "bash scripts/lint-fe.sh"
    }
  }

  ci_task "build_fe" {
    trigger    = "repo.kestrix.changed"
    depends_on = ["lint_fe", "test_fe"]
    execute {
      repo   = "kestrix"
      script = "bash scripts/build-fe.sh"
    }
  }

  # ─── Frontend Tests ──────────────────────────────────────────

  ci_task "test_fe" {
    trigger    = "repo.kestrix.changed"
    depends_on = ["lint_fe"]
    execute {
      repo   = "kestrix"
      script = "bash scripts/test-fe.sh"
    }
  }

  # ─── Coverage ────────────────────────────────────────────────

  ci_task "coverage_be" {
    trigger    = "repo.kestrix.changed"
    depends_on = ["test"]
    execute {
      repo   = "kestrix"
      script = "bash scripts/coverage-be.sh"
    }
  }

  ci_task "coverage_fe" {
    trigger    = "repo.kestrix.changed"
    depends_on = ["test_fe"]
    execute {
      repo   = "kestrix"
      script = "bash scripts/coverage-fe.sh"
    }
  }

  # ─── Docker ────────────────────────────────────────────────

  ci_task "docker_build" {
    trigger    = "repo.kestrix.changed"
    depends_on = ["build", "build_fe"]
    execute {
      repo   = "kestrix"
      script = "bash scripts/docker-build.sh"
    }
  }

  # ─── Business Website ──────────────────────────────────────

  ci_task "business_build" {
    trigger    = "repo.kestrix.changed"
    depends_on = ["build_fe"]
    execute {
      repo   = "kestrix"
      script = "bash scripts/build-business.sh"
    }
  }

  ci_task "business_deploy" {
    trigger    = "repo.kestrix.changed"
    depends_on = ["business_build"]
    manual     = true
    execute {
      repo   = "kestrix"
      script = "bash scripts/deploy-business.sh"
    }
  }

  # ─── Deploy ────────────────────────────────────────────────

  ci_task "deploy" {
    trigger    = "repo.kestrix.changed"
    depends_on = ["docker_build"]
    manual     = true
    execute {
      repo   = "kestrix"
      script = "bash scripts/deploy.sh"
    }
  }
}

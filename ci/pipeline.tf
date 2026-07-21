project "metaci" {
  default_image = "golang:1.25-alpine"

  ci_repo "metaci" {
    path = "."
    kind = "subpath"
  }

  # ─── Backend ────────────────────────────────────────────────

  ci_task "lint" {
    trigger = "repo.metaci.changed"
    execute {
      repo   = "metaci"
      script = "bash scripts/lint.sh"
    }
  }

  ci_task "test" {
    trigger    = "repo.metaci.changed"
    depends_on = ["lint"]
    execute {
      repo   = "metaci"
      script = "bash scripts/test.sh"
    }
  }

  ci_task "build" {
    trigger    = "repo.metaci.changed"
    depends_on = ["test"]
    execute {
      repo   = "metaci"
      script = "bash scripts/build.sh"
    }
  }

  # ─── Frontend ───────────────────────────────────────────────

  ci_task "lint_fe" {
    trigger = "repo.metaci.changed"
    execute {
      repo   = "metaci"
      script = "bash scripts/lint-fe.sh"
    }
  }

  ci_task "build_fe" {
    trigger    = "repo.metaci.changed"
    depends_on = ["lint_fe", "test_fe"]
    execute {
      repo   = "metaci"
      script = "bash scripts/build-fe.sh"
    }
  }

  # ─── Frontend Tests ──────────────────────────────────────────

  ci_task "test_fe" {
    trigger    = "repo.metaci.changed"
    depends_on = ["lint_fe"]
    execute {
      repo   = "metaci"
      script = "bash scripts/test-fe.sh"
    }
  }

  # ─── Coverage ────────────────────────────────────────────────

  ci_task "coverage" {
    trigger    = "repo.metaci.changed"
    depends_on = ["test", "test_fe"]
    execute {
      repo   = "metaci"
      script = "bash scripts/coverage.sh"
    }
  }

  # ─── Docker ────────────────────────────────────────────────

  ci_task "docker_build" {
    trigger    = "repo.metaci.changed"
    depends_on = ["build", "build_fe"]
    execute {
      repo   = "metaci"
      script = "bash scripts/docker-build.sh"
    }
  }

  # ─── Deploy ────────────────────────────────────────────────

  ci_task "deploy" {
    trigger    = "repo.metaci.changed"
    depends_on = ["docker_build"]
    manual     = true
    execute {
      repo   = "metaci"
      script = "bash scripts/deploy.sh"
    }
  }
}

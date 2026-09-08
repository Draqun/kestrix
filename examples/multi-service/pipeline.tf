# Example 2: Multi-component system with shared infrastructure
#
# Three microservices + an infrastructure repo:
#   - user-service (Go, REST API)
#   - payment-service (Python, FastAPI)
#   - notification-service (Node.js, Express)
#   - infra (Terraform + Helm charts)
#
# Key patterns:
#   - Each service builds/tests independently when its code changes
#   - Infra changes trigger a deploy of ALL services
#   - Integration tests run when any two services change simultaneously
#   - NOT triggers skip docs-only changes

project "multi-service" {
  default_image = "golang:1.22"

  ci_repo "user_service" {
    path = "services/user"
    kind = "subpath"
  }

  ci_repo "payment_service" {
    path = "services/payment"
    kind = "subpath"
  }

  ci_repo "notification_service" {
    path = "services/notification"
    kind = "subpath"
  }

  ci_repo "infra" {
    path = "infra"
    kind = "subpath"
  }

  ci_repo "docs" {
    path = "docs"
    kind = "subpath"
  }

  # ─── Per-service pipelines ─────────────────────────────────

  ci_task "user_lint" {
    trigger = "repo.user_service.changed"
    execute {
      repo   = "user_service"
      script = "golangci-lint run ./..."
    }
  }

  ci_task "user_test" {
    trigger    = "repo.user_service.changed"
    depends_on = ["user_lint"]
    execute {
      repo   = "user_service"
      script = "go test -race ./..."
    }
  }

  ci_task "user_build" {
    trigger    = "repo.user_service.changed"
    depends_on = ["user_test"]
    execute {
      repo   = "user_service"
      script = "go build -o bin/user-service ./cmd/server"
    }
  }

  ci_task "payment_lint" {
    trigger = "repo.payment_service.changed"
    execute {
      repo   = "payment_service"
      script = "ruff check ."
    }
  }

  ci_task "payment_test" {
    trigger    = "repo.payment_service.changed"
    depends_on = ["payment_lint"]
    execute {
      repo   = "payment_service"
      script = "pytest --cov=src tests/"
    }
  }

  ci_task "payment_build" {
    trigger    = "repo.payment_service.changed"
    depends_on = ["payment_test"]
    execute {
      repo   = "payment_service"
      script = "poetry build"
    }
  }

  ci_task "notification_lint" {
    trigger = "repo.notification_service.changed"
    execute {
      repo   = "notification_service"
      script = "eslint src/"
    }
  }

  ci_task "notification_test" {
    trigger    = "repo.notification_service.changed"
    depends_on = ["notification_lint"]
    execute {
      repo   = "notification_service"
      script = "npm test"
    }
  }

  ci_task "notification_build" {
    trigger    = "repo.notification_service.changed"
    depends_on = ["notification_test"]
    execute {
      repo   = "notification_service"
      script = "npm run build"
    }
  }

  # ─── Cross-service integration tests ──────────────────────
  # Run when ANY two services change at the same time.
  ci_task "integration_tests" {
    trigger = "(repo.user_service.changed && repo.payment_service.changed) || (repo.user_service.changed && repo.notification_service.changed) || (repo.payment_service.changed && repo.notification_service.changed)"
    execute {
      repo   = "user_service"
      script = "make integration-test"
    }
  }

  # ─── Infrastructure ────────────────────────────────────────
  ci_task "infra_validate" {
    trigger = "repo.infra.changed"
    execute {
      repo   = "infra"
      script = "terraform validate && tofu plan -detailed-exitcode"
    }
  }

  ci_task "infra_apply_staging" {
    trigger    = "repo.infra.changed"
    depends_on = ["infra_validate"]
    manual     = true
    execute {
      repo   = "infra"
      script = "TF_VAR_deploy_token=$DEPLOY_TOKEN tofu apply -auto-approve -var-file=environments/staging.tfvars"
    }
  }

  # DEPLOY_TOKEN is never written here: secret_ref names a `secret`
  # credential in Kestrix's encrypted store, created ahead of time with
  # `POST /api/credentials {"type":"secret",...}`. Kestrix resolves it when
  # infra_apply_staging is approved and injects it as this job's DEPLOY_TOKEN
  # environment variable; it is redacted from run and task output.
  variable "DEPLOY_TOKEN" {
    secret_ref = "cred_deploy_token"
    scope      = "job"
    repo       = "infra"
    job        = "infra_apply_staging"
  }

  # ─── Full deploy (infra triggers all services) ─────────────
  ci_task "deploy_all_staging" {
    trigger    = "repo.infra.changed && !repo.docs.changed"
    depends_on = ["infra_apply_staging"]
    manual     = true
    execute {
      repo   = "infra"
      script = "./scripts/deploy-all.sh staging"
    }
  }
}

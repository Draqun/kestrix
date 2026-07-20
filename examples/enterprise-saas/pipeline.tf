# Example 3: Enterprise monorepo — large SaaS platform
#
# A monorepo housing:
#   - 4 backend services (Go microservices)
#   - 2 frontend apps (React + Next.js)
#   - Shared libraries (Go modules + npm packages)
#   - Platform infra (Terraform + Kubernetes Helm)
#   - Data pipeline (Python/Spark ETL jobs)
#   - Mobile SDK (React Native)
#
# Patterns demonstrated:
#   - Shared library changes fan out to all consumers
#   - Environment-specific deployments (staging → canary → production)
#   - Security scans run on every change
#   - Compliance gates block production deploys
#   - NOT triggers skip docs/changelog-only changes
#   - depends_on chains enforce pipeline ordering

project "enterprise-saas" {
  default_image = "golang:1.22"

  # ─── Backend services ──────────────────────────────────────
  ci_repo "api_gateway" {
    path = "services/api-gateway"
    kind = "subpath"
  }

  ci_repo "auth_service" {
    path = "services/auth"
    kind = "subpath"
  }

  ci_repo "billing_service" {
    path = "services/billing"
    kind = "subpath"
  }

  ci_repo "analytics_service" {
    path = "services/analytics"
    kind = "subpath"
  }

  # ─── Frontend apps ─────────────────────────────────────────
  ci_repo "web_dashboard" {
    path = "apps/dashboard"
    kind = "subpath"
  }

  ci_repo "admin_portal" {
    path = "apps/admin"
    kind = "subpath"
  }

  # ─── Shared libraries ──────────────────────────────────────
  ci_repo "shared_go_libs" {
    path = "libs/go-shared"
    kind = "subpath"
  }

  ci_repo "shared_npm_libs" {
    path = "libs/npm-shared"
    kind = "subpath"
  }

  # ─── Platform ──────────────────────────────────────────────
  ci_repo "infra" {
    path = "platform/terraform"
    kind = "subpath"
  }

  ci_repo "helm_charts" {
    path = "platform/helm"
    kind = "subpath"
  }

  ci_repo "data_pipeline" {
    path = "data/etl"
    kind = "subpath"
  }

  ci_repo "mobile_sdk" {
    path = "sdk/mobile"
    kind = "subpath"
  }

  ci_repo "docs" {
    path = "docs"
    kind = "subpath"
  }

  # ═══════════════════════════════════════════════════════════
  # Backend pipelines
  # ═══════════════════════════════════════════════════════════

  ci_task "gateway_security_scan" {
    trigger = "repo.api_gateway.changed || repo.shared_go_libs.changed"
    execute {
      repo   = "api_gateway"
      script = "gosec ./... && go vet ./..."
    }
  }

  ci_task "gateway_test" {
    trigger    = "repo.api_gateway.changed || repo.shared_go_libs.changed"
    depends_on = ["gateway_security_scan"]
    execute {
      repo   = "api_gateway"
      script = "go test -race -count=100 ./..."
    }
  }

  ci_task "gateway_build" {
    trigger    = "repo.api_gateway.changed || repo.shared_go_libs.changed"
    depends_on = ["gateway_test"]
    execute {
      repo   = "api_gateway"
      script = "go build -o bin/gateway ./cmd/server"
    }
  }

  ci_task "auth_security_scan" {
    trigger = "repo.auth_service.changed || repo.shared_go_libs.changed"
    execute {
      repo   = "auth_service"
      script = "gosec ./... && go vet ./..."
    }
  }

  ci_task "auth_test" {
    trigger    = "repo.auth_service.changed || repo.shared_go_libs.changed"
    depends_on = ["auth_security_scan"]
    execute {
      repo   = "auth_service"
      script = "go test -race -count=50 ./..."
    }
  }

  ci_task "auth_build" {
    trigger    = "repo.auth_service.changed || repo.shared_go_libs.changed"
    depends_on = ["auth_test"]
    execute {
      repo   = "auth_service"
      script = "go build -o bin/auth ./cmd/server"
    }
  }

  ci_task "billing_security_scan" {
    trigger = "repo.billing_service.changed || repo.shared_go_libs.changed"
    execute {
      repo   = "billing_service"
      script = "gosec ./... && go vet ./..."
    }
  }

  ci_task "billing_test" {
    trigger    = "repo.billing_service.changed || repo.shared_go_libs.changed"
    depends_on = ["billing_security_scan"]
    execute {
      repo   = "billing_service"
      script = "go test -race -count=50 ./..."
    }
  }

  ci_task "billing_build" {
    trigger    = "repo.billing_service.changed || repo.shared_go_libs.changed"
    depends_on = ["billing_test"]
    execute {
      repo   = "billing_service"
      script = "go build -o bin/billing ./cmd/server"
    }
  }

  ci_task "analytics_test" {
    trigger = "repo.analytics_service.changed || repo.shared_go_libs.changed"
    execute {
      repo   = "analytics_service"
      script = "go test -race ./..."
    }
  }

  ci_task "analytics_build" {
    trigger    = "repo.analytics_service.changed || repo.shared_go_libs.changed"
    depends_on = ["analytics_test"]
    execute {
      repo   = "analytics_service"
      script = "go build -o bin/analytics ./cmd/server"
    }
  }

  # ═══════════════════════════════════════════════════════════
  # Frontend pipelines
  # ═══════════════════════════════════════════════════════════

  ci_task "dashboard_lint" {
    trigger = "repo.web_dashboard.changed || repo.shared_npm_libs.changed"
    execute {
      repo   = "web_dashboard"
      script = "npm run lint"
    }
  }

  ci_task "dashboard_test" {
    trigger    = "repo.web_dashboard.changed || repo.shared_npm_libs.changed"
    depends_on = ["dashboard_lint"]
    execute {
      repo   = "web_dashboard"
      script = "npm test -- --coverage"
    }
  }

  ci_task "dashboard_build" {
    trigger    = "repo.web_dashboard.changed || repo.shared_npm_libs.changed"
    depends_on = ["dashboard_test"]
    execute {
      repo   = "web_dashboard"
      script = "npm run build"
    }
  }

  ci_task "admin_lint" {
    trigger = "repo.admin_portal.changed || repo.shared_npm_libs.changed"
    execute {
      repo   = "admin_portal"
      script = "npm run lint"
    }
  }

  ci_task "admin_test" {
    trigger    = "repo.admin_portal.changed || repo.shared_npm_libs.changed"
    depends_on = ["admin_lint"]
    execute {
      repo   = "admin_portal"
      script = "npm test -- --coverage"
    }
  }

  ci_task "admin_build" {
    trigger    = "repo.admin_portal.changed || repo.shared_npm_libs.changed"
    depends_on = ["admin_test"]
    execute {
      repo   = "admin_portal"
      script = "npm run build"
    }
  }

  # ═══════════════════════════════════════════════════════════
  # Shared library fan-out — any consumer must rebuild
  # ═══════════════════════════════════════════════════════════

  ci_task "go_libs_test" {
    trigger = "repo.shared_go_libs.changed"
    execute {
      repo   = "shared_go_libs"
      script = "go test ./..."
    }
  }

  ci_task "npm_libs_test" {
    trigger = "repo.shared_npm_libs.changed"
    execute {
      repo   = "shared_npm_libs"
      script = "npm test"
    }
  }

  # ═══════════════════════════════════════════════════════════
  # Data pipeline
  # ═══════════════════════════════════════════════════════════

  ci_task "etl_lint" {
    trigger = "repo.data_pipeline.changed"
    execute {
      repo   = "data_pipeline"
      script = "ruff check . && mypy src/"
    }
  }

  ci_task "etl_test" {
    trigger    = "repo.data_pipeline.changed"
    depends_on = ["etl_lint"]
    execute {
      repo   = "data_pipeline"
      script = "pytest --cov=src tests/"
    }
  }

  # ═══════════════════════════════════════════════════════════
  # Mobile SDK
  # ═══════════════════════════════════════════════════════════

  ci_task "sdk_lint" {
    trigger = "repo.mobile_sdk.changed || repo.shared_npm_libs.changed"
    execute {
      repo   = "mobile_sdk"
      script = "eslint src/"
    }
  }

  ci_task "sdk_test" {
    trigger    = "repo.mobile_sdk.changed || repo.shared_npm_libs.changed"
    depends_on = ["sdk_lint"]
    execute {
      repo   = "mobile_sdk"
      script = "jest --coverage"
    }
  }

  # ═══════════════════════════════════════════════════════════
  # Platform infrastructure
  # ═══════════════════════════════════════════════════════════

  ci_task "tf_validate" {
    trigger = "repo.infra.changed"
    execute {
      repo   = "infra"
      script = "terraform validate && tofu fmt -check"
    }
  }

  ci_task "tf_plan_staging" {
    trigger    = "repo.infra.changed"
    depends_on = ["tf_validate"]
    execute {
      repo   = "infra"
      script = "tofu plan -var-file=environments/staging.tfvars -out=staging.tfplan"
    }
  }

  ci_task "tf_apply_staging" {
    trigger    = "repo.infra.changed"
    depends_on = ["tf_plan_staging"]
    manual     = true
    execute {
      repo   = "infra"
      script = "tofu apply staging.tfplan"
    }
  }

  ci_task "helm_lint" {
    trigger = "repo.helm_charts.changed"
    execute {
      repo   = "helm_charts"
      script = "helm lint charts/"
    }
  }

  ci_task "helm_test" {
    trigger    = "repo.helm_charts.changed"
    depends_on = ["helm_lint"]
    execute {
      repo   = "helm_charts"
      script = "helm unittest charts/"
    }
  }

  # ═══════════════════════════════════════════════════════════
  # Cross-cutting: compliance scan before any staging deploy
  # ═══════════════════════════════════════════════════════════

  ci_task "compliance_scan" {
    trigger = "(repo.api_gateway.changed || repo.auth_service.changed || repo.billing_service.changed) && !repo.docs.changed"
    execute {
      repo   = "api_gateway"
      script = "trivy fs --severity HIGH,CRITICAL . && license-checker --production"
    }
  }

  # ═══════════════════════════════════════════════════════════
  # Integration tests (when multiple backend services change)
  # ═══════════════════════════════════════════════════════════

  ci_task "integration_tests" {
    trigger    = "(repo.auth_service.changed && repo.billing_service.changed) || (repo.api_gateway.changed && repo.auth_service.changed)"
    depends_on = ["compliance_scan"]
    execute {
      repo   = "api_gateway"
      script = "make test-integration"
    }
  }

  # ═══════════════════════════════════════════════════════════
  # Staging deploy (all services)
  # ═══════════════════════════════════════════════════════════

  ci_task "deploy_staging" {
    trigger    = "(repo.infra.changed || repo.helm_charts.changed) && !repo.docs.changed"
    depends_on = ["tf_apply_staging", "helm_test"]
    manual     = true
    execute {
      repo   = "infra"
      script = "./scripts/deploy-all.sh staging"
    }
  }

  # ═══════════════════════════════════════════════════════════
  # Production deploy (requires manual approval + all green)
  # ═══════════════════════════════════════════════════════════

  ci_task "deploy_production" {
    trigger    = "repo.infra.changed && !repo.docs.changed"
    depends_on = ["deploy_staging"]
    manual     = true
    execute {
      repo   = "infra"
      script = "./scripts/deploy-all.sh production"
    }
  }
}

# Makefile for MetaCI development tasks
# Provides convenient shortcuts for common development tasks

export HOME      := /home/hermes
export GOPATH    := /home/hermes/workspace/.gopath
export GOMODCACHE := /home/hermes/workspace/.gopath/pkg/mod
export GOCACHE   := /home/hermes/workspace/.gocache

BINARY    := bin/metaci
GO        := go
GOFLAGS   :=
GOTEST    := go test

.PHONY: help init build build-release test test-short test-coverage fmt fmt-check lint vet staticcheck check quality validate resolve tidy clean full-cleanup

help: ## Show this help message
	@echo "MetaCI Development Commands:"
	@echo ""
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2}'

# === Setup ===

init: ## Initialize project (install deps, build)
	$(GO) mod tidy
	$(GO) build -o $(BINARY) ./cmd/metaci
	@echo ""
	@echo "=========================================="
	@echo "MetaCI initialized successfully!"
	@echo "=========================================="
	@echo ""
	@echo "Binary: $(BINARY)"
	@echo ""
	@echo "Usage:"
	@echo "  make validate"
	@echo "  make resolve CHANGED=auth_service"
	@echo ""

# === Build ===

build: ## Build the project
	$(GO) build -o $(BINARY) ./cmd/metaci

build-release: ## Build release version (stripped, optimized)
	$(GO) build -ldflags="-s -w" -o $(BINARY) ./cmd/metaci

# === Testing ===

test: ## Run all tests with verbose output
	$(GOTEST) ./... -v

test-short: ## Run tests (short mode, skip slow tests)
	$(GOTEST) ./... -short -count=1

test-coverage: ## Generate test coverage report
	@mkdir -p coverage
	$(GOTEST) ./... -coverprofile=coverage/coverage.out -covermode=atomic
	$(GO) tool cover -html=coverage/coverage.out -o coverage/coverage.html
	$(GO) tool cover -func=coverage/coverage.out
	@echo ""
	@echo "Coverage report: coverage/coverage.html"

# === Formatting ===

fmt: ## Format code with gofmt
	$(GO) fmt ./...

fmt-check: ## Check code formatting (CI-friendly)
	@echo "Checking formatting..."
	@test -z "$$(gofmt -l .)" || (echo "Files not formatted:"; gofmt -l .; exit 1)
	@echo "✓ Formatting OK"

# === Static Analysis ===

vet: ## Run go vet ( catches suspicious constructs)
	$(GO) vet ./...

staticcheck: ## Run staticcheck (advanced static analysis)
	@if command -v staticcheck >/dev/null 2>&1; then \
		staticcheck ./...; \
	else \
		echo "⚠ staticcheck not installed, skipping"; \
	fi

# === Linting ===

lint: fmt-check vet ## Run all lint checks (format + vet)
	@echo "✓ All lint checks passed"

check: fmt-check vet staticcheck ## Run full static analysis suite
	@echo "✓ All checks passed"

# === Quality Gate ===

quality: check test-coverage ## Full quality gate (format, vet, staticcheck, tests + coverage)
	@echo ""
	@echo "=========================================="
	@echo "✓ All quality checks passed"
	@echo "=========================================="

# === MetaCI CLI ===

validate: build ## Validate pipeline file
	$(BINARY) validate examples/pipeline.tf

resolve: build ## Resolve pipeline (usage: make resolve CHANGED=auth_service)
	$(BINARY) resolve --changed $(CHANGED) examples/pipeline.tf

serve: build ## Start webhook server (usage: make serve ADDR=:8080 DIR=./examples)
	$(BINARY) serve --addr $(or $(ADDR),:8080) --dir $(or $(DIR),./examples)

# === Maintenance ===

tidy: ## Run go mod tidy
	$(GO) mod tidy

clean: ## Clean build artifacts
	rm -rf bin/
	rm -rf coverage/

full-cleanup: ## Remove all project dependencies and build artifacts
	@echo "=========================================="
	@echo "Full cleanup of MetaCI project"
	@echo "=========================================="
	@echo ""
	@echo "This will remove:"
	@echo "  - bin/ directory (built binary)"
	@echo "  - coverage/ directory (test reports)"
	@echo ""
	@read -p "Continue? [y/N]: " -n 1 -r; \
	echo; \
	if [ "$$REPLY" != "y" ] && [ "$$REPLY" != "Y" ]; then \
		echo "Cleanup cancelled."; \
		exit 1; \
	fi
	@echo ""
	@echo "Cleaning build artifacts..."
	@rm -rf bin/ || true
	@rm -rf coverage/ || true
	@$(GO) clean || true
	@echo ""
	@echo "=========================================="
	@echo "Cleanup complete!"
	@echo "=========================================="

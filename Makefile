# Makefile for MetaCI development tasks
# Provides convenient shortcuts for common development tasks

export HOME      := /home/hermes
export GOPATH    := /home/hermes/workspace/.gopath
export GOMODCACHE := /home/hermes/workspace/.gopath/pkg/mod
export GOCACHE   := /home/hermes/workspace/.gocache

BINARY    := bin/metaci
GO        := go
GOFLAGS   :=

.PHONY: help init build build-release test fmt fmt-check lint clean validate resolve vet tidy quality test-short full-cleanup

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

build-release: ## Build release version
	$(GO) build -ldflags="-s -w" -o $(BINARY) ./cmd/metaci

# === Quality ===

test: ## Run tests
	$(GO) test ./... -v

test-short: ## Run tests (short mode)
	$(GO) test ./... -short

fmt: ## Format code with gofmt
	$(GO) fmt ./...

fmt-check: ## Check code formatting without modifying files
	@test -z "$$(gofmt -l .)" || (echo "Files not formatted:"; gofmt -l .; exit 1)

lint: fmt-check ## Run linter checks (format + vet)
	$(GO) vet ./...

vet: ## Run go vet
	$(GO) vet ./...

quality: fmt-check lint test ## Run all quality checks (format, vet, test)

# === MetaCI CLI ===

validate: build ## Validate pipeline file
	$(BINARY) validate examples/pipeline.tf

resolve: build ## Resolve pipeline (usage: make resolve CHANGED=auth_service)
	$(BINARY) resolve --changed $(CHANGED) examples/pipeline.tf

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

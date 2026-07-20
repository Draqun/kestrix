# Makefile for MetaCI development tasks
# Provides convenient shortcuts for common development tasks

BINARY    := bin/metaci
GO        := go
GOFLAGS   :=

.PHONY: help init build build-release test test-coverage fmt fmt-check vet staticcheck lint clean clean-all quality check validate resolve serve install-tools

## help: Show this help message
help:
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@sed -n 's/^## //p' $(MAKEFILE_LIST) | column -t -s ':' 2>/dev/null || sed -n 's/^## //p' $(MAKEFILE_LIST)

## init: Bootstrap the project (install deps + build)
init: $(BINARY)

## build: Build the metaci binary
build: $(BINARY)

$(BINARY): $(shell find . -name '*.go' -type f 2>/dev/null) go.mod
	$(GO) mod tidy
	$(GO) build -o $(BINARY) ./cmd/metaci

## build-release: Build release binary (stripped)
build-release:
	$(GO) build -ldflags="-s -w" -o $(BINARY) ./cmd/metaci

## test: Run all tests
test:
	$(GO) test ./... -v

## test-coverage: Run tests with coverage report
test-coverage:
	@mkdir -p coverage
	$(GO) test ./... -coverprofile=coverage/coverage.out -covermode=atomic
	$(GO) tool cover -html=coverage/coverage.out -o coverage/coverage.html
	$(GO) tool cover -func=coverage/coverage.out

## fmt: Format code
fmt:
	$(GO) fmt ./...

## fmt-check: Check formatting
fmt-check:
	@echo "Checking formatting..."
	@test -z "$$(gofmt -l .)" || (echo "Files need formatting:" && gofmt -l . && exit 1)
	@echo "✓ Formatting OK"

## vet: Run go vet
vet:
	$(GO) vet ./...

## staticcheck: Run staticcheck
staticcheck:
	@command -v staticcheck >/dev/null 2>&1 || { echo "⚠ staticcheck not installed, run: go install honnef.co/go/tools/cmd/staticcheck@latest"; exit 0; }
	staticcheck ./...

## lint: Run all linters
lint: fmt-check vet staticcheck

## clean: Remove build artifacts
clean:
	rm -rf bin/ coverage/

## clean-all: Remove everything including downloaded deps
clean-all: clean
	$(GO) clean -cache -testcache

## quality: Full quality gate (format + vet + test + coverage)
quality: fmt-check vet test test-coverage
	@echo ""
	@echo "=========================================="
	@echo "✓ All quality checks passed"
	@echo "=========================================="

## check: Run all checks (alias for quality)
check: quality

## validate: Validate the example pipeline
validate: $(BINARY)
	$(BINARY) validate examples/pipeline.tf

## resolve: Resolve triggers for a set of changed repos
resolve: $(BINARY)
	$(BINARY) resolve $(if $(CHANGED),--changed $(CHANGED)) examples/pipeline.tf

## serve: Start the webhook server
serve: $(BINARY)
	$(BINARY) serve --addr $(or $(ADDR),:8080) --dir $(or $(DIR),./examples)

## dev: Start development server with hot reload (air)
dev:
	@command -v air >/dev/null 2>&1 || { echo "Installing air..."; go install github.com/air-verse/air@latest; }
	air -c .air.toml -- --addr $(or $(ADDR),:8080) --dir $(or $(DIR),./examples)

## install-tools: Install development tools
install-tools:
	$(GO) install honnef.co/go/tools/cmd/staticcheck@latest
	$(GO) install github.com/air-verse/air@latest

## ui-install: Install frontend dependencies
ui-install:
	cd ui && npm install

## ui-dev: Start frontend dev server (port 3000)
ui-dev: ui-install
	cd ui && npm run dev

## ui-build: Build frontend for production
ui-build: ui-install
	cd ui && npm run build

## docker-up: Start local Docker environment
docker-up:
	cd docker && make up-d

## docker-down: Stop local Docker environment
docker-down:
	cd docker && make down

## docker-logs: Follow Docker logs
docker-logs:
	cd docker && make logs

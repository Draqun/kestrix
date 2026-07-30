# Makefile for Kestrix development tasks
# Provides convenient shortcuts for common development tasks

BINARY    := bin/kestrix
GO        := go
GOFLAGS   :=

# Portable Go toolchain locations.
# Override with env vars if your system needs different paths; otherwise
# everything stays inside the repo so this Makefile works on any machine
# without touching the user's $HOME.
GOCACHE     ?= $(CURDIR)/.gocache
GOMODCACHE  ?= $(CURDIR)/.gomodcache
GOPATH      ?= $(CURDIR)/.gopath
GOBIN       ?= $(GOPATH)/bin
GOENV       := GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) GOPATH=$(GOPATH) GOBIN=$(GOBIN) PATH=$(GOBIN):$(PATH)

.PHONY: help init build build-release test test-coverage test-integration test-e2e runner-unit-coverage runner-integration web-e2e fmt fmt-check vet staticcheck lint clean clean-all quality check validate resolve pipeline serve dev dev-all install-tools ui-install ui-dev ui-build ui-serve docker-up docker-down docker-logs

## help: Show this help message
help:
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@sed -n 's/^## //p' $(MAKEFILE_LIST) | column -t -s ':' 2>/dev/null || sed -n 's/^## //p' $(MAKEFILE_LIST)

## init: Bootstrap the project (install deps + build)
init: $(BINARY)

## build: Build the kestrix binary
build: $(BINARY)

$(BINARY): $(shell find . -name '*.go' -type f 2>/dev/null) go.mod
	$(GOENV) $(GO) mod tidy
	$(GOENV) $(GO) build -o $(BINARY) ./cmd/kestrix

## build-release: Build release binary (stripped)
build-release:
	$(GOENV) $(GO) build -ldflags="-s -w" -o $(BINARY) ./cmd/kestrix

## test: Run unit tests (integration and e2e are separate targets)
test:
	unset DATABASE_URL; $(GOENV) bash scripts/test.sh unit

## test-one: Run all tests in a single package (use as `make test-one PKG=./internal/parser/`)
test-one:
	@test -n "$(PKG)" || (echo "Usage: make test-one PKG=./path/to/pkg"; exit 1)
	unset DATABASE_URL; $(GOENV) $(GO) test -v $(PKG)

## test-coverage: Run unit tests with coverage report
test-coverage:
	@mkdir -p coverage
	unset DATABASE_URL; $(GOENV) bash scripts/test.sh unit
	mv coverage.out coverage/coverage.out
	$(GOENV) $(GO) tool cover -html=coverage/coverage.out -o coverage/coverage.html
	$(GOENV) $(GO) tool cover -func=coverage/coverage.out

## test-integration: Run integration tests, including PostgreSQL and external runner regressions
test-integration:
	unset DATABASE_URL; $(GOENV) bash scripts/test.sh integration

## test-e2e: Run end-to-end webhook and dispatch regressions
test-e2e: web-e2e

## runner-unit-coverage: Run runner unit tests without external executors and report coverage
runner-unit-coverage:
	@mkdir -p coverage
	KESTRIX_UNIT_ONLY=1 $(GOENV) $(GO) test ./internal/runner -count=1 -coverprofile=coverage/runner-unit.out -covermode=atomic
	$(GOENV) $(GO) tool cover -func=coverage/runner-unit.out | tee coverage/runner-unit-summary.txt
	@awk '/^mode:/ { next } { n=$$2+0; if (n == 0) next; total+=n; if (($$3+0) > 0) covered+=n } END { if (total == 0) { print "FAIL: runner unit coverage profile is empty"; exit 1 } if (covered != total) { printf "FAIL: runner unit coverage is %d/%d statements, want exact 100%%\n", covered, total; exit 1 } }' coverage/runner-unit.out

## runner-integration: Run external runner regression tests without coverage accounting
runner-integration:
	KESTRIX_UNIT_ONLY=0 $(GOENV) $(GO) test ./internal/runner -count=1 -run 'TestRunExecutesDependenciesInOrder|TestRunSkipsManualTasks|TestRunRejectsTaskWithUntriggeredDependency|TestRunReturnsPerTaskResults|TestRunLocal_|TestRunOne_|TestRunGitLabRunner_CommandNotFound'

## web-e2e: Run critical webhook and dispatch regression tests without coverage accounting
web-e2e:
	$(GOENV) $(GO) test ./internal/web -count=1 -run 'TestFullWebhookFlow|TestProjectLifecycle|TestWebhookDispatchIntegration|TestWebhookNoDispatch|TestWebhookWrongSecret'

## fmt: Format code
fmt:
	$(GOENV) $(GO) fmt ./...

## fmt-check: Check formatting
fmt-check:
	@echo "Checking formatting..."
	@out=$$(gofmt -l $(shell find . -name '*.go' -not -path './.gomodcache/*' -not -path './.gocache/*' -not -path './.gopath/*' -not -path './node_modules/*' -not -path './ui/dist/*') 2>/dev/null); \
	if [ -n "$$out" ]; then echo "Files need formatting:"; echo "$$out"; exit 1; fi
	@echo "✓ Formatting OK"

## vet: Run go vet
vet:
	GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) GOPATH=$(GOPATH) GOBIN=$(GOBIN) PATH=$(GOBIN):$(PATH) $(GO) vet ./...

## staticcheck: Run staticcheck
staticcheck:
	@set -eu; \
	if [ ! -x "$(GOBIN)/staticcheck" ]; then \
		echo "Installing staticcheck..."; \
		$(GOENV) $(GO) install honnef.co/go/tools/cmd/staticcheck@latest; \
	else \
		want="$$($(GO) version | awk '{print $$3}')"; \
		have="$$($(GO) version -m "$(GOBIN)/staticcheck" 2>/dev/null | sed -n '1p' | awk -F: '{print $$2}' | xargs)"; \
		if [ "$$want" != "$$have" ]; then \
			echo "Rebuilding staticcheck ($$have -> $$want)..."; \
			$(GOENV) $(GO) install honnef.co/go/tools/cmd/staticcheck@latest; \
		fi; \
	fi
	GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) GOPATH=$(GOPATH) GOBIN=$(GOBIN) PATH=$(GOBIN):$(PATH) $(GOBIN)/staticcheck ./...

## lint: Run all linters
lint: fmt-check vet staticcheck

## clean: Remove build artifacts
clean:
	rm -rf bin/ coverage/ .gocache/ .gomodcache/ .gopath/

## clean-all: Remove everything including downloaded deps
clean-all: clean
	$(GO) clean -cache -testcache

## quality: Full quality gate (format + vet + test + coverage)
quality: fmt-check vet test
	@echo ""
	@echo "=========================================="
	@echo "✓ All quality checks passed"
	@echo "=========================================="

## check: Run all checks (alias for quality)
check: quality

## validate: Validate the example pipeline
validate: $(BINARY)
	$(BINARY) validate examples/demo-mono/pipeline.tf

## resolve: Resolve triggers for a set of changed repos
resolve: $(BINARY)
	$(BINARY) resolve $(if $(CHANGED),--changed $(CHANGED)) examples/demo-mono/pipeline.tf

## pipeline: Run this repository's Kestrix pipeline locally
pipeline: $(BINARY)
	$(BINARY) run --changed kestrix ci/pipeline.tf

## serve: Start the webhook server
serve: $(BINARY)
	$(BINARY) serve --addr $(or $(ADDR),:8080)

## dev: Start development server with hot reload (air)
dev:
	@command -v air >/dev/null 2>&1 || { echo "Installing air..."; $(GOENV) $(GO) install github.com/air-verse/air@v1.62.0; }
	PATH="$(GOBIN):$(PATH)" air -c .air.toml -- serve --addr $(or $(ADDR),:8080)

## dev-all: Start backend + frontend dev servers
dev-all:
	@command -v air >/dev/null 2>&1 || { echo "Installing air..."; $(GOENV) $(GO) install github.com/air-verse/air@v1.62.0; }
	@cd ui && npm install --silent
	@echo "Starting backend (air) on :8080 and frontend (vite) on :3000..."
	PATH="$(GOBIN):$(PATH)" air -c .air.toml -- serve --addr $(or $(ADDR),:8080) &
	@cd ui && npx vite --port 3000

## install-tools: Install development tools
install-tools:
	$(GOENV) $(GO) install honnef.co/go/tools/cmd/staticcheck@latest
	$(GOENV) $(GO) install github.com/air-verse/air@v1.62.0

## add-dep: Add a Go module dependency (use as `make add-dep DEP=github.com/foo/bar@latest`)
add-dep:
	@test -n "$(DEP)" || (echo "Usage: make add-dep DEP=<module>[@version]"; exit 1)
	$(GOENV) $(GO) get $(DEP)
	$(GOENV) $(GO) mod tidy

## ui-install: Install frontend dependencies
ui-install:
	cd ui && npm install

## ui-dev: Start frontend dev server (port 3000, proxies to :8080)
ui-dev: ui-install
	cd ui && npm run dev

## ui-build: Build frontend for production (ui/dist)
ui-build: ui-install
	cd ui && npm run build
	cp -r docs ui/dist/docs

## ui-serve: Build frontend + serve via Go (single binary, port 8080)
ui-serve: ui-build $(BINARY)
	$(BINARY) serve --addr $(or $(ADDR),:8080) --dir $(or $(DIR),./examples/demo-mono) --static $(or $(STATIC),./ui/dist)

## docker-up: Start local Docker environment
docker-up:
	cd docker && make up-d

## docker-down: Stop local Docker environment
docker-down:
	cd docker && make down

## docker-logs: Follow Docker logs
docker-logs:
	cd docker && make logs

## ci-validate-docs: Validate the presence of documentation after UI build
ci-validate-docs:
	@if [ -d ui/dist/docs ]; then echo "Documentation present"; else echo "Documentation missing"; exit 1; fi

## serve-docs: Serve documentation locally (after UI build)
serve-docs: ui-build
	@cd ui/dist && python3 -m http.server 8000

# ─────────────────────────────────────────────────────────────
# Kestrix — Docker Makefile
# Run from this directory: cd docker && make up
# ─────────────────────────────────────────────────────────────

COMPOSE := docker compose

.PHONY: help up up-d deps-up deps-up-d down restart logs rebuild clean ps shell

## help: Show this help message
help:
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@sed -n 's/^## //p' $(MAKEFILE_LIST) | column -t -s ':' 2>/dev/null || sed -n 's/^## //p' $(MAKEFILE_LIST)

## up: Start all services (foreground)
up:
	$(COMPOSE) up

## up-d: Start all services (background)
up-d:
	$(COMPOSE) up -d
	@echo ""
	@echo "Services started in background."
	@echo "  Kestrix:   http://localhost:8080"
	@echo "  Swagger:   http://localhost:8080/docs/"
	@echo "  Postgres:  localhost:5433"
	@echo "  Redis:     localhost:6380"
	@echo ""
	@echo "  make logs    — follow logs"
	@echo "  make down    — stop services"

## deps-up: Start development dependencies only (foreground)
deps-up:
	$(COMPOSE) up postgres redis

## deps-up-d: Start development dependencies only (background)
deps-up-d:
	$(COMPOSE) up -d postgres redis

## down: Stop and remove all services
down:
	$(COMPOSE) down

## restart: Restart all services
restart: down up

## logs: Follow logs from all services
logs:
	$(COMPOSE) logs -f

## logs-kestrix: Follow only Kestrix logs
logs-kestrix:
	$(COMPOSE) logs -f kestrix

## rebuild: Rebuild images and restart
rebuild:
	$(COMPOSE) build --no-cache
	$(COMPOSE) up -d

## clean: Stop services and remove volumes
clean:
	$(COMPOSE) down -v
	@echo "Volumes removed."

## ps: Show running services
ps:
	$(COMPOSE) ps

## shell: Open a shell in the Kestrix container
shell:
	$(COMPOSE) exec kestrix sh

## test-webhook: Send a test webhook to the local server
test-webhook:
	@echo "Sending test webhook..."
	@curl -s -X POST http://localhost:8080/webhook \
		-H "Content-Type: application/json" \
		-H "X-Gitlab-Token: $${KESTRIX_WEBHOOK_SECRET:-dev-secret}" \
		-d '{ \
			"object_kind": "push", \
			"ref": "refs/heads/main", \
			"project": { \
				"path_with_namespace": "src/auth", \
				"default_branch": "main" \
			}, \
			"commits": [{ \
				"id": "abc123", \
				"message": "fix handler", \
				"added": [], \
				"modified": ["handler.go"], \
				"removed": [] \
			}] \
		}' | python3 -m json.tool 2>/dev/null || echo "(install python3 for pretty output)"

## health: Check service health
health:
	@echo "Kestrix health:"
	@curl -s http://localhost:8080/api/health 2>/dev/null || echo "  Kestrix: not running"
	@echo ""
	@echo "Postgres:"
	@$(COMPOSE) exec -T postgres pg_isready -U kestrix 2>/dev/null || echo "  Postgres: not running"
	@echo ""
	@echo "Redis:"
	@$(COMPOSE) exec -T redis redis-cli ping 2>/dev/null || echo "  Redis: not running"

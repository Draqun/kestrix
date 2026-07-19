# ─────────────────────────────────────────────────────────────
# MetaCI — Docker Makefile
# Run from this directory: cd docker && make up
# ─────────────────────────────────────────────────────────────

COMPOSE := docker compose

.PHONY: help up down restart logs rebuild clean ps shell

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
	@echo "  MetaCI:    http://localhost:8080"
	@echo "  Swagger:   http://localhost:8080/docs/"
	@echo "  Postgres:  localhost:5432"
	@echo "  Redis:     localhost:6379"
	@echo ""
	@echo "  make logs    — follow logs"
	@echo "  make down    — stop services"

## down: Stop and remove all services
down:
	$(COMPOSE) down

## restart: Restart all services
restart: down up

## logs: Follow logs from all services
logs:
	$(COMPOSE) logs -f

## logs-metaci: Follow only MetaCI logs
logs-metaci:
	$(COMPOSE) logs -f metaci

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

## shell: Open a shell in the MetaCI container
shell:
	$(COMPOSE) exec metaci sh

## test-webhook: Send a test webhook to the local server
test-webhook:
	@echo "Sending test webhook..."
	@curl -s -X POST http://localhost:8080/webhook \
		-H "Content-Type: application/json" \
		-H "X-Gitlab-Token: $${METACI_WEBHOOK_SECRET:-dev-secret}" \
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
	@echo "MetaCI health:"
	@curl -s http://localhost:8080/health 2>/dev/null || echo "  MetaCI: not running"
	@echo ""
	@echo "Postgres:"
	@$(COMPOSE) exec -T postgres pg_isready -U metaci 2>/dev/null || echo "  Postgres: not running"
	@echo ""
	@echo "Redis:"
	@$(COMPOSE) exec -T redis redis-cli ping 2>/dev/null || echo "  Redis: not running"

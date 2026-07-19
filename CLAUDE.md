# CLAUDE.md - MetaCI Development Guide

## Project Overview

**MetaCI** is a repo-agnostic CI/CD control plane built in Go. It uses HCL for pipeline definitions, evaluates boolean trigger expressions (AND/OR/NOT), and dispatches to GitLab runners.

**TREAT the Makefile as source of truth** for all commands — never run tools directly.

## Quick Reference

```bash
make init          # Install deps + build
make test          # Run all tests
make quality       # fmt-check + vet + test + coverage
make dev           # Hot reload dev server (air)
make validate      # Validate example pipeline
make serve         # Start webhook server
make docker-up     # Full local Docker environment
```

## Architecture

```
cmd/metaci/           CLI entry point (resolve, validate, serve, variables)
internal/
  model/              Domain types (Project, Repo, Task, Variable)
  dag/                DAG engine + boolean expression parser
  parser/             HCL file parser (manual hclsyntax)
  variables/          GitLab-like variable store (scoped)
  web/                Webhook receiver + CI YAML generator + Chi server
  dispatch/           GitLab pipeline API client
docs/                 Auto-generated Swagger/OpenAPI
docker/               Dockerfiles + docker-compose
examples/             Example pipeline.tf files
openspec/             Proposal, specs, design, tasks
```

## Conventions

### Code Style
- **Line length:** 120 chars (golangci-lint gofmt)
- **Imports:** grouped as stdlib → external → internal
- **Error handling:** wrap errors with `fmt.Errorf("context: %w", err)`
- **Naming:** idiomatic Go (camelCase, exported PascalCase)

### Testing
- **TDD:** write tests first, then implement
- **Coverage:** target 80%+ per package
- **Test files:** `*_test.go` in same package
- **Table-driven tests:** use `[]struct{name, input, expected}` pattern
- **Mocking:** `net/http/httptest` for HTTP, interfaces for external deps

### HCL Syntax
- **Reserved words in HCL:** `type` is reserved — use `kind` instead
- **Block format:** multi-line blocks only (no single-line with 2+ attributes)
- **Trigger strings:** always quoted: `trigger = "repo.auth.changed"`

### Git
- **Branch:** `main`
- **Commits:** conventional format: `feat:`, `fix:`, `chore:`, `docs:`, `test:`
- **CI:** must pass before merge

### Docker
- **Production:** multi-stage with distroless base
- **Dev:** air hot reload + volume mounts
- **Compose:** profiles (--profile dev/all), health checks mandatory

## AI Guidelines

### When adding a new package
1. Create `internal/<name>/` with `<name>.go`
2. Add `<name>_test.go` with table-driven tests
3. Update Makefile if needed
4. Update README.md project structure

### When modifying the HCL parser
1. Check for reserved words (`type` → `kind`)
2. Use `attrsMap()` for attribute access
3. Test with both multi-line and edge cases
4. Run `make quality` before commit

### When adding a webhook feature
1. Add handler in `server.go`
2. Add swagger annotation with `@Summary`, `@Tags`, `@Param`
3. Add test in `*_test.go` using `httptest.NewServer`
4. Regenerate docs: `swag init -g cmd/metaci/main.go -o docs --parseInternal`

### When adding CLI commands
1. Add `cmd<Name>` function in `main.go`
2. Add to `usage()` string
3. Add to `switch os.Args[1]`
4. Add help text in `--help`

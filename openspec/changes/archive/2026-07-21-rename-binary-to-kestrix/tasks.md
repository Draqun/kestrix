## 1. Build system

- [x] 1.1 Rename `BINARY := bin/metaci` to `BINARY := bin/kestrix` in `Makefile`
- [x] 1.2 Update all Makefile targets that reference `bin/metaci` to use `$(BINARY)`
- [x] 1.3 Update Makefile help text and example commands to use `kestrix`

## 2. Docker and deployment

- [x] 2.1 Update `docker/Dockerfile` to build/copy the `kestrix` binary
- [x] 2.2 Update `docker/docker-compose.yml` and `docker/docker-compose.prod.yml` commands to run `kestrix`

## 3. Scripts and dogfood pipeline

- [x] 3.1 Update `scripts/*.sh` invocations from `./bin/metaci` to `./bin/kestrix`
- [x] 3.2 Update `ci/pipeline.tf` commands that invoke the binary (no binary invocations found)
- [x] 3.3 Update example commands in `ui/src/pages/Dashboard.jsx`

## 4. CLI identity and logs

- [x] 4.1 Update `cmd/kestrix/main.go` usage header to identify as Kestrix
- [x] 4.2 Update the server startup log in `internal/web/server.go` to identify as Kestrix
- [x] 4.3 Update the health endpoint `service` field from `"metaci"` to `"kestrix"`

## 5. Swagger/OpenAPI title

- [x] 5.1 Update `@title` annotation in `cmd/kestrix/main.go` from "MetaCI API" to "Kestrix API"
- [x] 5.2 Regenerate `docs/docs.go`, `docs/swagger.json`, and `docs/swagger.yaml` to reflect the new title
- [x] 5.3 Update contact name annotation from "MetaCI Team" to "Kestrix Team"

## 6. Documentation

- [x] 6.1 Update `AGENTS.md` build and run examples to use `bin/kestrix`
- [x] 6.2 Update any remaining `metaci` references in `README.md` or `docs/*.md` that refer to the binary

## 7. Verification

- [x] 7.1 Run `make build` and confirm `bin/kestrix` is produced
- [x] 7.2 Run `make test` and confirm all tests pass
- [x] 7.3 Run `make pipeline` and confirm dogfood pipeline completes
- [x] 7.4 Run `cd ui && npm run build` and confirm UI builds
- [x] 7.5 Start the server and confirm `/docs/swagger.json` shows `info.title: "Kestrix API"`
- [x] 7.6 Confirm server log line contains "Kestrix server"

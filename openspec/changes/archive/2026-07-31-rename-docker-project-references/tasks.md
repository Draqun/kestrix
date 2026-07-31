## 1. Docker Compose identity

- [x] 1.1 Rename project-facing application, Redis, and business-site service/container identifiers in `docker/docker-compose.yml` and `docker/docker-compose.prod.yml` to the Kestrix naming convention.
- [x] 1.2 Update Compose dependency references and comments while preserving ports, images, topology, PostgreSQL compatibility defaults, and `METACI_*` environment variables.

## 2. Docker tooling and initialization

- [x] 2.1 Update `docker/Makefile` help text, log targets, shell targets, health checks, and service commands to use the renamed Kestrix application service.
- [x] 2.2 Update `docker/postgres-init.sh` and Docker-local operational messages, retaining the existing PostgreSQL/test database compatibility names without embedding new secrets.

## 3. Verification and migration notes

- [x] 3.1 Search the `docker/` directory for unintended project-facing `metaci` references and confirm only explicitly preserved database/test-database and `METACI_*` compatibility names remain.
- [x] 3.2 Validate both Compose files and verify Makefile command references resolve to existing services without changing published ports.
- [x] 3.3 Document that existing containers and volumes using old names require explicit recreation or migration and are not deleted automatically.
- [x] 3.4 Run the relevant Docker configuration checks and record that the Kestrix naming requirements and compatibility scenarios pass.

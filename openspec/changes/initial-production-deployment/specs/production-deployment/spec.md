# Spec Delta: production-deployment

## ADDED Requirements

### Requirement: Production deployment health verification
The production deployment SHALL respond with HTTP 200 to `GET /api/health` and verify PostgreSQL connectivity upon container startup without requiring manual intervention.

#### Scenario: Automated health check on startup
- **WHEN** the production deployment is started via `docker compose -f docker/docker-compose.prod.yml up -d`
- **THEN** `GET /api/health` returns HTTP 200 within 30 seconds

### Requirement: First administrator bootstrap flow
The production deployment SHALL expose the `/api/setup` endpoint until the first administrator account is created, and SHALL lock down setup access immediately upon account persistence.

#### Scenario: Initial administrator creation
- **WHEN** an operator submits the initial administrator credentials on `/api/setup`
- **THEN** the administrator account is persisted in PostgreSQL
- **AND** subsequent requests to `/api/setup` are rejected

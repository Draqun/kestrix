## 1. Define isolated runtime configuration

- [x] 1.1 Set distinct local development database name, host port, and volume/container identities in `docker/docker-compose.yml`.
- [x] 1.2 Keep production database settings explicit and separate in `docker/docker-compose.prod.yml`; remove any shared resource naming that can collide on the same host.
- [x] 1.3 Update the local `serve` fallback and environment templates so host-run development uses the local database URL.

## 2. Verify isolation and migration safety

- [x] 2.1 Add tests or configuration checks proving local and deployment profiles resolve to different database identities and that explicit `DATABASE_URL` overrides the local fallback.
- [x] 2.2 Preserve and verify the `_test` database guardrails and update relevant documentation with the old regression source and data migration guidance.
- [x] 2.3 Run compose/config validation, formatting, targeted tests, and the relevant full test suite.

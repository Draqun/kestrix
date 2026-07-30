## 1. Compose static-site service

- [x] 1.1 Add a `business` nginx service to `docker/docker-compose.prod.yml`, mounting `ui/dist/business` read-only and exposing a configurable host port defaulting to `8082`.
- [x] 1.2 Add a minimal nginx configuration or equivalent defaults so SPA/static asset requests serve the built business site correctly.

## 2. Deployment script

- [x] 2.1 Update `scripts/deploy-business.sh` to use Docker Compose for local deployment, with configurable Compose file/service/port values and a clear missing-artifact failure.
- [x] 2.2 Preserve and verify the `DEPLOY_HOST` rsync branch, ensuring local Docker deployment is not attempted for remote targets.

## 3. Verification and documentation

- [x] 3.1 Add script/configuration checks covering source validation, local Compose invocation, remote rsync selection, and configurable defaults without requiring a privileged `/var/www` target.
- [x] 3.2 Update deployment documentation with build, deploy, URL, stop/cleanup, and environment override commands.
- [x] 3.3 Run the business build and local Docker deployment verification, then confirm the configured endpoint returns the built website content.

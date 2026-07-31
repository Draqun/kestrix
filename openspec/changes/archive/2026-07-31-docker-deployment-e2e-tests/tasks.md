## 1. Test harness and isolation

- [x] 1.1 Add a Bash Docker deployment E2E entrypoint using `set -euo pipefail`
- [x] 1.2 Add preflight checks for Docker, Docker Compose, `curl`, and required production Compose files
- [x] 1.3 Generate an isolated Compose project name, image tag, host port, and disposable environment without using production credentials or volumes

## 2. Deployment lifecycle

- [x] 2.1 Build the deployment image with the deterministic test tag, allowing an optional prebuilt image override
- [x] 2.2 Start the production Compose stack with the isolated project and test-specific environment
- [x] 2.3 Add bounded readiness polling for the deployed health endpoint
- [x] 2.4 Add failure diagnostics and a trap that removes only the isolated Compose resources on success, failure, or interruption

## 3. Black-box API verification

- [x] 3.1 Verify the external health endpoint status and JSON response shape
- [x] 3.2 Verify a database-backed projects API response against the disposable empty database
- [x] 3.3 Verify a database-backed runs API response and report failing URL/status details

## 4. Developer and CI integration

- [x] 4.1 Add a `make docker-e2e` target that invokes the test entrypoint
- [x] 4.2 Document prerequisites, configurable ports/timeouts, image override, and cleanup behavior
- [x] 4.3 Run the E2E test and verify both successful cleanup and actionable failure diagnostics

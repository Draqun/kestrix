## 1. Document the runtime hardening scope

- [x] 1.1 Confirm the composition root is owned by `cmd/kestrix/main.go`
- [x] 1.2 Confirm `web` no longer performs runtime fallback wiring
- [x] 1.3 Confirm webhook logging still flows through the server wiring

## 2. Verify service-level contracts

- [x] 2.1 Confirm `RunService` requires explicit task-runner/pipeline dependencies
- [x] 2.2 Confirm missing dependencies fail early and clearly
- [x] 2.3 Confirm `RunService` tests cover the tightened contract

## 3. Verify webhook hardening

- [x] 3.1 Confirm webhook log processing is nil-safe
- [x] 3.2 Confirm webhook logging wiring is preserved in server setup
- [x] 3.3 Confirm webhook tests cover the happy path and the no-sink path

## 4. Verify

- [x] 4.1 `go test ./... -count=1` passes; unit, integration, application E2E, and Docker deployment E2E checks passed
- [x] 4.2 `make lint` passes
- [x] 4.3 No remaining runtime fallback was discovered after wiring the webhook service in `NewServerWithDeps`

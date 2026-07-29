## 1. Document the runtime hardening scope

- [ ] 1.1 Confirm the composition root is owned by `cmd/kestrix/main.go`
- [ ] 1.2 Confirm `web` no longer performs runtime fallback wiring
- [ ] 1.3 Confirm webhook logging still flows through the server wiring

## 2. Verify service-level contracts

- [ ] 2.1 Confirm `RunService` requires explicit task-runner/pipeline dependencies
- [ ] 2.2 Confirm missing dependencies fail early and clearly
- [ ] 2.3 Confirm `RunService` tests cover the tightened contract

## 3. Verify webhook hardening

- [ ] 3.1 Confirm webhook log processing is nil-safe
- [ ] 3.2 Confirm webhook logging wiring is preserved in server setup
- [ ] 3.3 Confirm webhook tests cover the happy path and the no-sink path

## 4. Verify

- [ ] 4.1 `go test ./... -count=1` passes
- [ ] 4.2 `make lint` passes
- [ ] 4.3 Update OpenSpec notes if any remaining runtime fallback is discovered

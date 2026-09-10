## 1. Legacy IPv4 literal parsing

- [ ] 1.1 In `internal/service/service.go`, add `parseLegacyIPv4Literal(host string) (netip.Addr, bool)` implementing the `inet_aton`-style rule (1-4 dot-separated components, each decimal/octal(`0`-prefix)/hex(`0x`-prefix), last component absorbing remaining bits); verify a table-driven unit test covers `2130706433`, `0x7f000001`, `0177.0.0.1`, `127.1`, `10.0.0.5` (allowed, private), and a non-numeric host (`git.example.test`, must return `ok=false`) against their expected decoded address or rejection.
- [ ] 1.2 Call `parseLegacyIPv4Literal` from `isDisallowedRepoHost` only when `netip.ParseAddr` fails, and run its result through the same `IsLoopback`/`IsLinkLocalUnicast`/`IsUnspecified` checks the canonical path uses; verify a unit test asserts `isDisallowedRepoHost` returns `true` for every alternate-encoding form of `127.0.0.1` and `169.254.169.254` used in 1.1, and unchanged behavior (`false`) for `10.0.0.5` and ordinary hostnames.

## 2. End-to-end regression coverage

- [ ] 2.1 Add a `validateRepoURL`/project-create test asserting `https://2130706433/x.git` and `https://0x7f000001/x.git` are rejected with the same `400` and error text as `https://127.0.0.1/x.git`.
- [ ] 2.2 Run `go build ./... && go vet ./...` and `go test ./internal/service/...`; confirm the package's existing coverage gate (`runtime-secrets`'s exclusion aside) still passes.

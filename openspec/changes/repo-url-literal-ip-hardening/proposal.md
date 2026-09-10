## Why

`webhook-ingress-hardening` added `isDisallowedRepoHost`
(`internal/service/service.go:172-183`) to reject a `repo_url` naming a
loopback, link-local, or unspecified address, closing the obvious SSRF path
to the cloud metadata endpoint (`169.254.169.254`). The check calls
`netip.ParseAddr(h)` on the URL's host and treats a parse failure as "not an
IP, therefore not disallowed" — the fall-through `validateRepoURL` needs for
ordinary hostnames. `netip.ParseAddr` only accepts the canonical dotted-quad
form; it rejects the decimal (`2130706433`), hexadecimal (`0x7f000001`), and
short (`127.1`) notations for the same address, even though `git`'s
libcurl-based HTTP transport resolves them as literal IPs. A `repo_url` of
`https://2852039166/x.git` (decimal for `169.254.169.254`) or
`https://0x7f000001/x.git` passes today's validation and defeats the check
`webhook-ingress-hardening`'s own design doc calls "the realistic target of
this mitigation."

## What Changes

- `isDisallowedRepoHost` recognizes alternate IPv4 literal encodings
  (decimal, hex, octal, short-form) before falling through to "allowed",
  either by normalizing the host through a parser that accepts them or by
  rejecting an all-digit/hex-looking host component outright.
- Existing unit tests for `validateRepoURL`/`isDisallowedRepoHost` gain cases
  for each alternate encoding of `127.0.0.1` and `169.254.169.254`.

## Non-goals

- IPv6 zone-ID or embedded-IPv4 edge cases beyond what the existing
  `netip.ParseAddr` path already covers for canonical IPv6 literals.
- DNS-rebinding protection (resolving a hostname at fetch time to catch a
  hostname that later resolves to a disallowed address) — `design.md` of
  `webhook-ingress-hardening` already scoped that out as a non-goal, and
  nothing here changes that scope.
- Changing which address ranges are disallowed; only closing the encoding
  bypass of the existing loopback/link-local/unspecified check.

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `project-registration`: the `repo_url` SSRF-hardening requirement must
  reject alternate literal-IP encodings of a disallowed host, not only the
  canonical dotted-quad form.

## Impact

- `internal/service/service.go`: `isDisallowedRepoHost` (and/or
  `validateRepoURL` that calls it) gains a normalization or rejection step.
- No API shape change; a `repo_url` that previously passed with an
  alternate-encoding loopback/link-local literal now returns the same `400`
  it would for the canonical form.

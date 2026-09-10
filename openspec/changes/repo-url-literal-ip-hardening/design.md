## Context

See proposal.md — Why.

```
repo_url ──► url.Parse ──► host ──► isDisallowedRepoHost(host) ──► accept/reject
                                          │
                                          ▼
                                 netip.ParseAddr(host)
                                   success → check ranges
                                   failure → "not an IP" → ALLOWED  ← the bug
```

- `netip.ParseAddr` (and `net.ParseIP`) deliberately accept only the
  canonical dotted-decimal / colon-hex forms — this is itself a Go security
  fix from Go 1.17 (`net`: "reject non-canonical IPv4 addresses"), aimed at
  exactly this class of ambiguous-parsing bug. It is the right choice for a
  library that must agree with every other consumer on what an address
  means; it is the wrong signal to read as "therefore not an IP" here,
  because `git`'s HTTP transport (libcurl) parses hostnames the way
  `inet_aton(3)` does: any of `a`, `a.b`, `a.b.c`, or `a.b.c.d`, each
  component decimal, `0`-prefixed octal, or `0x`-prefixed hex, with the last
  component absorbing the remaining bits.
- `isDisallowedRepoHost` (`internal/service/service.go:172-183`) is called
  from `validateRepoURL`, itself called from project create and update
  (`service.go:56-73`, `216`, `277`) — one call site to fix.

## Goals / Non-Goals

**Goals:** a host component that `git`'s own HTTP transport would resolve to
a disallowed literal IP is rejected, regardless of which of the four
`inet_aton`-style notations it uses.

**Non-Goals:**
- DNS-rebinding protection (resolving a hostname at fetch time to catch one
  that later resolves to a disallowed address) — out of scope per
  `webhook-ingress-hardening`'s own design, unchanged here.
- IPv6 alternate-encoding forms beyond what `netip.ParseAddr` already
  canonicalizes (IPv6 has no equivalent decimal/octal ambiguity — every
  library agrees on hex-colon notation).

## Decisions

**D1: Add a small `inet_aton`-style parser (`parseLegacyIPv4Literal`) that
runs only when `netip.ParseAddr` fails, converts a 1-to-4-component
decimal/octal/hex host into a 32-bit address the same way `inet_aton` does,
and feeds the result through the same `IsLoopback`/`IsLinkLocalUnicast`/
`IsUnspecified` checks the canonical path already uses.**
Alternative considered: reject outright any host that is not a valid DNS
name and not a canonical IP (i.e., treat "looks numeric" as disallowed
without computing what it means). Rejected — simpler, but a host like
`0x1.example.test` (a real subdomain label starting with digits) would never
trigger this, so precision costs little; the false-positive risk of a blunt
numeric-shape rejection was judged higher value to avoid than the extra ~25
lines of a focused parser, and a focused parser produces a clearer error
message ("resolves to a disallowed address" vs. "host looks suspicious").

**D2: Keep the fix inside `internal/service/service.go`; do not introduce a
dependency on a URL/IP-parsing library.** The `inet_aton` algorithm is short
enough that a stdlib-only implementation (`strconv.ParseUint` per component)
is smaller and more auditable than pulling in a third-party parser for one
function.

## Security Considerations

This closes an SSRF bypass: without the fix, a `repo_url` naming the cloud
metadata endpoint in decimal or hex form reaches `git`'s HTTP transport
un-rejected, exactly the outcome `webhook-ingress-hardening` set out to
prevent. The new parser only ever narrows what is accepted — it cannot make
a previously-rejected host newly acceptable — so the change is safe to ship
without a feature flag.

## Migration Plan

No data migration. A `repo_url` already stored with a canonical-form
disallowed host was already rejected at creation time and cannot exist; a
`repo_url` stored with a legacy-encoded literal (if one somehow exists from
before this fix) will now be rejected on the next update attempt, not
retroactively — this change only guards create/update, matching the
existing requirement's scope. Rollback is a plain revert.

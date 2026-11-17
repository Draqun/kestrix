package service

import (
	"fmt"
	"net/netip"
	"net/url"
	"strconv"
	"strings"

	"kestrix/internal/source"
)

// validateRepoURL accepts only an https:// URL, an ssh:// URL, or an
// scp-like [user@]host:path address, and rejects a host that is a literal
// loopback, link-local, or unspecified address. See
// webhook-ingress-hardening design.md D6. The checks are syntactic only -
// no DNS resolution and no network access.
func validateRepoURL(raw string) error {
	if err := validateRepoURLForm(raw); err != nil {
		return err
	}
	// The same key routes webhooks and identifies the primary source; a URL
	// without one (no repository path) would register but never be reached.
	if _, err := source.SourceMatchKey(raw); err != nil {
		return fmt.Errorf("%w: repo_url must name a repository path", ErrValidation)
	}
	return nil
}

// validateRepoURLForm checks the scheme, address form, and host of raw.
func validateRepoURLForm(raw string) error {
	if host, _, ok := splitSCPLikeAddress(raw); ok {
		if isDisallowedRepoHost(host) {
			return fmt.Errorf("%w: repo_url host is not allowed", ErrValidation)
		}
		return nil
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%w: repo_url is invalid", ErrValidation)
	}
	switch parsed.Scheme {
	case "https", "ssh":
	default:
		return fmt.Errorf("%w: repo_url scheme is not allowed", ErrValidation)
	}
	if parsed.User != nil {
		_, hasPassword := parsed.User.Password()
		if parsed.Scheme != "ssh" || hasPassword {
			return fmt.Errorf("%w: repo_url must not contain embedded credentials", ErrValidation)
		}
	}
	if isDisallowedRepoHost(parsed.Hostname()) {
		return fmt.Errorf("%w: repo_url host is not allowed", ErrValidation)
	}
	return nil
}

// splitSCPLikeAddress recognizes the scp-like "[user@]host:path" form: no
// "://" anywhere, and the first ":" precedes any "/". A double colon (as in
// the "ext::" transport helper) is rejected by requiring a non-empty path
// that does not itself start with ":".
func splitSCPLikeAddress(raw string) (host, path string, ok bool) {
	if strings.Contains(raw, "://") {
		return "", "", false
	}
	firstColon := strings.Index(raw, ":")
	if firstColon < 0 {
		return "", "", false
	}
	if firstSlash := strings.Index(raw, "/"); firstSlash >= 0 && firstColon > firstSlash {
		return "", "", false
	}
	rest := raw
	if at := strings.Index(raw[:firstColon], "@"); at >= 0 {
		rest = raw[at+1:]
	}
	// rest still contains raw's first colon (any "@" found above precedes
	// it), so this always finds one.
	colonIdx := strings.Index(rest, ":")
	host = rest[:colonIdx]
	path = rest[colonIdx+1:]
	if host == "" || path == "" || strings.HasPrefix(path, ":") {
		return "", "", false
	}
	return host, path, true
}

// isDisallowedRepoHost reports whether host is a literal loopback,
// link-local, or unspecified address, or the name "localhost". A hostname
// that is none of those is allowed without resolving it - see D6.
func isDisallowedRepoHost(host string) bool {
	h := strings.Trim(host, "[]")
	if strings.EqualFold(h, "localhost") {
		return true
	}
	addr, err := netip.ParseAddr(h)
	if err == nil {
		return addr.IsLoopback() || addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() || addr.IsUnspecified()
	}
	// netip.ParseAddr accepts only the canonical dotted-quad form; git's
	// HTTP transport (libcurl) resolves inet_aton-style literals (decimal,
	// octal, hex, short-form) as IPs too, so a legacy encoding of a
	// disallowed address must not fall through to "allowed" - see
	// repo-url-literal-ip-hardening design.md D1.
	legacy, ok := parseLegacyIPv4Literal(h)
	if !ok {
		return false
	}
	return legacy.IsLoopback() || legacy.IsLinkLocalUnicast() || legacy.IsLinkLocalMulticast() || legacy.IsUnspecified()
}

// parseLegacyIPv4Literal decodes an inet_aton-style IPv4 literal: 1-4
// dot-separated components, each decimal, 0-prefixed octal, or 0x-prefixed
// hex, with the last component absorbing the remaining bits (1 part = 32
// bits, 2 parts = 8+24, 3 parts = 8+8+16, 4 parts = 8+8+8+8). It reports
// ok=false for ordinary hostnames and malformed literals.
func parseLegacyIPv4Literal(host string) (netip.Addr, bool) {
	if host == "" {
		return netip.Addr{}, false
	}
	parts := strings.Split(host, ".")
	if len(parts) > 4 {
		return netip.Addr{}, false
	}
	vals := make([]uint64, len(parts))
	for i, p := range parts {
		if p == "" {
			return netip.Addr{}, false
		}
		base := 10
		s := p
		if len(p) >= 2 && p[0] == '0' && (p[1] == 'x' || p[1] == 'X') {
			base = 16
			s = p[2:]
			if s == "" || !isHexDigits(s) {
				return netip.Addr{}, false
			}
		} else if len(p) > 1 && p[0] == '0' {
			base = 8
			if !isOctalDigits(s) {
				return netip.Addr{}, false
			}
		} else {
			if !isDecimalDigits(s) {
				return netip.Addr{}, false
			}
		}
		v, err := strconv.ParseUint(s, base, 32)
		if err != nil {
			return netip.Addr{}, false
		}
		vals[i] = v
	}
	var b [4]byte
	switch len(vals) {
	case 1:
		v := vals[0]
		b[0] = byte(v >> 24)
		b[1] = byte(v >> 16)
		b[2] = byte(v >> 8)
		b[3] = byte(v)
	case 2:
		if vals[0] > 0xFF || vals[1] > 0xFFFFFF {
			return netip.Addr{}, false
		}
		b[0] = byte(vals[0])
		b[1] = byte(vals[1] >> 16)
		b[2] = byte(vals[1] >> 8)
		b[3] = byte(vals[1])
	case 3:
		if vals[0] > 0xFF || vals[1] > 0xFF || vals[2] > 0xFFFF {
			return netip.Addr{}, false
		}
		b[0] = byte(vals[0])
		b[1] = byte(vals[1])
		b[2] = byte(vals[2] >> 8)
		b[3] = byte(vals[2])
	case 4:
		for j, v := range vals {
			if v > 0xFF {
				return netip.Addr{}, false
			}
			b[j] = byte(v)
		}
	}
	return netip.AddrFrom4(b), true
}

func isDecimalDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return len(s) > 0
}

func isOctalDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '7' {
			return false
		}
	}
	return len(s) > 0
}

func isHexDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return len(s) > 0
}

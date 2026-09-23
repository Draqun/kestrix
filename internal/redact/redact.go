// Package redact holds the byte-string secret-redaction primitive shared by
// run output (internal/service) and process log fields (internal/web,
// internal/service, internal/store). It lives below all three so none of
// them has to depend on another to reuse it - internal/store in particular
// must not import internal/service, which sits above it in this repo's
// hexagonal layering (see hexagonal-ports spec.md).
package redact

import (
	"sort"
	"strings"
)

// MinSecretLen is the shortest value Values will ever redact. Redacting "",
// "1", or "ok" would shred unrelated text, so a shorter value is still
// usable elsewhere but is not guaranteed to be redacted here - documented,
// not a bug; see internal/service/redact.go's original design.md D5, which
// this package continues to follow.
const MinSecretLen = 8

// Prepare drops any value shorter than MinSecretLen and deduplicates and
// sorts the rest longest-first, so a longer secret is fully redacted before
// a shorter one that happens to be its substring is considered - otherwise
// part of the longer secret's bytes would stay exposed after the shorter
// match consumed only part of it. Exported so a caller that redacts many
// times against the same value set (redactingWriter, streaming run output)
// can prepare it once instead of on every call, the way Values does
// internally for a one-shot string.
func Prepare(values []string) []string {
	seen := make(map[string]bool, len(values))
	filtered := make([]string, 0, len(values))
	for _, v := range values {
		if len(v) < MinSecretLen || seen[v] {
			continue
		}
		seen[v] = true
		filtered = append(filtered, v)
	}
	sort.Slice(filtered, func(i, j int) bool { return len(filtered[i]) > len(filtered[j]) })
	return filtered
}

// Values replaces every occurrence of each value at least MinSecretLen long
// in s with "***". Safe to call with a nil or empty values slice (a no-op)
// or an empty/known-safe s.
func Values(s string, values []string) string {
	return ValuesPrepared(s, Prepare(values))
}

// ValuesPrepared is Values for a caller that already has a Prepare-d list
// (already filtered, sorted longest-first is no longer required by this
// implementation but harmless), so it is not redundantly re-prepared on
// every call.
//
// Matches are found against the original s for every value independently,
// then merged into disjoint byte ranges before a single replacement pass -
// unlike a sequential strings.ReplaceAll per value, this also redacts two
// secrets that overlap without either containing the other (e.g. values
// "abcdefgh" and "efghijkl" against "abcdefghijkl"): replacing the first
// found would consume the shared bytes and hide the second from a later
// ReplaceAll pass, leaving part of it in plaintext - see REVIEW.md R3.
func ValuesPrepared(s string, prepared []string) string {
	if s == "" || len(prepared) == 0 {
		return s
	}
	type span struct{ start, end int }
	var spans []span
	for _, v := range prepared {
		for start := 0; ; {
			idx := strings.Index(s[start:], v)
			if idx < 0 {
				break
			}
			absStart := start + idx
			spans = append(spans, span{absStart, absStart + len(v)})
			start = absStart + 1
		}
	}
	if len(spans) == 0 {
		return s
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].start < spans[j].start })
	merged := spans[:1]
	for _, sp := range spans[1:] {
		last := &merged[len(merged)-1]
		if sp.start > last.end {
			merged = append(merged, sp)
			continue
		}
		if sp.end > last.end {
			last.end = sp.end
		}
	}

	var b strings.Builder
	b.Grow(len(s))
	pos := 0
	for _, sp := range merged {
		b.WriteString(s[pos:sp.start])
		b.WriteString("***")
		pos = sp.end
	}
	b.WriteString(s[pos:])
	return b.String()
}

// Err redacts err's message against values, returning "" for a nil err. A
// log call site with no known secret value in scope passes a nil or empty
// values slice, which makes this a plain err.Error() - still routed through
// the same function so every err-carrying log call follows one pattern; see
// observability design.md's "Redaction misses a field" risk.
func Err(err error, values []string) string {
	if err == nil {
		return ""
	}
	return Values(err.Error(), values)
}

package store

import (
	"strings"
	"unicode/utf8"
)

// storable returns s sanitized so PostgreSQL TEXT and JSONB columns accept it:
// every run of bytes that are not valid UTF-8, and every NUL byte, becomes a single
// replacement character U+FFFD (design.md D2). Clean text is returned as it is,
// without allocating a copy.
func storable(s string) string {
	if s == "" {
		return ""
	}
	if utf8.ValidString(s) && strings.IndexByte(s, 0) < 0 {
		return s
	}
	return strings.ToValidUTF8(strings.ReplaceAll(s, "\x00", "\xff"), "\uFFFD")
}

// cleanTaskResult returns a copy of tr with Output and Error passed through storable (design.md D3).
func cleanTaskResult(tr TaskResult) TaskResult {
	tr.Output = storable(tr.Output)
	tr.Error = storable(tr.Error)
	return tr
}

// cleanTaskResults returns a new slice of TaskResults with each result's Output and
// Error passed through storable (design.md D3). The caller's slice is not modified.
func cleanTaskResults(results []TaskResult) []TaskResult {
	if len(results) == 0 {
		return results
	}
	out := make([]TaskResult, len(results))
	for i, tr := range results {
		out[i] = cleanTaskResult(tr)
	}
	return out
}

package service

import (
	"io"
	"sort"
	"strings"
)

// minRedactedSecretLen is the shortest resolved secret value redactingWriter
// will ever match; see design.md D5. Redacting "", "1", or "ok" would shred
// unrelated output, so a shorter value is still injected into a task but is
// not guaranteed to be redacted from output - documented, not a bug.
const minRedactedSecretLen = 8

// redactingWriter wraps an io.Writer, replacing every occurrence of a
// configured secret value with "***" before the bytes reach the underlying
// writer. It buffers up to maxLen-1 trailing bytes across Write calls so a
// value split across two writes is still matched; call Flush once no more
// data is coming so a value sitting in that trailing buffer is redacted
// before being lost. See design.md D5: this is a byte-string match on the
// raw value, not redaction that survives encoding.
type redactingWriter struct {
	w       io.Writer
	values  []string // len >= minRedactedSecretLen, longest first
	maxLen  int
	pending []byte
}

// prepareRedactionValues drops any value shorter than minRedactedSecretLen
// and deduplicates and sorts the rest longest-first, so a longer secret is
// fully redacted before a shorter one that happens to be its substring is
// considered - otherwise part of the longer secret's bytes would stay
// exposed after the shorter match consumed only part of it.
func prepareRedactionValues(values []string) []string {
	seen := make(map[string]bool, len(values))
	filtered := make([]string, 0, len(values))
	for _, v := range values {
		if len(v) < minRedactedSecretLen || seen[v] {
			continue
		}
		seen[v] = true
		filtered = append(filtered, v)
	}
	sort.Slice(filtered, func(i, j int) bool { return len(filtered[i]) > len(filtered[j]) })
	return filtered
}

// newRedactingWriter builds a redactingWriter over w from values (see
// prepareRedactionValues).
func newRedactingWriter(w io.Writer, values []string) *redactingWriter {
	filtered := prepareRedactionValues(values)
	maxLen := 0
	for _, v := range filtered {
		if len(v) > maxLen {
			maxLen = len(v)
		}
	}
	return &redactingWriter{w: w, values: filtered, maxLen: maxLen}
}

// redactValues replaces every occurrence of each configured secret value
// (see prepareRedactionValues) in s with "***". Used where the text is
// already a complete string - TaskResult.Output and ApplyTaskResult's
// outputExtra - so no cross-write buffering is needed.
func redactValues(s string, values []string) string {
	for _, v := range prepareRedactionValues(values) {
		s = strings.ReplaceAll(s, v, "***")
	}
	return s
}

// Write implements io.Writer. It always reports len(p) consumed - any
// buffering delay in reaching the underlying writer is internal.
func (r *redactingWriter) Write(p []byte) (int, error) {
	n := len(p)
	if len(r.values) == 0 {
		_, err := r.w.Write(p)
		return n, err
	}
	buf := r.redact(append(r.pending, p...))
	// keep is never negative here: the len(r.values) == 0 fast path above
	// already covers an empty configured set, so maxLen is always at least
	// minRedactedSecretLen whenever this line runs.
	keep := r.maxLen - 1
	if keep > len(buf) {
		keep = len(buf)
	}
	flush := buf[:len(buf)-keep]
	r.pending = append([]byte(nil), buf[len(buf)-keep:]...)
	if len(flush) == 0 {
		return n, nil
	}
	_, err := r.w.Write(flush)
	return n, err
}

// Flush redacts and forwards any bytes still held back for a possible
// cross-write match. Call it once no further data is coming - see D5's
// "call Flush() in finishRun before persisting".
func (r *redactingWriter) Flush() error {
	if len(r.pending) == 0 {
		return nil
	}
	out := r.redact(r.pending)
	r.pending = nil
	_, err := r.w.Write(out)
	return err
}

func (r *redactingWriter) redact(buf []byte) []byte {
	s := string(buf)
	for _, v := range r.values {
		s = strings.ReplaceAll(s, v, "***")
	}
	return []byte(s)
}

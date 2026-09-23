package service

import (
	"io"

	"kestrix/internal/redact"
)

// redactingWriter wraps an io.Writer, replacing every occurrence of a
// configured secret value with "***" before the bytes reach the underlying
// writer. It buffers up to maxLen-1 trailing bytes across Write calls so a
// value split across two writes is still matched; call Flush once no more
// data is coming so a value sitting in that trailing buffer is redacted
// before being lost. See design.md D5: this is a byte-string match on the
// raw value, not redaction that survives encoding. The matching itself
// lives in internal/redact, shared with the process log fields internal/web
// and internal/store redact - see that package's doc comment for why it
// sits below internal/service.
type redactingWriter struct {
	w       io.Writer
	values  []string // len >= redact.MinSecretLen, longest first
	maxLen  int
	pending []byte
}

// newRedactingWriter builds a redactingWriter over w from values, dropping
// any value shorter than redact.MinSecretLen and deduplicating and sorting
// the rest longest-first (see redact.Prepare).
func newRedactingWriter(w io.Writer, values []string) *redactingWriter {
	filtered := redact.Prepare(values)
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
	return redact.Values(s, values)
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
	// redact.MinSecretLen whenever this line runs.
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
// cross-write match, then flushes the underlying writer if it has its own
// Flush (for example boundedWriter's truncation marker) - call it once no
// further data is coming; see D5's "call Flush() in finishRun before
// persisting" and run-concurrency-and-retention design.md D4.
func (r *redactingWriter) Flush() error {
	if len(r.pending) != 0 {
		out := r.redact(r.pending)
		r.pending = nil
		if _, err := r.w.Write(out); err != nil {
			return err
		}
	}
	if f, ok := r.w.(interface{ Flush() error }); ok {
		return f.Flush()
	}
	return nil
}

func (r *redactingWriter) redact(buf []byte) []byte {
	// r.values is already Prepare-d (see newRedactingWriter), so this uses
	// ValuesPrepared rather than re-filtering/re-sorting on every Write.
	return []byte(redact.ValuesPrepared(string(buf), r.values))
}

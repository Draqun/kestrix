package redact

import "io"

// Writer wraps an io.Writer, replacing every occurrence of a
// configured secret value with "***" before the bytes reach the underlying
// writer. It buffers up to maxLen-1 trailing bytes across Write calls so a
// value split across two writes is still matched; call Flush once no more
// data is coming so a value sitting in that trailing buffer is redacted
// before being lost. See runtime-secrets design.md D5: this is a byte-string match on the
// raw value, not redaction that survives encoding. The matching itself
// is ValuesPrepared (redact.go). It lives here rather than in
// internal/service so `kestrix runner` can redact its streamed trace without
// importing the service layer (remote-runner-fixes design.md D1).
type Writer struct {
	w       io.Writer
	values  []string // len >= MinSecretLen, longest first
	maxLen  int
	pending []byte
}

// NewWriter builds a Writer over w from values, dropping
// any value shorter than MinSecretLen and deduplicating and sorting
// the rest longest-first (see Prepare).
func NewWriter(w io.Writer, values []string) *Writer {
	filtered := Prepare(values)
	maxLen := 0
	for _, v := range filtered {
		if len(v) > maxLen {
			maxLen = len(v)
		}
	}
	return &Writer{w: w, values: filtered, maxLen: maxLen}
}

// Write implements io.Writer. It always reports len(p) consumed - any
// buffering delay in reaching the underlying writer is internal.
func (r *Writer) Write(p []byte) (int, error) {
	n := len(p)
	if len(r.values) == 0 {
		_, err := r.w.Write(p)
		return n, err
	}
	buf := r.redact(append(r.pending, p...))
	// keep is never negative here: the len(r.values) == 0 fast path above
	// already covers an empty configured set, so maxLen is always at least
	// MinSecretLen whenever this line runs.
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
func (r *Writer) Flush() error {
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

func (r *Writer) redact(buf []byte) []byte {
	// r.values is already Prepare-d (see NewWriter), so this uses
	// ValuesPrepared rather than re-filtering/re-sorting on every Write.
	return []byte(ValuesPrepared(string(buf), r.values))
}

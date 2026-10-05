package service

import (
	"io"

	"kestrix/internal/domain"
)

// boundedWriter wraps an io.Writer, forwarding at most max bytes and
// discarding the rest; see run-concurrency-and-retention design.md D4. It
// always reports len(p) consumed, matching redactingWriter's contract. Flush
// appends a marker line once, only if truncation actually happened, naming
// the bound so an operator knows how to raise it.
//
// This is output-size bounding, a separate concern from redact.go's
// secret-redaction writer; it lives in its own file so a reader looking for
// output-bounding logic has a file named for it - see REVIEW.md R15.
type boundedWriter struct {
	w         io.Writer
	max       int
	written   int
	truncated bool
}

// newBoundedWriter builds a boundedWriter over w. A non-positive max means
// unbounded (no truncation).
func newBoundedWriter(w io.Writer, max int) *boundedWriter {
	return &boundedWriter{w: w, max: max}
}

func (b *boundedWriter) Write(p []byte) (int, error) {
	n := len(p)
	if b.max <= 0 {
		_, err := b.w.Write(p)
		return n, err
	}
	if b.written >= b.max {
		b.truncated = true
		return n, nil
	}
	remaining := b.max - b.written
	if remaining >= len(p) {
		_, err := b.w.Write(p)
		b.written += len(p)
		return n, err
	}
	_, err := b.w.Write(p[:remaining])
	b.written += remaining
	b.truncated = true
	return n, err
}

// Flush appends the truncation marker line once, only if Write ever dropped
// bytes.
func (b *boundedWriter) Flush() error {
	if !b.truncated {
		return nil
	}
	_, err := io.WriteString(b.w, domain.TruncationMarker(b.max))
	return err
}

// truncateOutput bounds a complete string to max bytes, appending
// domain.TruncationMarker - the same line boundedWriter.Flush (streaming run
// output) and remote output end with. Unlike boundedWriter, which wraps a
// live stream, TaskResult.Output arrives from task runners as a single
// already-complete buffer built independently of the run's aggregate output
// stream - see REVIEW.md R1. A non-positive max means unbounded.
func truncateOutput(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max] + domain.TruncationMarker(max)
}

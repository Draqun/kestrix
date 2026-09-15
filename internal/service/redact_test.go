package service

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestRedactingWriter_ValueInOneWrite(t *testing.T) {
	var buf bytes.Buffer
	w := newRedactingWriter(&buf, []string{"super-secret-value"})
	if _, err := w.Write([]byte("token=super-secret-value done")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if got := buf.String(); got != "token=*** done" {
		t.Fatalf("output = %q, want token=*** done", got)
	}
}

func TestRedactingWriter_ValueSplitAcrossWrites(t *testing.T) {
	var buf bytes.Buffer
	secret := "super-secret-value"
	w := newRedactingWriter(&buf, []string{secret})
	half := len(secret) / 2
	if _, err := w.Write([]byte("token=" + secret[:half])); err != nil {
		t.Fatalf("Write 1: %v", err)
	}
	if _, err := w.Write([]byte(secret[half:] + " done")); err != nil {
		t.Fatalf("Write 2: %v", err)
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if got := buf.String(); got != "token=*** done" {
		t.Fatalf("output = %q, want token=*** done", got)
	}
}

func TestRedactingWriter_OverlappingValuesLongestFirst(t *testing.T) {
	var buf bytes.Buffer
	// "password123extra" contains "password123" as a prefix. Redacting the
	// longer value first must not leave "extra" exposed.
	w := newRedactingWriter(&buf, []string{"password123", "password123extra"})
	if _, err := w.Write([]byte("value=password123extra!")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if got := buf.String(); got != "value=***!" {
		t.Fatalf("output = %q, want value=***!", got)
	}
}

func TestRedactingWriter_ShortValueUntouched(t *testing.T) {
	var buf bytes.Buffer
	w := newRedactingWriter(&buf, []string{"short"}) // 5 bytes, under the 8-byte floor
	if _, err := w.Write([]byte("value=short done")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if got := buf.String(); got != "value=short done" {
		t.Fatalf("output = %q, want the short value left untouched", got)
	}
}

func TestRedactingWriter_EmptyConfiguredValues(t *testing.T) {
	var buf bytes.Buffer
	w := newRedactingWriter(&buf, nil)
	if _, err := w.Write([]byte("plain output")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if got := buf.String(); got != "plain output" {
		t.Fatalf("output = %q, want plain output unchanged", got)
	}
}

// TestRedactingWriter_FlushesUnderlyingBoundedWriter lives here (not in
// bounded_writer_test.go) because it exercises redactingWriter.Flush's own
// contract - forwarding to an underlying Flush - using boundedWriter only as
// a collaborator; see REVIEW.md R15.
func TestRedactingWriter_FlushesUnderlyingBoundedWriter(t *testing.T) {
	var buf bytes.Buffer
	bounded := newBoundedWriter(&buf, 10)
	redactor := newRedactingWriter(bounded, []string{"super-secret-value"})
	// The secret straddles the truncation cut; redaction (inside
	// redactingWriter) must run before the bound (inside boundedWriter) so no
	// partial secret ever reaches the underlying buffer - see
	// run-concurrency-and-retention design.md D4.
	if _, err := redactor.Write([]byte("token=super-secret-value tail")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := redactor.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	got := buf.String()
	if strings.Contains(got, "super-secret-value") {
		t.Fatalf("output = %q, secret leaked past truncation", got)
	}
	if !strings.HasSuffix(got, "--- output truncated at 10 bytes (runner.max_output_bytes) ---\n") {
		t.Fatalf("output = %q, want the truncation marker (boundedWriter.Flush not reached)", got)
	}
}

type errWriter struct{ err error }

func (w errWriter) Write([]byte) (int, error) { return 0, w.err }

func TestRedactingWriter_Flush_PropagatesUnderlyingWriteError(t *testing.T) {
	wantErr := errors.New("disk full")
	w := newRedactingWriter(errWriter{err: wantErr}, []string{"super-secret-value"})
	if _, err := w.Write([]byte("token=super-secret-value")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := w.Flush(); !errors.Is(err, wantErr) {
		t.Fatalf("Flush() error = %v, want %v", err, wantErr)
	}
}

func TestRedactingWriter_FlushNoPendingIsNoop(t *testing.T) {
	var buf bytes.Buffer
	w := newRedactingWriter(&buf, []string{"super-secret-value"})
	if _, err := w.Write([]byte("token=super-secret-value")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("Flush 1: %v", err)
	}
	after := buf.String()
	if err := w.Flush(); err != nil {
		t.Fatalf("Flush 2: %v", err)
	}
	if buf.String() != after {
		t.Fatalf("second Flush changed output with no pending bytes: before=%q after=%q", after, buf.String())
	}
}

package service

import (
	"bytes"
	"strings"
	"testing"
)

func TestBoundedWriter_UnderBoundUnchanged(t *testing.T) {
	var buf bytes.Buffer
	w := newBoundedWriter(&buf, 100)
	if _, err := w.Write([]byte("hello")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if got := buf.String(); got != "hello" {
		t.Fatalf("output = %q, want hello unchanged", got)
	}
}

// REVIEW.md R5: exact-boundary case (writing exactly max bytes, no more)
// must leave truncated false and produce no marker line - a regression from
// `remaining >= len(p)` to `remaining > len(p)` in boundedWriter.Write would
// otherwise go undetected.
func TestBoundedWriter_ExactlyAtBoundLeavesUntruncated(t *testing.T) {
	var buf bytes.Buffer
	w := newBoundedWriter(&buf, 5)
	if _, err := w.Write([]byte("12345")); err != nil { // exactly max bytes
		t.Fatalf("Write: %v", err)
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if got := buf.String(); got != "12345" {
		t.Fatalf("output = %q, want exactly the 5 written bytes with no marker", got)
	}
}

func TestBoundedWriter_UnlimitedWhenNonPositive(t *testing.T) {
	var buf bytes.Buffer
	w := newBoundedWriter(&buf, 0)
	big := bytes.Repeat([]byte("x"), 1000)
	if _, err := w.Write(big); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if buf.Len() != 1000 {
		t.Fatalf("output len = %d, want 1000 (unbounded)", buf.Len())
	}
}

func TestBoundedWriter_OverBoundTruncatesWithMarker(t *testing.T) {
	var buf bytes.Buffer
	w := newBoundedWriter(&buf, 10)
	if _, err := w.Write([]byte("0123456789extra bytes past the bound")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	got := buf.String()
	marker := "--- output truncated at 10 bytes (runner.max_output_bytes) ---\n"
	if !strings.HasSuffix(got, marker) {
		t.Fatalf("output = %q, want it to end with marker %q", got, marker)
	}
	if buf.Len() > 10+len(marker) {
		t.Fatalf("output len = %d, want at most bound+marker (%d)", buf.Len(), 10+len(marker))
	}
	if !strings.HasPrefix(got, "0123456789") {
		t.Fatalf("output = %q, want it to start with the first 10 bytes", got)
	}
}

func TestBoundedWriter_FlushWithoutTruncationIsNoop(t *testing.T) {
	var buf bytes.Buffer
	w := newBoundedWriter(&buf, 100)
	if _, err := w.Write([]byte("short")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if got := buf.String(); got != "short" {
		t.Fatalf("output = %q, want short unchanged (no marker)", got)
	}
}

func TestTruncateOutput_UnderBoundUnchanged(t *testing.T) {
	if got := truncateOutput("hello", 100); got != "hello" {
		t.Fatalf("truncateOutput = %q, want hello unchanged", got)
	}
}

func TestTruncateOutput_UnlimitedWhenNonPositive(t *testing.T) {
	big := strings.Repeat("x", 1000)
	if got := truncateOutput(big, 0); got != big {
		t.Fatalf("truncateOutput with max=0 changed a 1000-byte string")
	}
}

func TestTruncateOutput_ExactlyAtBoundLeavesUnchanged(t *testing.T) {
	if got := truncateOutput("12345", 5); got != "12345" {
		t.Fatalf("truncateOutput = %q, want exactly the 5 bytes with no marker", got)
	}
}

func TestTruncateOutput_OverBoundAppendsMarker(t *testing.T) {
	got := truncateOutput("0123456789extra bytes past the bound", 10)
	marker := "--- output truncated at 10 bytes (runner.max_output_bytes) ---\n"
	if !strings.HasSuffix(got, marker) {
		t.Fatalf("truncateOutput = %q, want it to end with marker %q", got, marker)
	}
	if len(got) > 10+len(marker) {
		t.Fatalf("truncateOutput len = %d, want at most bound+marker (%d)", len(got), 10+len(marker))
	}
	if !strings.HasPrefix(got, "0123456789") {
		t.Fatalf("truncateOutput = %q, want it to start with the first 10 bytes", got)
	}
}

func TestBoundedWriter_WriteAfterAlreadyTruncatedIsNoop(t *testing.T) {
	var buf bytes.Buffer
	w := newBoundedWriter(&buf, 5)
	if _, err := w.Write([]byte("0123456789")); err != nil { // exceeds the bound, sets truncated
		t.Fatalf("Write 1: %v", err)
	}
	before := buf.String()
	if _, err := w.Write([]byte("more")); err != nil { // written already >= max
		t.Fatalf("Write 2: %v", err)
	}
	if buf.String() != before {
		t.Fatalf("a write after truncation changed the buffer: before=%q after=%q", before, buf.String())
	}
}

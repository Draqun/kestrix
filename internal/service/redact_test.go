package service

import (
	"bytes"
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

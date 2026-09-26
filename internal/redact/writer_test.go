package redact

import (
	"bytes"
	"errors"
	"testing"
)

func TestWriter_RedactsValueSplitAcrossWrites(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf, []string{"S3cr3t-Remote-Value"})
	for _, chunk := range []string{"token=S3cr3t-Re", "mote-Value done\n"} {
		if _, err := w.Write([]byte(chunk)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	if got, want := buf.String(), "token=*** done\n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

type errWriter struct{ err error }

func (e errWriter) Write([]byte) (int, error) { return 0, e.err }

type flushRecorder struct {
	bytes.Buffer
	flushed bool
}

func (f *flushRecorder) Flush() error { f.flushed = true; return nil }

func TestWriter_NoValuesPassesThrough(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf, nil)
	if _, err := w.Write([]byte("plain")); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "plain" {
		t.Fatalf("got %q", buf.String())
	}
}

func TestWriter_PropagatesWriteErrors(t *testing.T) {
	want := errors.New("boom")
	w := NewWriter(errWriter{want}, []string{"super-secret-value"})
	if _, err := w.Write([]byte("a long line well past the holdback window\n")); !errors.Is(err, want) {
		t.Fatalf("Write err = %v, want %v", err, want)
	}
	w = NewWriter(errWriter{want}, []string{"super-secret-value"})
	if _, err := w.Write([]byte("short")); err != nil {
		t.Fatalf("buffered Write should not reach the writer: %v", err)
	}
	if err := w.Flush(); !errors.Is(err, want) {
		t.Fatalf("Flush err = %v, want %v", err, want)
	}
}

func TestWriter_FlushFlushesUnderlyingWriter(t *testing.T) {
	var rec flushRecorder
	if err := NewWriter(&rec, []string{"super-secret-value"}).Flush(); err != nil {
		t.Fatal(err)
	}
	if !rec.flushed {
		t.Fatal("underlying Flush not called")
	}
}

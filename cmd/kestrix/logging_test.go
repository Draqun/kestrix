package main

import (
	"bytes"
	"encoding/json"
	"log"
	"log/slog"
	"strings"
	"testing"
)

func TestParseLogLevel(t *testing.T) {
	cases := map[string]slog.Level{
		"debug": slog.LevelDebug,
		"info":  slog.LevelInfo,
		"warn":  slog.LevelWarn,
		"error": slog.LevelError,
	}
	for level, want := range cases {
		got, err := parseLogLevel(level)
		if err != nil {
			t.Errorf("parseLogLevel(%q) unexpected error: %v", level, err)
		}
		if got != want {
			t.Errorf("parseLogLevel(%q) = %v, want %v", level, got, want)
		}
	}
}

func TestParseLogLevelRejectsUnknown(t *testing.T) {
	_, err := parseLogLevel("verbose")
	if err == nil {
		t.Fatal("expected an error for an unrecognized level")
	}
	if !strings.Contains(err.Error(), "logging.level") {
		t.Errorf("error = %q, want it to name logging.level", err.Error())
	}
}

// jsonLines splits buf by newline, dropping any trailing empty line, and
// asserts every remaining line is a single valid JSON object - the
// exposition contract of spec.md "Log line is machine readable".
func jsonLines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	raw := strings.TrimRight(buf.String(), "\n")
	if raw == "" {
		return nil
	}
	lines := strings.Split(raw, "\n")
	out := make([]map[string]any, 0, len(lines))
	for i, line := range lines {
		var obj map[string]any
		if err := json.Unmarshal([]byte(line), &obj); err != nil {
			t.Fatalf("line %d is not valid JSON: %q: %v", i, line, err)
		}
		out = append(out, obj)
	}
	return out
}

// TestInstallLogging_SlogCallIsJSON covers tasks.md 2.1: a direct slog call
// after installLogging is a single JSON object per line, carrying a
// timestamp, level, and message - spec.md "Log line is machine readable".
func TestInstallLogging_SlogCallIsJSON(t *testing.T) {
	var buf bytes.Buffer
	if err := installLogging(&buf, "info"); err != nil {
		t.Fatal(err)
	}
	slog.Info("test event", "run_id", 42)

	lines := jsonLines(t, &buf)
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(lines))
	}
	obj := lines[0]
	for _, field := range []string{"time", "level", "msg"} {
		if _, ok := obj[field]; !ok {
			t.Errorf("line missing field %q: %v", field, obj)
		}
	}
	if obj["msg"] != "test event" {
		t.Errorf("msg = %v, want %q", obj["msg"], "test event")
	}
	if obj["run_id"] != float64(42) {
		t.Errorf("run_id = %v, want 42", obj["run_id"])
	}
}

// TestInstallLogging_StdlibLogIsJSON covers tasks.md 2.1's other half: a
// stdlib log.Printf call - the same call chiMiddleware.Logger uses - is
// bridged through the same JSON handler once installLogging has run, so the
// two never split into two different formats on the same stream.
func TestInstallLogging_StdlibLogIsJSON(t *testing.T) {
	var buf bytes.Buffer
	if err := installLogging(&buf, "info"); err != nil {
		t.Fatal(err)
	}
	log.Printf("legacy call site: %s", "value")

	lines := jsonLines(t, &buf)
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(lines))
	}
	obj := lines[0]
	if _, ok := obj["time"]; !ok {
		t.Errorf("bridged log.Printf line missing time field: %v", obj)
	}
	if obj["msg"] != "legacy call site: value" {
		t.Errorf("msg = %v, want %q", obj["msg"], "legacy call site: value")
	}
}

// TestInstallLogging_LevelFiltersRecords covers tasks.md 2.5 and spec.md
// "Level filters records": at warn, info and debug records are dropped and
// warn/error are kept.
func TestInstallLogging_LevelFiltersRecords(t *testing.T) {
	var buf bytes.Buffer
	if err := installLogging(&buf, "warn"); err != nil {
		t.Fatal(err)
	}
	slog.Debug("debug event")
	slog.Info("info event")
	slog.Warn("warn event")
	slog.Error("error event")

	lines := jsonLines(t, &buf)
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2 (warn and error only): %v", len(lines), lines)
	}
	if lines[0]["msg"] != "warn event" || lines[1]["msg"] != "error event" {
		t.Errorf("unexpected messages: %v", lines)
	}
}

func TestInstallLoggingRejectsUnknownLevel(t *testing.T) {
	var buf bytes.Buffer
	err := installLogging(&buf, "verbose")
	if err == nil {
		t.Fatal("expected an error for an unrecognized level")
	}
}

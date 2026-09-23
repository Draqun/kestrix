package web

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	chiMiddleware "github.com/go-chi/chi/v5/middleware"
)

// captureSlog installs a JSON slog handler over buf for the rest of the
// test, restoring the previous default on cleanup - the fixture this
// change's own installLogging (cmd/kestrix) sets up in production, but
// buildRouter's middleware must not assume a caller has called it.
func captureSlog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

// TestSlogRequestLogger_EmitsOneJSONLinePerRequest covers observability
// spec.md "Log line is machine readable" for the request logger
// specifically: chi's own DefaultLogger builds an independent *log.Logger
// around log.New(os.Stdout, ...) at package init time, which never picks up
// the slog JSON handler installLogging installs - this replacement must.
func TestSlogRequestLogger_EmitsOneJSONLinePerRequest(t *testing.T) {
	buf := captureSlog(t)
	srv := NewServer(Config{ListenAddr: ":0"})

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	var requestLine map[string]any
	for _, line := range lines {
		var obj map[string]any
		if err := json.Unmarshal([]byte(line), &obj); err != nil {
			t.Fatalf("line is not valid JSON: %q: %v", line, err)
		}
		if obj["msg"] == "request" {
			requestLine = obj
		}
	}
	if requestLine == nil {
		t.Fatalf("no request line found in log output: %q", buf.String())
	}
	if requestLine["route"] != "/api/health" {
		t.Errorf("route = %v, want /api/health", requestLine["route"])
	}
	if requestLine["method"] != "GET" {
		t.Errorf("method = %v, want GET", requestLine["method"])
	}
	if requestLine["status"] != float64(http.StatusOK) {
		t.Errorf("status = %v, want 200", requestLine["status"])
	}
	if _, ok := requestLine["request_id"]; !ok {
		t.Error("request line missing request_id")
	}
	if _, ok := requestLine["duration"]; !ok {
		t.Error("request line missing duration")
	}
}

// TestSlogRequestLogger_AddrFallsBackToRemoteAddr covers the fallback chi's
// own DefaultLogFormatter uses: an empty client-IP context value (no
// TrustProxyHeader-style middleware populated it) falls back to
// r.RemoteAddr rather than logging an empty address.
func TestSlogRequestLogger_AddrFallsBackToRemoteAddr(t *testing.T) {
	buf := captureSlog(t)
	srv := NewServer(Config{ListenAddr: ":0"})

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	req.RemoteAddr = "203.0.113.7:54321"
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)

	if !strings.Contains(buf.String(), "203.0.113.7:54321") {
		t.Fatalf("log output missing RemoteAddr fallback: %s", buf.String())
	}
}

// TestSlogRequestLogger_ErrorStatusUsesWarnOrError covers the level mapping:
// a 4xx/5xx response is not silently logged at Info.
func TestSlogRequestLogger_ErrorStatusUsesWarnOrError(t *testing.T) {
	buf := captureSlog(t)
	srv := NewServer(Config{ListenAddr: ":0"})

	req := httptest.NewRequest(http.MethodGet, "/api/does-not-exist", nil)
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	var found bool
	for _, line := range lines {
		var obj map[string]any
		if err := json.Unmarshal([]byte(line), &obj); err != nil {
			t.Fatalf("line is not valid JSON: %q: %v", line, err)
		}
		if obj["msg"] != "request" {
			continue
		}
		found = true
		if obj["level"] != "WARN" && obj["level"] != "ERROR" {
			t.Errorf("level = %v, want WARN or ERROR for a 404", obj["level"])
		}
	}
	if !found {
		t.Fatal("no request line found")
	}
}

// TestSlogLogEntry_Panic covers the Panic path: buildRouter chains
// slogRequestLogger before chiMiddleware.Recoverer (chi.Logger's documented
// ordering requirement), so a panicking handler reaches Recoverer, which
// looks up the in-context LogEntry and calls its Panic method - this must
// also land in the JSON stream, not chi's own stderr stack dump.
func TestSlogLogEntry_Panic(t *testing.T) {
	buf := captureSlog(t)
	panicking := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	})
	handler := slogRequestLogger(chiMiddleware.Recoverer(panicking))

	req := httptest.NewRequest(http.MethodGet, "/panics", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (Recoverer)", w.Code)
	}
	var found bool
	for _, line := range strings.Split(strings.TrimRight(buf.String(), "\n"), "\n") {
		var obj map[string]any
		if err := json.Unmarshal([]byte(line), &obj); err != nil {
			t.Fatalf("line is not valid JSON: %q: %v", line, err)
		}
		if obj["msg"] == "request panic" {
			found = true
			if obj["panic"] != "boom" {
				t.Errorf("panic value = %v, want boom", obj["panic"])
			}
			if _, ok := obj["stack"]; !ok {
				t.Error("panic line missing stack")
			}
		}
	}
	if !found {
		t.Fatalf("no \"request panic\" line found: %s", buf.String())
	}
}

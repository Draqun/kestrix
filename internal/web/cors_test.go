package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCORSDisabledByDefault(t *testing.T) {
	srv := NewServer(Config{
		ListenAddr: ":0",
		StorePath:  t.TempDir() + "/projects.json",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	req.Header.Set("Origin", "https://portal.example.com")
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Access-Control-Allow-Origin = %q, want empty when disabled", got)
	}

	preflight := httptest.NewRequest(http.MethodOptions, "/api/projects", nil)
	preflight.Header.Set("Origin", "https://portal.example.com")
	preflight.Header.Set("Access-Control-Request-Method", "GET")
	wPre := httptest.NewRecorder()
	srv.router.ServeHTTP(wPre, preflight)

	if got := wPre.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("preflight Access-Control-Allow-Origin = %q, want empty when disabled", got)
	}
}

func TestCORSAllowedOriginPreflightAndSimpleRequest(t *testing.T) {
	srv := NewServer(Config{
		ListenAddr: ":0",
		StorePath:  t.TempDir() + "/projects.json",
		CORSAllowedOrigins: []string{
			"https://portal.example.com",
			"http://localhost:3000",
		},
	})

	t.Run("preflight options with custom headers", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodOptions, "/api/projects", nil)
		req.Header.Set("Origin", "https://portal.example.com")
		req.Header.Set("Access-Control-Request-Method", "POST")
		req.Header.Set("Access-Control-Request-Headers", "Content-Type, Authorization, X-Custom")
		w := httptest.NewRecorder()
		srv.router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}
		if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://portal.example.com" {
			t.Errorf("Access-Control-Allow-Origin = %q, want https://portal.example.com", got)
		}
		if got := w.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
			t.Errorf("Access-Control-Allow-Credentials = %q, want true", got)
		}
		if got := w.Header().Get("Access-Control-Allow-Methods"); got == "" {
			t.Errorf("Access-Control-Allow-Methods empty, want allowed methods")
		}
		if got := w.Header().Get("Access-Control-Allow-Headers"); got != "Content-Type, Authorization, X-Custom" {
			t.Errorf("Access-Control-Allow-Headers = %q, want requested headers", got)
		}
		if got := w.Header().Get("Vary"); got != "Origin" {
			t.Errorf("Vary = %q, want Origin", got)
		}
	})

	t.Run("preflight options with default headers", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodOptions, "/api/health", nil)
		req.Header.Set("Origin", "http://localhost:3000")
		w := httptest.NewRecorder()
		srv.router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}
		if got := w.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:3000" {
			t.Errorf("Access-Control-Allow-Origin = %q, want http://localhost:3000", got)
		}
		wantHeaders := "Authorization, Content-Type, X-Kestrix-Request, Accept"
		if got := w.Header().Get("Access-Control-Allow-Headers"); got != wantHeaders {
			t.Errorf("Access-Control-Allow-Headers = %q, want %q", got, wantHeaders)
		}
	})

	t.Run("simple GET with allowed origin", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
		req.Header.Set("Origin", "https://portal.example.com")
		w := httptest.NewRecorder()
		srv.router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}
		if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://portal.example.com" {
			t.Errorf("Access-Control-Allow-Origin = %q, want https://portal.example.com", got)
		}
		if got := w.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
			t.Errorf("Access-Control-Allow-Credentials = %q, want true", got)
		}
		if got := w.Header().Get("Vary"); got != "Origin" {
			t.Errorf("Vary = %q, want Origin", got)
		}
	})

	t.Run("simple GET with disallowed origin", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
		req.Header.Set("Origin", "https://evil.example.com")
		w := httptest.NewRecorder()
		srv.router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}
		if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Errorf("Access-Control-Allow-Origin = %q, want empty for disallowed origin", got)
		}
	})

	t.Run("preflight with disallowed origin", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodOptions, "/api/projects", nil)
		req.Header.Set("Origin", "https://evil.example.com")
		w := httptest.NewRecorder()
		srv.router.ServeHTTP(w, req)

		if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Errorf("Access-Control-Allow-Origin = %q, want empty for disallowed origin", got)
		}
	})

	t.Run("simple GET with no origin header", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
		w := httptest.NewRecorder()
		srv.router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}
		if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Errorf("Access-Control-Allow-Origin = %q, want empty when Origin header absent", got)
		}
	})
}

func TestCORSMiddlewareUnitEdgeCases(t *testing.T) {
	t.Run("nil or empty slice", func(t *testing.T) {
		mw := corsMiddleware(nil)
		called := false
		next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
		})
		h := mw(next)
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if !called {
			t.Fatal("expected next handler to be called")
		}
	})

	t.Run("only wildcard or empty strings in slice", func(t *testing.T) {
		mw := corsMiddleware([]string{"*", "  ", ""})
		called := false
		next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
		})
		h := mw(next)
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Origin", "https://example.com")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if !called {
			t.Fatal("expected next handler to be called")
		}
		if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Errorf("Access-Control-Allow-Origin = %q, want empty", got)
		}
	})
}

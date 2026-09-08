package web

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// assertSecurityHeaders checks the four headers SecurityHeaders sets on
// every response.
func assertSecurityHeaders(t *testing.T, h http.Header, wantCSP string) {
	t.Helper()
	if got := h.Get("Content-Security-Policy"); got != wantCSP {
		t.Errorf("Content-Security-Policy = %q, want %q", got, wantCSP)
	}
	if got := h.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
	}
	if got := h.Get("Referrer-Policy"); got != "no-referrer" {
		t.Errorf("Referrer-Policy = %q, want no-referrer", got)
	}
	if got := h.Get("X-Frame-Options"); got != "DENY" {
		t.Errorf("X-Frame-Options = %q, want DENY", got)
	}
}

func TestSecurityHeaders_OnAPIResponse(t *testing.T) {
	srv := systemHandlerServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)
	assertSecurityHeaders(t, w.Header(), defaultContentSecurityPolicy)
}

func TestSecurityHeaders_OnErrorResponse(t *testing.T) {
	srv := systemHandlerServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/projects", nil) // no token: 401
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
	assertSecurityHeaders(t, w.Header(), defaultContentSecurityPolicy)
}

func TestSecurityHeaders_OnStaticAsset(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html></html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := NewServer(Config{StaticDir: dir})
	req := httptest.NewRequest(http.MethodGet, "/index.html", nil)
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)
	assertSecurityHeaders(t, w.Header(), defaultContentSecurityPolicy)
}

func TestSecurityHeaders_ConfiguredPolicyOverridesDefault(t *testing.T) {
	const custom = "default-src 'none'"
	srv := systemHandlerServer(t)
	srv.cfg.ContentSecurityPolicy = custom
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)
	if got := w.Header().Get("Content-Security-Policy"); got != custom {
		t.Fatalf("Content-Security-Policy = %q, want configured value %q", got, custom)
	}
}

func TestSecurityHeaders_DocsRouteGetsItsOwnPolicy(t *testing.T) {
	srv := systemHandlerServer(t)
	req := httptest.NewRequest(http.MethodGet, "/docs/swagger.json", nil)
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if got := w.Header().Get("Content-Security-Policy"); got != docsContentSecurityPolicy {
		t.Fatalf("Content-Security-Policy = %q, want the docs policy %q", got, docsContentSecurityPolicy)
	}
	// The other three headers still apply on /docs/* - only the policy is
	// route-specific.
	if got := w.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q, want nosniff even on /docs/*", got)
	}
}

func TestSecurityHeaders_ApplicationRoutesKeepStrictPolicyDespiteDocs(t *testing.T) {
	srv := systemHandlerServer(t)
	// Exercise /docs/* first to guard against any shared-state leakage
	// between the two policies.
	docsReq := httptest.NewRequest(http.MethodGet, "/docs/swagger.json", nil)
	srv.router.ServeHTTP(httptest.NewRecorder(), docsReq)

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)
	if got := w.Header().Get("Content-Security-Policy"); got != defaultContentSecurityPolicy {
		t.Fatalf("Content-Security-Policy = %q, want the strict default, not the docs policy", got)
	}
}

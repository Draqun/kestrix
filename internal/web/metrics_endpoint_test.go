package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"kestrix/internal/domain"
	"kestrix/internal/mocks"
)

// TestHandleMetrics_DisabledReturns404 covers spec.md "Metrics disabled by
// default": with no Metrics.Enabled setting, /metrics does not exist at
// all - the router's ordinary 404, indistinguishable from any other unknown
// path.
func TestHandleMetrics_DisabledReturns404(t *testing.T) {
	srv := NewServer(Config{})
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)

	req2 := httptest.NewRequest(http.MethodGet, "/some-unknown-path", nil)
	w2 := httptest.NewRecorder()
	srv.router.ServeHTTP(w2, req2)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
	if w.Code != w2.Code || w.Body.String() != w2.Body.String() {
		t.Fatalf("/metrics response (%d %q) is distinguishable from an unknown path (%d %q)",
			w.Code, w.Body.String(), w2.Code, w2.Body.String())
	}
}

// metricsAuthTestServer builds a server with metrics enabled, using the
// default NewServer wiring (mocks.MockUserStore, a real JWT issuer), so
// both the scrape-token and viewer-session paths can be exercised through
// the real router.
func metricsAuthTestServer(t *testing.T, scrapeToken string) *Server {
	t.Helper()
	return NewServer(Config{Metrics: MetricsConfig{Enabled: true, Token: scrapeToken}})
}

// TestHandleMetrics_EnabledNoCredentials401 covers spec.md "Unauthenticated
// scrape rejected".
func TestHandleMetrics_EnabledNoCredentials401(t *testing.T) {
	srv := metricsAuthTestServer(t, "correct-scrape-token-1")
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
	if strings.Contains(w.Body.String(), "kestrix_") {
		t.Fatalf("response body contains a metric despite no credentials: %s", w.Body.String())
	}
}

// TestHandleMetrics_ScrapeTokenAccepted covers spec.md "Scrape token
// accepted": no user account or session is needed when the bearer token
// matches.
func TestHandleMetrics_ScrapeTokenAccepted(t *testing.T) {
	srv := metricsAuthTestServer(t, "correct-scrape-token-1")
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.Header.Set("Authorization", "Bearer correct-scrape-token-1")
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "kestrix_build_info") {
		t.Fatalf("response body missing kestrix_build_info: %s", w.Body.String())
	}
}

// TestHandleMetrics_ContentType covers spec.md's exposition content type.
func TestHandleMetrics_ContentType(t *testing.T) {
	srv := metricsAuthTestServer(t, "correct-scrape-token-1")
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.Header.Set("Authorization", "Bearer correct-scrape-token-1")
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)

	if got := w.Header().Get("Content-Type"); got != metricsContentType {
		t.Fatalf("Content-Type = %q, want %q", got, metricsContentType)
	}
}

// TestHandleMetrics_WrongTokenSameBodyAsNoCredentials covers spec.md "Wrong
// token is not distinguished".
func TestHandleMetrics_WrongTokenSameBodyAsNoCredentials(t *testing.T) {
	srv := metricsAuthTestServer(t, "correct-scrape-token-1")

	noCreds := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	wNoCreds := httptest.NewRecorder()
	srv.router.ServeHTTP(wNoCreds, noCreds)

	wrongToken := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	wrongToken.Header.Set("Authorization", "Bearer wrong-token-value-here")
	wWrong := httptest.NewRecorder()
	srv.router.ServeHTTP(wWrong, wrongToken)

	if wWrong.Code != http.StatusUnauthorized {
		t.Fatalf("wrong-token status = %d, want 401", wWrong.Code)
	}
	if wWrong.Code != wNoCreds.Code || wWrong.Body.String() != wNoCreds.Body.String() {
		t.Fatalf("wrong-token response (%d %q) differs from no-credentials response (%d %q)",
			wWrong.Code, wWrong.Body.String(), wNoCreds.Code, wNoCreds.Body.String())
	}
}

// TestHandleMetrics_ViewerSessionAccepted covers spec.md "Viewer may read
// metrics" (access-control delta): a signed-in viewer works the same as any
// other protected route, with no scrape token involved.
func TestHandleMetrics_ViewerSessionAccepted(t *testing.T) {
	srv := metricsAuthTestServer(t, "" /* no scrape token configured */)
	created, err := srv.userStore.(*mocks.MockUserStore).CreateUser("viewer1", domain.RoleViewer, "")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	token, _, err := srv.tokenIssuer.Issue(created)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
}

// TestRequireMetricsAuth_TokenDoesNotAuthenticateAPI covers access-control
// spec.md "Scrape token is not an API credential": the same bearer value
// that works on /metrics must be rejected on an ordinary /api route.
func TestRequireMetricsAuth_TokenDoesNotAuthenticateAPI(t *testing.T) {
	srv := metricsAuthTestServer(t, "correct-scrape-token-1")
	req := httptest.NewRequest(http.MethodGet, "/api/dashboard", nil)
	req.Header.Set("Authorization", "Bearer correct-scrape-token-1")
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 - the metrics scrape token must not authenticate /api", w.Code)
	}
}

// TestHandleMetrics_PublicRouteSetUnchanged covers access-control spec.md
// "Public route set is unchanged": enabling metrics adds no new public
// route - /metrics itself still requires credentials (covered above), and
// an unrelated unauthenticated /api route's behavior is untouched.
func TestHandleMetrics_PublicRouteSetUnchanged(t *testing.T) {
	srv := metricsAuthTestServer(t, "correct-scrape-token-1")
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/health status = %d, want 200 (still public)", w.Code)
	}
}

func TestHandleMetrics_TracksUnmatchedJobsQuery(t *testing.T) {
	srv := metricsAuthTestServer(t, "token")
	mockRemote := &mocks.MockRemoteJobStore{
		UnmatchedPendingJobsResult: []domain.UnmatchedPendingJob{
			{ID: 1, RunID: 10, TaskName: "build"},
			{ID: 2, RunID: 11, TaskName: "deploy"},
		},
	}
	srv.remoteJobStore = mockRemote

	// Scrape with 2 unmatched jobs
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.Header.Set("Authorization", "Bearer token")
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "# HELP kestrix_remote_jobs_unmatched Number of pending remote jobs that no registered runner can take.\n# TYPE kestrix_remote_jobs_unmatched gauge\nkestrix_remote_jobs_unmatched 2") {
		t.Fatalf("metrics body does not contain expected gauge = 2:\n%s", body)
	}

	// Scrape when query returns 0 jobs
	mockRemote.UnmatchedPendingJobsResult = nil
	w = httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)
	body = w.Body.String()
	if !strings.Contains(body, "kestrix_remote_jobs_unmatched 0") {
		t.Fatalf("metrics body does not contain expected gauge = 0:\n%s", body)
	}

	// Scrape when query fails with error
	mockRemote.UnmatchedPendingJobsErr = http.ErrHandlerTimeout
	w = httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)
	body = w.Body.String()
	if !strings.Contains(body, "kestrix_remote_jobs_unmatched 0") {
		t.Fatalf("metrics body does not contain expected gauge = 0 on error:\n%s", body)
	}
}

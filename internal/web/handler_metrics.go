package web

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"kestrix/internal/domain"
	"kestrix/internal/metrics"
	"kestrix/internal/version"
)

// metricsContentType is the Prometheus text exposition format 0.0.4 content
// type - see observability spec.md "Metrics endpoint".
const metricsContentType = "text/plain; version=0.0.4; charset=utf-8"

// handleMetrics renders the current metric snapshot in Prometheus text
// exposition format. It is registered only when metrics are enabled (see
// buildRouter), so a disabled instance never reaches this handler at all.
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	var running, queued int
	if s.runService != nil {
		running, queued = s.runService.Counts()
	}
	var unmatched int
	if s.remoteJobStore != nil {
		if jobs, err := s.remoteJobStore.UnmatchedPendingJobs(nil); err == nil {
			unmatched = len(jobs)
		}
	}
	snap := metrics.Snapshot{
		RunsRunning:         running,
		RunsQueued:          queued,
		RemoteJobsUnmatched: unmatched,
		BuildVersion:        version.Version,
	}
	if reg, ok := s.metrics.(*metrics.Registry); ok {
		snap = reg.Snapshot(running, queued, unmatched, version.Version)
	}
	w.Header().Set("Content-Type", metricsContentType)
	w.Write(metrics.Render(snap))
}

// requireMetricsAuth wraps the metrics endpoint with its own authentication
// - see access-control spec.md "Metrics endpoint authentication" and
// observability design.md D5. A bearer token is checked against the
// configured scrape token in constant time, matching
// auth/password.go's ConstantTimeCompare pattern; a request with no bearer
// header falls through to the ordinary RequireAuth + RequireRole(viewer)
// chain, so a signed-in account works exactly as it does on every other
// route. The scrape token authenticates nothing outside this one route: it
// never resolves to an account, a role, or a session (see
// TestRequireMetricsAuth_TokenDoesNotAuthenticateAPI).
func (s *Server) requireMetricsAuth(next http.Handler) http.Handler {
	protected := s.RequireAuth(s.RequireRole(domain.RoleViewer)(next))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only intercept when a scrape token is actually configured: with
		// none set there is nothing to compare a bearer value against as
		// "the" scrape token, so every request - bearer or cookie - goes
		// through the ordinary RequireAuth + RequireRole(viewer) chain, the
		// same as any other protected route (a viewer's session JWT is
		// routinely presented as a bearer header, not only as a cookie).
		if s.cfg.Metrics.Token != "" {
			header := r.Header.Get("Authorization")
			const prefix = "Bearer "
			if strings.HasPrefix(header, prefix) {
				token := header[len(prefix):]
				// Keep this comparison on subtle.ConstantTimeCompare - a
				// short-circuiting == here would pass every existing test
				// (they assert accept/reject outcomes, not timing) while
				// reopening the timing side-channel this line exists to
				// close; see REVIEW.md R8.
				if subtle.ConstantTimeCompare([]byte(token), []byte(s.cfg.Metrics.Token)) == 1 {
					next.ServeHTTP(w, r)
					return
				}
				// A bearer header was presented and a scrape token is
				// configured, but it did not match. Reject here with the
				// same body RequireAuth uses for no credentials at all,
				// rather than falling through to it: RequireAuth would try
				// to verify this value as a session JWT and answer with a
				// different message - see spec.md "Wrong token is not
				// distinguished".
				writeError(w, http.StatusUnauthorized, missingCredentialMessage)
				return
			}
		}
		protected.ServeHTTP(w, r)
	})
}

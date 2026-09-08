package web

import "net/http"

// defaultContentSecurityPolicy fits the bundled React UI: same-origin
// scripts and styles, plus inline styles for the component libraries it
// uses. No framing, no foreign origins. See design.md D8.
const defaultContentSecurityPolicy = "default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; base-uri 'self'; frame-ancestors 'none'"

// docsContentSecurityPolicy relaxes script-src for Swagger UI, which
// renders itself with inline script execution. It is scoped to /docs/*
// only - the rest of the application keeps defaultContentSecurityPolicy (or
// the operator's configured override).
const docsContentSecurityPolicy = "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; base-uri 'self'; frame-ancestors 'none'"

// SecurityHeaders sets the response headers every route needs regardless of
// authentication state: a content security policy
// (s.cfg.ContentSecurityPolicy, falling back to defaultContentSecurityPolicy
// when unset), MIME-sniffing protection, a conservative referrer policy,
// and a framing policy. It is mounted on the root router, so it covers the
// API, the webhook route, the docs route, and the static frontend alike.
//
// This is the mitigation rbac-auth's design D8 named for the token kept in
// sessionStorage but never implemented; see design.md D8 here.
func (s *Server) SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		policy := s.cfg.ContentSecurityPolicy
		if policy == "" {
			policy = defaultContentSecurityPolicy
		}
		h := w.Header()
		h.Set("Content-Security-Policy", policy)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}

// docsSecurityHeaders overrides the Content-Security-Policy SecurityHeaders
// set, scoped to /docs/*, which needs inline script execution to render
// Swagger UI. It leaves the other three headers SecurityHeaders already set
// in place rather than loosening them too.
func docsSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", docsContentSecurityPolicy)
		next.ServeHTTP(w, r)
	})
}

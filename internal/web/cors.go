package web

import (
	"net/http"
	"strings"
)

// corsMiddleware returns a middleware that handles CORS for matching allowed
// origins on the /api subtree. If allowedOrigins is empty, it returns a no-op
// middleware leaving responses untouched. Wildcard "*" is ignored to prevent
// credential leakage.
func corsMiddleware(allowedOrigins []string) func(http.Handler) http.Handler {
	if len(allowedOrigins) == 0 {
		return func(next http.Handler) http.Handler {
			return next
		}
	}

	allowed := make(map[string]struct{}, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		trimmed := strings.TrimSpace(origin)
		if trimmed != "" && trimmed != "*" {
			allowed[trimmed] = struct{}{}
		}
	}
	if len(allowed) == 0 {
		return func(next http.Handler) http.Handler {
			return next
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin == "" {
				next.ServeHTTP(w, r)
				return
			}

			if _, ok := allowed[origin]; !ok {
				next.ServeHTTP(w, r)
				return
			}

			h := w.Header()
			h.Set("Access-Control-Allow-Origin", origin)
			h.Set("Access-Control-Allow-Credentials", "true")
			h.Add("Vary", "Origin")

			if r.Method == http.MethodOptions {
				h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS, HEAD")
				reqHeaders := r.Header.Get("Access-Control-Request-Headers")
				if reqHeaders != "" {
					h.Set("Access-Control-Allow-Headers", reqHeaders)
				} else {
					h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Kestrix-Request, Accept")
				}
				h.Set("Access-Control-Max-Age", "86400")
				w.WriteHeader(http.StatusOK)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

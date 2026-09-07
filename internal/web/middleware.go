package web

import (
	"context"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"kestrix/internal/domain"
)

type contextKey int

const claimsContextKey contextKey = iota

// claimsFromContext returns the verified claims RequireAuth placed on the
// request context, if any.
func claimsFromContext(ctx context.Context) (domain.Claims, bool) {
	claims, ok := ctx.Value(claimsContextKey).(domain.Claims)
	return claims, ok
}

// RequireAuth verifies the bearer token on every request it wraps and places
// the resulting claims on the request context for RequireRole and handlers.
// A missing header, a non-bearer header, or a token that fails verification
// (signature, algorithm, or expiry) is rejected with 401 before next runs.
func (s *Server) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		const prefix = "Bearer "
		if !strings.HasPrefix(header, prefix) || len(header) <= len(prefix) {
			writeError(w, http.StatusUnauthorized, "missing or malformed Authorization header")
			return
		}
		token := header[len(prefix):]
		if s.tokenIssuer == nil {
			writeError(w, http.StatusUnauthorized, "authentication is not configured")
			return
		}
		claims, err := s.tokenIssuer.Verify(token)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid or expired token")
			return
		}
		ctx := context.WithValue(r.Context(), claimsContextKey, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// projectRouteParam is the chi URL parameter name every project-scoped route
// in this router uses. RequireRole reads it to resolve a per-project grant;
// a route with no such parameter is treated as not project-scoped.
const projectRouteParam = "name"

// RequireRole returns middleware that rejects a request whose effective role
// - the higher of the token's global role and any project grant on the
// route's {name} parameter, per domain.HigherRole - does not meet min. It
// must run after RequireAuth, so claims are always present; a missing
// claims value is a wiring bug and is rejected rather than trusted.
func (s *Server) RequireRole(min domain.Role) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := claimsFromContext(r.Context())
			if !ok {
				writeError(w, http.StatusUnauthorized, "missing or malformed Authorization header")
				return
			}
			effective := claims.Role
			if projectName := chi.URLParam(r, projectRouteParam); projectName != "" && s.grantStore != nil {
				if grant, found, err := s.grantStore.ResolveGrant(claims.UserName, projectName); err == nil && found {
					effective = domain.HigherRole(effective, grant)
				}
			}
			if !effective.AtLeast(min) {
				writeError(w, http.StatusForbidden, "insufficient role")
				return
			}
			next.ServeHTTP(w, r.WithContext(r.Context()))
		})
	}
}

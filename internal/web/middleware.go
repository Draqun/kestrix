package web

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"kestrix/internal/domain"
	"kestrix/internal/store"
)

type contextKey int

const claimsContextKey contextKey = iota

// invalidTokenMessage is the single response body used for every reason a
// bearer token is not accepted: a bad signature, an unknown account, and a
// superseded token version all answer identically, so a caller cannot use
// the response to tell which one occurred; see openspec spec
// user-authentication, "Token verification".
const invalidTokenMessage = "invalid or expired token"

// missingCredentialMessage is returned for a request with no usable
// credential at all: no Authorization header and no session cookie, or a
// header present but not a well-formed bearer token. The latter never falls
// back to the cookie; see design.md D2.
const missingCredentialMessage = "missing or malformed Authorization header"

// csrfHeaderName is the custom header a cookie-authenticated, state-changing
// request must carry; see design.md D3.
const csrfHeaderName = "X-Kestrix-Request"

// errMissingCredential is returned by credentialFromRequest when neither a
// well-formed bearer header nor a session cookie is present.
var errMissingCredential = errors.New(missingCredentialMessage)

// isSafeMethod reports whether method never changes state, per design.md D3.
func isSafeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	default:
		return false
	}
}

// credentialFromRequest extracts the token RequireAuth should verify: the
// bearer header when present, or the session cookie otherwise. fromCookie
// reports which source won. A header that is present but not a well-formed
// bearer token is rejected here without falling back to the cookie; see
// design.md D2.
func credentialFromRequest(r *http.Request) (token string, fromCookie bool, err error) {
	header := r.Header.Get("Authorization")
	if header != "" {
		const prefix = "Bearer "
		if !strings.HasPrefix(header, prefix) || len(header) <= len(prefix) {
			return "", false, errMissingCredential
		}
		return header[len(prefix):], false, nil
	}
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || cookie.Value == "" {
		return "", false, errMissingCredential
	}
	return cookie.Value, true, nil
}

// claimsFromContext returns the verified claims RequireAuth placed on the
// request context, if any.
func claimsFromContext(ctx context.Context) (domain.Claims, bool) {
	claims, ok := ctx.Value(claimsContextKey).(domain.Claims)
	return claims, ok
}

// RequireAuth verifies the bearer token on every request it wraps and places
// the resulting claims on the request context for RequireRole and handlers.
// A missing header, a non-bearer header, or a token that fails signature,
// algorithm, or expiry verification is rejected with 401 before next runs.
//
// The token's role claim is never used to decide the request; see
// design.md D1. Once the token itself verifies, RequireAuth resolves the
// account's current role and token version from the user store (through
// resolveIdentity, which may serve a bounded-age cached answer) and rejects
// with 401 if the account no longer exists or the token's version has been
// superseded by a role change, a password change, or account deletion; see
// design.md D2. A user-store failure is distinguished from both: it is
// rejected with 503, never silently treated as an unknown account; see
// design.md D3.
func (s *Server) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, fromCookie, err := credentialFromRequest(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, missingCredentialMessage)
			return
		}
		if fromCookie && !isSafeMethod(r.Method) && r.Header.Get(csrfHeaderName) == "" {
			writeError(w, http.StatusForbidden, "missing "+csrfHeaderName+" header")
			return
		}
		if s.tokenIssuer == nil {
			writeError(w, http.StatusUnauthorized, "authentication is not configured")
			return
		}
		tokenClaims, err := s.tokenIssuer.Verify(token)
		if err != nil {
			s.logAuthFailure(r, "", "token failed signature, algorithm, or expiry verification")
			writeError(w, http.StatusUnauthorized, invalidTokenMessage)
			return
		}
		if s.userStore == nil {
			writeError(w, http.StatusUnauthorized, "authentication is not configured")
			return
		}
		identity, storeErr := s.resolveIdentity(tokenClaims.UserName)
		if storeErr != nil {
			log.Printf("auth: resolve identity for %q: %v", tokenClaims.UserName, storeErr)
			writeError(w, http.StatusServiceUnavailable, "authentication is temporarily unavailable")
			return
		}
		if !identity.Exists || identity.TokenVersion != tokenClaims.TokenVersion {
			s.logAuthFailure(r, tokenClaims.UserName, "unknown account or superseded token version")
			writeError(w, http.StatusUnauthorized, invalidTokenMessage)
			return
		}
		claims := domain.Claims{
			UserName:     tokenClaims.UserName,
			Role:         identity.Role,
			TokenVersion: identity.TokenVersion,
			ExpiresAt:    tokenClaims.ExpiresAt,
		}
		ctx := context.WithValue(r.Context(), claimsContextKey, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// resolveIdentity resolves name's current role, existence, and token
// version, preferring s.identityCache (nil disables it) and falling back to
// s.userStore on a miss. Its error return is non-nil only for a genuine
// store failure: an account that does not exist is a normal, cacheable
// result (IdentityCacheEntry{Exists: false}), never an error.
func (s *Server) resolveIdentity(name string) (domain.IdentityCacheEntry, error) {
	if s.identityCache != nil {
		if entry, ok := s.identityCache.Get(name); ok {
			return entry, nil
		}
	}
	user, err := s.userStore.GetUser(name)
	var entry domain.IdentityCacheEntry
	switch {
	case err == nil:
		entry = domain.IdentityCacheEntry{Exists: true, Role: user.Role, TokenVersion: user.TokenVersion}
	case errors.Is(err, store.ErrUserNotFound):
		entry = domain.IdentityCacheEntry{Exists: false}
	default:
		return domain.IdentityCacheEntry{}, err
	}
	if s.identityCache != nil {
		s.identityCache.Set(name, entry)
	}
	return entry, nil
}

// invalidateIdentity drops any cached entry for name, so the very next
// request against it resolves fresh from the user store. Handlers that
// change or delete an account call this after a successful write.
func (s *Server) invalidateIdentity(name string) {
	if s.identityCache != nil {
		s.identityCache.Invalidate(name)
	}
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
//
// A grant-store failure is rejected with 503 rather than treated as an
// absent grant, so an outage cannot silently demote a caller; see
// design.md D3.
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
				grant, found, err := s.grantStore.ResolveGrant(claims.UserName, projectName)
				if err != nil {
					log.Printf("authz: resolve grant for %q/%q: %v", claims.UserName, projectName, err)
					writeError(w, http.StatusServiceUnavailable, "authorization is temporarily unavailable")
					return
				}
				if found {
					effective = domain.HigherRole(effective, grant)
				}
			}
			if !effective.AtLeast(min) {
				s.logDenial(r, claims, min, effective)
				writeError(w, http.StatusForbidden, "insufficient role")
				return
			}
			next.ServeHTTP(w, r.WithContext(r.Context()))
		})
	}
}

// logAuthFailure records a rejected authentication. account is empty when
// the token itself failed verification, before any identity is known. Never
// pass the token, any part of it, or a request body here.
func (s *Server) logAuthFailure(r *http.Request, account, reason string) {
	log.Printf("auth: rejected account=%q addr=%q route=%q reason=%q", account, r.RemoteAddr, r.URL.Path, reason)
}

// logDenial records a 403 authorization denial with enough to audit it:
// the account, the route, and both roles compared. Never pass the token or
// a request body here.
func (s *Server) logDenial(r *http.Request, claims domain.Claims, required, effective domain.Role) {
	log.Printf("authz: denied account=%q addr=%q route=%q required=%q effective=%q",
		claims.UserName, r.RemoteAddr, r.URL.Path, required, effective)
}

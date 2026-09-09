package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"kestrix/internal/service"
)

// sessionCookieName is the browser session cookie set on login and read by
// RequireAuth as an alternative to the bearer header; see design.md D1.
const sessionCookieName = "kestrix_session"

// sessionCookiePath scopes the cookie to the API, keeping it off static
// assets and /docs/*; see design.md D4.
const sessionCookiePath = "/api"

// loginRequest is the POST /api/auth/login body.
type loginRequest struct {
	Name     string `json:"name"`
	Password string `json:"password"`
}

// loginResponse never carries a password hash: it is the token and its
// expiry only.
type loginResponse struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

// meResponse is the GET /api/auth/me body: the signed-in account's identity,
// never the token itself; see design.md D5.
type meResponse struct {
	Name      string    `json:"name"`
	Role      string    `json:"role"`
	ExpiresAt time.Time `json:"expires_at"`
}

// sessionCookie builds the Set-Cookie for a browser session carrying token,
// with Max-Age derived from expiresAt; see design.md D1/D4.
func (s *Server) sessionCookie(token string, expiresAt time.Time) *http.Cookie {
	maxAge := int(time.Until(expiresAt).Seconds())
	if maxAge <= 0 {
		// http.Cookie.String serializes MaxAge==0 as *no* Max-Age attribute
		// at all - a session cookie, the opposite of expired. Only a
		// negative value emits "Max-Age=0", which is what actually expires
		// it immediately.
		maxAge = -1
	}
	return s.buildSessionCookie(token, maxAge)
}

// clearSessionCookie builds the Set-Cookie that discards a browser session,
// matching sessionCookie's other attributes so the browser recognizes it as
// the same cookie; see design.md D6.
func (s *Server) clearSessionCookie() *http.Cookie {
	return s.buildSessionCookie("", -1)
}

// buildSessionCookie holds the attributes sessionCookie and
// clearSessionCookie must agree on: only value and Max-Age ever differ
// between setting and clearing the session.
func (s *Server) buildSessionCookie(value string, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name:     sessionCookieName,
		Value:    value,
		Path:     sessionCookiePath,
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   s.cfg.CookieSecure,
		SameSite: http.SameSiteStrictMode,
	}
}

// handleLogin implements POST /api/auth/login. It is registered on the
// public router group: no token is required to call it.
//
// @Summary Authenticate and obtain a token
// @Tags auth
// @Accept json
// @Produce json
// @Param body body loginRequest true "Credentials"
// @Success 200 {object} loginResponse
// @Failure 401 {object} map[string]string
// @Router /api/auth/login [post]
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if s.authService == nil {
		writeError(w, http.StatusServiceUnavailable, "authentication is not configured")
		return
	}
	var req loginRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	// Login is public and now has a side effect a cross-site request can
	// exploit even without reading the response: a form whose Content-Type
	// is one of the browser's CORS-safelisted types (e.g. text/plain) needs
	// no preflight, and json.Decoder ignores trailing bytes after the first
	// value, so a form body can still decode above. Requiring the exact
	// JSON content type before ever calling AuthService closes that path -
	// no HTML form can produce it - so a cross-site page can no longer
	// plant its own account's session cookie in a victim's browser (login
	// CSRF / session fixation). Checked after decodeJSON so an oversized or
	// malformed body still answers 413/400, matching every other route.
	if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		writeError(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
		return
	}
	token, expiresAt, err := s.authService.Login(req.Name, req.Password)
	if err != nil {
		if errors.Is(err, service.ErrInvalidCredentials) {
			writeError(w, http.StatusUnauthorized, "invalid name or password")
			return
		}
		writeError(w, http.StatusInternalServerError, "login failed")
		return
	}
	http.SetCookie(w, s.sessionCookie(token, expiresAt))
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(loginResponse{Token: token, ExpiresAt: expiresAt})
}

// handleLogout implements POST /api/auth/logout. It is registered on the
// public router group so an expired session can still be cleaned up, and it
// succeeds whether or not a valid session is present; see design.md D6. It
// never touches token_version: a bearer client holding the same account's
// token is unaffected.
//
// Logout takes no credential, so it never runs through RequireAuth's CSRF
// gate; it still requires the same custom header, since without it a
// cross-site auto-submitting form could force any signed-in visitor's
// session to end (the browser accepts this response's Set-Cookie
// regardless of SameSite - that attribute governs whether an existing
// cookie is attached to a request, not whether a response may set one).
// The header is not a credential, so this still satisfies "no session
// required" - the bundled UI already sends it on every call.
//
// @Summary End the browser session
// @Tags auth
// @Success 204
// @Router /api/auth/logout [post]
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get(csrfHeaderName) == "" {
		writeError(w, http.StatusForbidden, "missing "+csrfHeaderName+" header")
		return
	}
	http.SetCookie(w, s.clearSessionCookie())
	w.WriteHeader(http.StatusNoContent)
}

// handleMe implements GET /api/auth/me behind RequireAuth. It returns the
// stored identity RequireAuth already resolved onto the context - the
// account's current role, not the token's - so a client that cannot decode
// its own HttpOnly cookie can restore its session; see design.md D5.
//
// @Summary Current session
// @Tags auth
// @Produce json
// @Success 200 {object} meResponse
// @Failure 401 {object} map[string]string
// @Router /api/auth/me [get]
func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, invalidTokenMessage)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(meResponse{
		Name:      claims.UserName,
		Role:      string(claims.Role),
		ExpiresAt: claims.ExpiresAt,
	})
}

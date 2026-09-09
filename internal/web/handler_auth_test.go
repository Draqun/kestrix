package web

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"kestrix/internal/domain"
	"kestrix/internal/mocks"
	"kestrix/internal/service"
)

func createTestUser(t *testing.T, srv *Server, name, password string, role domain.Role) {
	t.Helper()
	hash, err := srv.passwordHasher.Hash(password)
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if _, err := srv.userStore.CreateUser(name, role, hash); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
}

func TestHandleLogin_Success(t *testing.T) {
	srv := NewServer(Config{})
	createTestUser(t, srv, "alice", "s3cret-pass", domain.RoleOperator)

	body, _ := json.Marshal(loginRequest{Name: "alice", Password: "s3cret-pass"})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "hash") || strings.Contains(w.Body.String(), "s3cret-pass") {
		t.Fatalf("response leaked password material: %s", w.Body.String())
	}
	var resp loginResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Token == "" {
		t.Fatal("response carries no token")
	}
}

func TestHandleLogin_FailureModesReturnIdenticalBody(t *testing.T) {
	srv := NewServer(Config{})
	createTestUser(t, srv, "bob", "correct-password", domain.RoleViewer)

	wrongPassword, _ := json.Marshal(loginRequest{Name: "bob", Password: "wrong-password"})
	unknownAccount, _ := json.Marshal(loginRequest{Name: "nobody", Password: "anything"})

	req1 := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(wrongPassword))
	req1.Header.Set("Content-Type", "application/json")
	w1 := httptest.NewRecorder()
	srv.router.ServeHTTP(w1, req1)

	req2 := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(unknownAccount))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	srv.router.ServeHTTP(w2, req2)

	if w1.Code != http.StatusUnauthorized || w2.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d/%d, want both 401", w1.Code, w2.Code)
	}
	if w1.Body.String() != w2.Body.String() {
		t.Fatalf("bodies differ: wrong-password=%q unknown-account=%q", w1.Body.String(), w2.Body.String())
	}
}

func TestHandleLogin_InvalidJSON(t *testing.T) {
	srv := NewServer(Config{})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader("{not json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

// TestHandleLogin_RejectsNonJSONContentType locks in the fix for a login-CSRF
// / session-fixation primitive: a cross-site auto-submitting form can send a
// CORS-safelisted Content-Type (e.g. text/plain) with no preflight, and
// json.Decoder ignores trailing form-encoded bytes after the first value, so
// the body still parses. Since login now has a side effect (Set-Cookie) a
// forger can exploit without ever reading the response, requiring the exact
// JSON content type closes that path: no HTML form can produce it.
func TestHandleLogin_RejectsNonJSONContentType(t *testing.T) {
	srv := NewServer(Config{})
	createTestUser(t, srv, "alice", "s3cret-pass", domain.RoleOperator)

	// A body a <form enctype="text/plain"> could produce: valid JSON
	// followed by the form's own trailing field, which json.Decoder simply
	// never reads.
	body := `{"name":"alice","password":"s3cret-pass"}` + "\r\nignored=1"
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "text/plain")
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)

	if w.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want 415", w.Code)
	}
	if len(w.Result().Cookies()) != 0 {
		t.Fatalf("cookies = %+v, want none - a non-JSON Content-Type must never reach AuthService.Login", w.Result().Cookies())
	}
}

func TestHandleLogin_NilAuthService(t *testing.T) {
	srv := &Server{}
	w := httptest.NewRecorder()
	srv.handleLogin(w, httptest.NewRequest(http.MethodPost, "/api/auth/login", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", w.Code)
	}
}

func TestHandleLogin_UnexpectedServiceError(t *testing.T) {
	srv := NewServer(Config{})
	createTestUser(t, srv, "alice", "s3cret-pass", domain.RoleOperator)
	srv.tokenIssuer = mocks.NewMockTokenIssuer()
	srv.tokenIssuer.(*mocks.MockTokenIssuer).IssueErr = errors.New("signing key unavailable")
	srv.authService = service.NewAuthService(srv.userStore, srv.passwordHasher, srv.tokenIssuer)

	body, _ := json.Marshal(loginRequest{Name: "alice", Password: "s3cret-pass"})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
}

func TestSessionCookie_Attributes(t *testing.T) {
	srv := NewServer(Config{CookieSecure: true})
	expiresAt := time.Now().Add(45 * time.Minute)
	cookie := srv.sessionCookie("the-token", expiresAt)

	if cookie.Name != sessionCookieName {
		t.Fatalf("Name = %q, want %q", cookie.Name, sessionCookieName)
	}
	if cookie.Value != "the-token" {
		t.Fatalf("Value = %q, want %q", cookie.Value, "the-token")
	}
	if cookie.Path != "/api" {
		t.Fatalf("Path = %q, want /api", cookie.Path)
	}
	if !cookie.HttpOnly {
		t.Fatal("HttpOnly = false, want true")
	}
	if !cookie.Secure {
		t.Fatal("Secure = false, want true (config.CookieSecure = true)")
	}
	if cookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("SameSite = %v, want Strict", cookie.SameSite)
	}
	if cookie.MaxAge < 2699 || cookie.MaxAge > 2700 {
		t.Fatalf("MaxAge = %d, want ~2700 (45m)", cookie.MaxAge)
	}
}

func TestSessionCookie_SecureFalseWhenConfigDisablesIt(t *testing.T) {
	srv := NewServer(Config{CookieSecure: false})
	cookie := srv.sessionCookie("t", time.Now().Add(time.Hour))
	if cookie.Secure {
		t.Fatal("Secure = true, want false (config.CookieSecure = false)")
	}
}

func TestSessionCookie_ExpiresImmediatelyForAnAlreadyExpiredToken(t *testing.T) {
	srv := NewServer(Config{})
	cookie := srv.sessionCookie("t", time.Now().Add(-time.Minute))
	// http.Cookie serializes MaxAge==0 as *no* Max-Age attribute at all (a
	// session cookie - never expires on its own), the opposite of what an
	// already-expired token needs; only MaxAge<0 emits "Max-Age=0", which
	// is what actually tells the browser to delete it now. Assert both the
	// Go-level field and the literal header text, so a regression back to
	// the wrong floor is caught even if only one is checked.
	if cookie.MaxAge >= 0 {
		t.Fatalf("MaxAge = %d, want negative for an already-expired token", cookie.MaxAge)
	}
	if !strings.Contains(cookie.String(), "Max-Age=0") {
		t.Fatalf("Set-Cookie = %q, want it to contain Max-Age=0 (immediate expiry)", cookie.String())
	}
}

func TestSessionCookie_MaxAgeTracksChangedExpiry(t *testing.T) {
	srv := NewServer(Config{})
	short := srv.sessionCookie("t", time.Now().Add(5*time.Minute))
	long := srv.sessionCookie("t", time.Now().Add(50*time.Minute))
	if !(long.MaxAge > short.MaxAge) {
		t.Fatalf("MaxAge did not track a changed expiry: short=%d long=%d", short.MaxAge, long.MaxAge)
	}
}

func TestHandleLogin_SetsSessionCookieOn200(t *testing.T) {
	srv := NewServer(Config{})
	createTestUser(t, srv, "alice", "s3cret-pass", domain.RoleOperator)

	body, _ := json.Marshal(loginRequest{Name: "alice", Password: "s3cret-pass"})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != sessionCookieName {
		t.Fatalf("cookies = %+v, want exactly one %q cookie", cookies, sessionCookieName)
	}
	var resp loginResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if cookies[0].Value != resp.Token {
		t.Fatal("cookie does not carry the same token as the response body")
	}
}

func TestHandleLogin_NoCookieOn401(t *testing.T) {
	srv := NewServer(Config{})
	createTestUser(t, srv, "bob", "correct-password", domain.RoleViewer)

	body, _ := json.Marshal(loginRequest{Name: "bob", Password: "wrong-password"})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
	if len(w.Result().Cookies()) != 0 {
		t.Fatalf("cookies = %+v, want none on a failed login", w.Result().Cookies())
	}
}

func TestHandleLogin_BodyUnchangedFromBearerFormat(t *testing.T) {
	srv := NewServer(Config{})
	createTestUser(t, srv, "alice", "s3cret-pass", domain.RoleOperator)

	body, _ := json.Marshal(loginRequest{Name: "alice", Password: "s3cret-pass"})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(raw) != 2 {
		t.Fatalf("response has fields %v, want exactly token and expires_at (unchanged from today)", raw)
	}
	if _, ok := raw["token"]; !ok {
		t.Fatal("response missing token field")
	}
	if _, ok := raw["expires_at"]; !ok {
		t.Fatal("response missing expires_at field")
	}
}

func TestHandleLogout_ClearsCookieWithValidSession(t *testing.T) {
	srv := NewServer(Config{})
	user := ensureTestUser(t, srv, "logout-user", domain.RoleViewer)
	token, _, err := srv.tokenIssuer.Issue(user)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	beforeVersion := user.TokenVersion

	req := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	req.Header.Set(csrfHeaderName, "1")
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", w.Code)
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != sessionCookieName || cookies[0].MaxAge >= 0 {
		t.Fatalf("cookies = %+v, want a clearing Set-Cookie (negative Max-Age)", cookies)
	}

	after, err := srv.userStore.GetUser(user.Name)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if after.TokenVersion != beforeVersion {
		t.Fatalf("TokenVersion changed from %d to %d, want unchanged by logout", beforeVersion, after.TokenVersion)
	}
}

func TestHandleLogout_SucceedsWithoutCredentials(t *testing.T) {
	srv := NewServer(Config{})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	req.Header.Set(csrfHeaderName, "1")
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", w.Code)
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != sessionCookieName || cookies[0].MaxAge >= 0 {
		t.Fatalf("cookies = %+v, want a clearing Set-Cookie even with no session", cookies)
	}
}

// TestHandleLogout_RejectsWithoutCSRFHeader locks in the fix for a
// cross-site forced-logout: logout takes no credential, so RequireAuth's
// CSRF gate never runs for it; without this check here, an auto-submitting
// cross-site form could force any signed-in visitor's session to end (the
// browser accepts this response's Set-Cookie regardless of SameSite, which
// governs attaching an existing cookie to a request, not accepting one from
// a response). The header is not a credential, so a session-less caller
// that does send it is still served - see the test above.
func TestHandleLogout_RejectsWithoutCSRFHeader(t *testing.T) {
	srv := NewServer(Config{})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.Code)
	}
	if len(w.Result().Cookies()) != 0 {
		t.Fatalf("cookies = %+v, want none - a request without the header must not touch the cookie at all", w.Result().Cookies())
	}
}

func TestHandleMe_ReturnsStoredRoleNotTokenRole(t *testing.T) {
	srv := newAuthTestServer(t)
	// The account is genuinely a viewer; the token carries a forged admin
	// claim (same trick as TestRequireAuth_TokenRoleClaimIgnored_DecidesOnStoredRole)
	// at the account's real token version, so it verifies. handleMe must
	// reflect the stored role, not this claim.
	user := ensureTestUser(t, srv, "me-user", domain.RoleViewer)
	forged := domain.User{Name: user.Name, Role: domain.RoleAdmin, TokenVersion: user.TokenVersion}
	token, _, err := srv.tokenIssuer.Issue(forged)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), token) {
		t.Fatal("response body leaked the token")
	}
	var resp meResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Name != user.Name {
		t.Fatalf("Name = %q, want %q", resp.Name, user.Name)
	}
	if resp.Role != string(domain.RoleViewer) {
		t.Fatalf("Role = %q, want the stored role %q, not the token's forged admin claim", resp.Role, domain.RoleViewer)
	}
}

func TestHandleMe_401WithoutCredentials(t *testing.T) {
	srv := newAuthTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

func TestHandleMe_401WhenClaimsMissingFromContext(t *testing.T) {
	srv := &Server{}
	req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	w := httptest.NewRecorder()
	srv.handleMe(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

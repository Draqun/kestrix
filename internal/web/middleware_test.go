package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"

	"kestrix/internal/domain"
)

func newAuthTestServer(t *testing.T) *Server {
	t.Helper()
	srv := NewServer(Config{})
	srv.grantStore = &fakeGrantStore{grants: map[[2]string]domain.Role{}}
	return srv
}

type fakeGrantStore struct {
	grants map[[2]string]domain.Role
}

func (f *fakeGrantStore) SetProjectGrant(userName, projectName string, role domain.Role) error {
	f.grants[[2]string{userName, projectName}] = role
	return nil
}
func (f *fakeGrantStore) RemoveProjectGrant(userName, projectName string) error {
	delete(f.grants, [2]string{userName, projectName})
	return nil
}
func (f *fakeGrantStore) ListGrantsForUser(userName string) ([]domain.ProjectGrant, error) {
	return nil, nil
}
func (f *fakeGrantStore) ListGrantsForProject(projectName string) ([]domain.ProjectGrant, error) {
	return nil, nil
}
func (f *fakeGrantStore) ResolveGrant(userName, projectName string) (domain.Role, bool, error) {
	role, ok := f.grants[[2]string{userName, projectName}]
	return role, ok, nil
}

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

// contextWithClaims places claims on ctx exactly as RequireAuth would,
// letting RequireRole tests run without going through a real token.
func contextWithClaims(ctx context.Context, claims domain.Claims) context.Context {
	return context.WithValue(ctx, claimsContextKey, claims)
}

// requestWithClaims builds a bare request carrying claims on its context,
// for tests that exercise RequireRole in isolation from RequireAuth.
func requestWithClaims(claims domain.Claims) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	return req.WithContext(contextWithClaims(req.Context(), claims))
}

// expiredToken builds a well-formed HS256 token, signed with the given key,
// whose exp already passed - exercising RequireAuth's expiry rejection
// without reaching into internal/auth's unexported claim type.
func expiredToken(t *testing.T, key []byte) string {
	t.Helper()
	claims := jwt.MapClaims{
		"sub":  "expired-user",
		"role": string(domain.RoleViewer),
		"iat":  jwt.NewNumericDate(time.Now().Add(-2 * time.Hour)),
		"exp":  jwt.NewNumericDate(time.Now().Add(-time.Hour)),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(key)
	if err != nil {
		t.Fatalf("sign expired token: %v", err)
	}
	return signed
}

func TestRequireAuth_MissingHeader(t *testing.T) {
	srv := newAuthTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	w := httptest.NewRecorder()
	srv.RequireAuth(okHandler()).ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

func TestRequireAuth_NonBearerHeader(t *testing.T) {
	srv := newAuthTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
	w := httptest.NewRecorder()
	srv.RequireAuth(okHandler()).ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

func TestRequireAuth_NilTokenIssuer(t *testing.T) {
	srv := &Server{}
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("Authorization", "Bearer whatever")
	w := httptest.NewRecorder()
	srv.RequireAuth(okHandler()).ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

func TestRequireAuth_InvalidToken(t *testing.T) {
	srv := newAuthTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("Authorization", "Bearer not-a-real-token")
	w := httptest.NewRecorder()
	srv.RequireAuth(okHandler()).ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

func TestRequireAuth_ExpiredToken(t *testing.T) {
	srv := newAuthTestServer(t)
	key, err := testSigningKey()
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("Authorization", "Bearer "+expiredToken(t, key))
	w := httptest.NewRecorder()
	srv.RequireAuth(okHandler()).ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

func TestRequireAuth_ValidTokenReachesHandler(t *testing.T) {
	srv := newAuthTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("Authorization", authToken(t, srv, domain.RoleViewer))
	w := httptest.NewRecorder()
	called := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		claims, ok := claimsFromContext(r.Context())
		if !ok {
			t.Fatal("claims missing from context")
		}
		if claims.Role != domain.RoleViewer {
			t.Fatalf("claims.Role = %s, want viewer", claims.Role)
		}
		w.WriteHeader(http.StatusOK)
	})
	srv.RequireAuth(handler).ServeHTTP(w, req)
	if !called {
		t.Fatal("handler was not called")
	}
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}

func TestRequireRole_MissingClaims(t *testing.T) {
	srv := newAuthTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	w := httptest.NewRecorder()
	srv.RequireRole(domain.RoleViewer)(okHandler()).ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

func TestRequireRole_GlobalRoleBelowMinimum(t *testing.T) {
	srv := newAuthTestServer(t)
	req := requestWithClaims(domain.Claims{UserName: "v", Role: domain.RoleViewer})
	w := httptest.NewRecorder()
	srv.RequireRole(domain.RoleOperator)(okHandler()).ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.Code)
	}
}

func TestRequireRole_GlobalRoleMeetsMinimum(t *testing.T) {
	srv := newAuthTestServer(t)
	req := requestWithClaims(domain.Claims{UserName: "o", Role: domain.RoleOperator})
	w := httptest.NewRecorder()
	called := false
	srv.RequireRole(domain.RoleOperator)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(w, req)
	if !called || w.Code != http.StatusOK {
		t.Fatalf("called=%v status=%d, want called=true status=200", called, w.Code)
	}
}

// projectScopedRouter builds a minimal router carrying claims through
// context (standing in for RequireAuth) and RequireRole guarding a
// project-scoped route, for the grant-resolution tests below.
func projectScopedRouter(srv *Server, claims domain.Claims, min domain.Role) http.Handler {
	router := chi.NewRouter()
	inject := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(contextWithClaims(r.Context(), claims)))
		})
	}
	router.With(inject, srv.RequireRole(min)).Post("/projects/{name}/run", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	return router
}

func TestRequireRole_GrantRaisesViewerOnOneProject(t *testing.T) {
	srv := newAuthTestServer(t)
	srv.grantStore.(*fakeGrantStore).grants[[2]string{"v", "alpha"}] = domain.RoleOperator

	router := projectScopedRouter(srv, domain.Claims{UserName: "v", Role: domain.RoleViewer}, domain.RoleOperator)
	req := httptest.NewRequest(http.MethodPost, "/projects/alpha/run", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (grant should raise viewer to operator on alpha)", w.Code)
	}
}

func TestRequireRole_GrantDoesNotApplyToOtherProjects(t *testing.T) {
	srv := newAuthTestServer(t)
	srv.grantStore.(*fakeGrantStore).grants[[2]string{"v", "alpha"}] = domain.RoleOperator

	router := projectScopedRouter(srv, domain.Claims{UserName: "v", Role: domain.RoleViewer}, domain.RoleOperator)
	req := httptest.NewRequest(http.MethodPost, "/projects/beta/run", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (grant on alpha must not apply to beta)", w.Code)
	}
}

func TestRequireRole_GrantCannotDemote(t *testing.T) {
	srv := newAuthTestServer(t)
	srv.grantStore.(*fakeGrantStore).grants[[2]string{"o", "alpha"}] = domain.RoleViewer

	router := projectScopedRouter(srv, domain.Claims{UserName: "o", Role: domain.RoleOperator}, domain.RoleOperator)
	req := httptest.NewRequest(http.MethodPost, "/projects/alpha/run", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (a viewer grant must not demote a global operator)", w.Code)
	}
}

func TestRequireRole_CallerSuppliedRoleIgnored(t *testing.T) {
	srv := newAuthTestServer(t)
	req := requestWithClaims(domain.Claims{UserName: "v", Role: domain.RoleViewer})
	req.Header.Set("X-Role", "admin")
	q := req.URL.Query()
	q.Set("role", "admin")
	req.URL.RawQuery = q.Encode()
	w := httptest.NewRecorder()
	srv.RequireRole(domain.RoleAdmin)(okHandler()).ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (header/query role must be ignored)", w.Code)
	}
}

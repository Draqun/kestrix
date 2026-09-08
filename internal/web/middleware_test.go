package web

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"

	"kestrix/internal/auth"
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

// ── Store-authoritative role and token-version revocation (rbac-hardening D1/D2) ──

func TestRequireAuth_NilUserStore(t *testing.T) {
	srv := &Server{tokenIssuer: auth.JWTIssuer{Key: testSigningKey}}
	token, _, err := srv.tokenIssuer.Issue(domain.User{Name: "x", Role: domain.RoleViewer, TokenVersion: 1})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	srv.RequireAuth(okHandler()).ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 when no user store is configured", w.Code)
	}
}

func TestRequireAuth_TokenForDeletedAccount(t *testing.T) {
	srv := newAuthTestServer(t)
	user := ensureTestUser(t, srv, "soon-deleted", domain.RoleOperator)
	token, _, err := srv.tokenIssuer.Issue(user)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if err := srv.userStore.DeleteUser(user.Name); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	srv.RequireAuth(okHandler()).ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 for a token naming a deleted account", w.Code)
	}
}

func TestRequireAuth_TokenRoleClaimIgnored_DecidesOnStoredRole(t *testing.T) {
	srv := newAuthTestServer(t)
	user := ensureTestUser(t, srv, "claim-mismatch", domain.RoleViewer)
	key, err := testSigningKey()
	if err != nil {
		t.Fatal(err)
	}
	// A token whose role claim (admin) does not match the account's stored
	// role (viewer), but whose version still matches - e.g. an
	// externally-crafted or otherwise inconsistent token. RequireAuth must
	// never let the claim's role reach the request.
	ver := user.TokenVersion
	forged := jwt.MapClaims{
		"sub":  user.Name,
		"role": string(domain.RoleAdmin),
		"ver":  ver,
		"iat":  jwt.NewNumericDate(time.Now()),
		"exp":  jwt.NewNumericDate(time.Now().Add(time.Hour)),
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, forged).SignedString(key)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("Authorization", "Bearer "+signed)
	w := httptest.NewRecorder()
	var gotRole domain.Role
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, _ := claimsFromContext(r.Context())
		gotRole = claims.Role
		w.WriteHeader(http.StatusOK)
	})
	srv.RequireAuth(handler).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if gotRole != domain.RoleViewer {
		t.Fatalf("claims.Role = %s, want the stored viewer role, not the token's admin claim", gotRole)
	}
}

func TestRequireAuth_UserStoreFailure_Returns503DistinctFrom401(t *testing.T) {
	srv := newAuthTestServer(t)
	user := domain.User{Name: "erred", Role: domain.RoleViewer, TokenVersion: 1}
	backing := newErrUserStore()
	backing.users[user.Name] = user
	backing.GetErr = errors.New("dial tcp 10.0.0.5:5432: connection refused")
	srv.userStore = backing
	token, _, err := srv.tokenIssuer.Issue(user)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	srv.RequireAuth(okHandler()).ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 (distinct from 401) for a user-store failure", w.Code)
	}
	if strings.Contains(w.Body.String(), "10.0.0.5") || strings.Contains(w.Body.String(), "connection refused") {
		t.Fatalf("response body leaked the underlying store error: %s", w.Body.String())
	}
}

func TestResolveIdentity_CachedByAccountNameOnly(t *testing.T) {
	srv := newAuthTestServer(t)
	backing := newErrUserStore()
	backing.users["shared"] = domain.User{Name: "shared", Role: domain.RoleOperator, TokenVersion: 4}
	srv.userStore = backing
	srv.identityCache = auth.NewInMemoryIdentityCache(time.Minute, 0, time.Now)

	first, err := srv.resolveIdentity("shared")
	if err != nil {
		t.Fatalf("resolveIdentity: %v", err)
	}
	second, err := srv.resolveIdentity("shared")
	if err != nil {
		t.Fatalf("resolveIdentity: %v", err)
	}
	// Two tokens for "shared" - whatever they individually claim - would
	// both resolve through this same call, so identical results here is
	// exactly what makes two tokens for one account unable to disagree.
	if first != second {
		t.Fatalf("resolveIdentity(\"shared\") = %+v then %+v, want identical results", first, second)
	}
	if backing.getCalls != 1 {
		t.Fatalf("GetUser called %d times, want 1 (the second resolveIdentity must hit the cache)", backing.getCalls)
	}
}

func TestRequireAuth_RoleChangeInvalidatesCache_NewTokenWorksImmediately(t *testing.T) {
	srv := newAuthTestServer(t)
	srv.identityCache = auth.NewInMemoryIdentityCache(time.Minute, 0, time.Now)

	user := ensureTestUser(t, srv, "promoted", domain.RoleViewer)
	oldToken, _, err := srv.tokenIssuer.Issue(user)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	// Warm the cache with the pre-change identity.
	warmReq := httptest.NewRequest(http.MethodGet, "/x", nil)
	warmReq.Header.Set("Authorization", "Bearer "+oldToken)
	warmW := httptest.NewRecorder()
	srv.RequireAuth(okHandler()).ServeHTTP(warmW, warmReq)
	if warmW.Code != http.StatusOK {
		t.Fatalf("warm-up request status = %d, want 200", warmW.Code)
	}

	// Promote through the real handler, which must invalidate the cache
	// entry it just warmed - not just bump the store's token_version.
	updateReq := httptest.NewRequest(http.MethodPut, "/api/users/promoted/role", jsonBody(updateUserRoleRequest{Role: domain.RoleAdmin}))
	updateReq = withURLParam(updateReq, "user", "promoted")
	updateW := httptest.NewRecorder()
	srv.handleUpdateUserRole(updateW, updateReq)
	if updateW.Code != http.StatusOK {
		t.Fatalf("handleUpdateUserRole status = %d, want 200, body=%s", updateW.Code, updateW.Body.String())
	}

	updated, err := srv.userStore.GetUser("promoted")
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	newToken, _, err := srv.tokenIssuer.Issue(updated)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("Authorization", "Bearer "+newToken)
	w := httptest.NewRecorder()
	var gotRole domain.Role
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, _ := claimsFromContext(r.Context())
		gotRole = claims.Role
		w.WriteHeader(http.StatusOK)
	})
	srv.RequireAuth(handler).ServeHTTP(w, req)
	// If the stale cache entry (old version) had survived, this new token
	// (new version) would mismatch it and be rejected with 401 - so 200
	// here is proof the handler's invalidation ran, not just a passing
	// assertion about the role.
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 - the stale cache entry must have been invalidated by the role change", w.Code)
	}
	if gotRole != domain.RoleAdmin {
		t.Fatalf("claims.Role = %s, want admin immediately after promotion, no wait for the cache TTL", gotRole)
	}
}

func TestHandleCreateUser_InvalidatesStaleNegativeCacheEntry(t *testing.T) {
	srv := newAuthTestServer(t)
	srv.identityCache = auth.NewInMemoryIdentityCache(time.Minute, 0, time.Now)

	// A token naming an account that doesn't exist yet - forged, or simply
	// presented before the account was created - caches a negative
	// identity result for that name.
	notYetUser := domain.User{Name: "newbie", Role: domain.RoleViewer, TokenVersion: 1}
	earlyToken, _, err := srv.tokenIssuer.Issue(notYetUser)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	warmReq := httptest.NewRequest(http.MethodGet, "/x", nil)
	warmReq.Header.Set("Authorization", "Bearer "+earlyToken)
	warmW := httptest.NewRecorder()
	srv.RequireAuth(okHandler()).ServeHTTP(warmW, warmReq)
	if warmW.Code != http.StatusUnauthorized {
		t.Fatalf("pre-creation request status = %d, want 401 (account does not exist yet)", warmW.Code)
	}

	// Create the account through the real handler, which must invalidate
	// the negative cache entry it just warmed.
	createReq := httptest.NewRequest(http.MethodPost, "/api/users", jsonBody(createUserRequest{
		Name: "newbie", Password: "irrelevant-password", Role: domain.RoleViewer,
	}))
	createW := httptest.NewRecorder()
	srv.handleCreateUser(createW, createReq)
	if createW.Code != http.StatusCreated {
		t.Fatalf("handleCreateUser status = %d, want 201, body=%s", createW.Code, createW.Body.String())
	}

	created, err := srv.userStore.GetUser("newbie")
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	newToken, _, err := srv.tokenIssuer.Issue(created)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("Authorization", "Bearer "+newToken)
	w := httptest.NewRecorder()
	srv.RequireAuth(okHandler()).ServeHTTP(w, req)
	// If the stale negative cache entry had survived, this genuinely new
	// account's token would still be rejected as unknown until the TTL
	// expired.
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 - account creation must invalidate a stale negative cache entry", w.Code)
	}
}

func TestRequireAuth_401ResponsesAreIdentical(t *testing.T) {
	srv := newAuthTestServer(t)
	user := ensureTestUser(t, srv, "consistent", domain.RoleViewer)

	otherKeyIssuer := auth.JWTIssuer{Key: func() ([]byte, error) {
		key := make([]byte, 32)
		for i := range key {
			key[i] = byte(255 - i)
		}
		return key, nil
	}}
	badSigToken, _, err := otherKeyIssuer.Issue(user)
	if err != nil {
		t.Fatalf("Issue(other key): %v", err)
	}

	unknownToken, _, err := srv.tokenIssuer.Issue(domain.User{Name: "never-existed", Role: domain.RoleViewer, TokenVersion: 1})
	if err != nil {
		t.Fatalf("Issue(unknown account): %v", err)
	}

	staleToken, _, err := srv.tokenIssuer.Issue(user)
	if err != nil {
		t.Fatalf("Issue(stale): %v", err)
	}
	if err := srv.userStore.UpdateUserRole(user.Name, domain.RoleAdmin); err != nil {
		t.Fatalf("UpdateUserRole: %v", err)
	}

	var codes []int
	var bodies []string
	for _, token := range []string{badSigToken, unknownToken, staleToken} {
		req := httptest.NewRequest(http.MethodGet, "/x", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		srv.RequireAuth(okHandler()).ServeHTTP(w, req)
		codes = append(codes, w.Code)
		bodies = append(bodies, w.Body.String())
	}
	for i := 1; i < len(codes); i++ {
		if codes[i] != codes[0] || bodies[i] != bodies[0] {
			t.Fatalf("response %d = (%d, %q), want identical to response 0 = (%d, %q)",
				i, codes[i], bodies[i], codes[0], bodies[0])
		}
	}
	if codes[0] != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", codes[0])
	}
}

// ── Fail-closed authorization store errors (rbac-hardening D3) ──

func TestRequireRole_GrantStoreFailure_Returns503(t *testing.T) {
	srv := newAuthTestServer(t)
	errGrants := newErrGrantStore()
	errGrants.ResolveErr = errors.New("dial tcp: connection reset by peer")
	srv.grantStore = errGrants

	router := chi.NewRouter()
	inject := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// A global admin: if the grant-store failure ever fell back to
			// the global role instead of failing closed, this request
			// would still pass, masking the bug. It must not.
			next.ServeHTTP(w, r.WithContext(contextWithClaims(r.Context(), domain.Claims{UserName: "a", Role: domain.RoleAdmin})))
		})
	}
	called := false
	router.With(inject, srv.RequireRole(domain.RoleOperator)).Post("/projects/{name}/run", func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodPost, "/projects/alpha/run", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", w.Code)
	}
	if called {
		t.Fatal("handler ran despite a grant-store failure")
	}
	if strings.Contains(w.Body.String(), "connection reset") {
		t.Fatalf("response body leaked the underlying store error: %s", w.Body.String())
	}
}

func TestRequireRole_AbsentGrantStillResolvesToGlobalRole(t *testing.T) {
	srv := newAuthTestServer(t) // fakeGrantStore with no grants set at all
	router := projectScopedRouter(srv, domain.Claims{UserName: "o", Role: domain.RoleOperator}, domain.RoleOperator)
	req := httptest.NewRequest(http.MethodPost, "/projects/alpha/run", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 - an absent grant must fall back to the global role, not fail closed", w.Code)
	}
}

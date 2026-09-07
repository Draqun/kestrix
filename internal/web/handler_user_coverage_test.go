package web

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"kestrix/internal/domain"
)

// errUserStore is a domain.UserStore whose failure on any operation is
// injectable, for the "internal error" branches real stores rarely exercise
// through their own error taxonomy.
type errUserStore struct {
	users map[string]domain.User

	CreateErr     error
	GetErr        error
	GetErrAfter   int // GetErr only takes effect once GetUser has been called more than this many times
	getCalls      int
	ListErr       error
	UpdateRoleErr error
	DeleteErr     error
}

func newErrUserStore() *errUserStore {
	return &errUserStore{users: map[string]domain.User{}}
}

func (s *errUserStore) CreateUser(name string, role domain.Role, _ string) (domain.User, error) {
	if s.CreateErr != nil {
		return domain.User{}, s.CreateErr
	}
	u := domain.User{Name: name, Role: role}
	s.users[name] = u
	return u, nil
}

func (s *errUserStore) GetUser(name string) (domain.User, error) {
	s.getCalls++
	if s.GetErr != nil && s.getCalls > s.GetErrAfter {
		return domain.User{}, s.GetErr
	}
	u, ok := s.users[name]
	if !ok {
		return domain.User{}, errors.New("not found")
	}
	return u, nil
}

func (s *errUserStore) GetUserCredential(name string) (domain.UserCredential, error) {
	return domain.UserCredential{}, errors.New("not implemented")
}

func (s *errUserStore) ListUsers() ([]domain.User, error) {
	if s.ListErr != nil {
		return nil, s.ListErr
	}
	var out []domain.User
	for _, u := range s.users {
		out = append(out, u)
	}
	return out, nil
}

func (s *errUserStore) UpdateUserRole(name string, role domain.Role) error {
	if s.UpdateRoleErr != nil {
		return s.UpdateRoleErr
	}
	u := s.users[name]
	u.Role = role
	s.users[name] = u
	return nil
}

func (s *errUserStore) UpdateUserPassword(name string, _ string) error { return nil }

func (s *errUserStore) DeleteUser(name string) error {
	if s.DeleteErr != nil {
		return s.DeleteErr
	}
	delete(s.users, name)
	return nil
}

// errGrantStore is a domain.GrantStore whose failure on any operation is
// injectable.
type errGrantStore struct {
	ListErr   error
	RemoveErr error
	SetErr    error
	grants    map[[2]string]domain.Role
}

func newErrGrantStore() *errGrantStore {
	return &errGrantStore{grants: map[[2]string]domain.Role{}}
}

func (g *errGrantStore) SetProjectGrant(userName, projectName string, role domain.Role) error {
	if g.SetErr != nil {
		return g.SetErr
	}
	g.grants[[2]string{userName, projectName}] = role
	return nil
}

func (g *errGrantStore) RemoveProjectGrant(userName, projectName string) error {
	if g.RemoveErr != nil {
		return g.RemoveErr
	}
	delete(g.grants, [2]string{userName, projectName})
	return nil
}

func (g *errGrantStore) ListGrantsForUser(userName string) ([]domain.ProjectGrant, error) {
	if g.ListErr != nil {
		return nil, g.ListErr
	}
	var out []domain.ProjectGrant
	for k, role := range g.grants {
		if k[0] == userName {
			out = append(out, domain.ProjectGrant{UserName: k[0], ProjectName: k[1], Role: role})
		}
	}
	return out, nil
}

func (g *errGrantStore) ListGrantsForProject(projectName string) ([]domain.ProjectGrant, error) {
	return nil, nil
}

func (g *errGrantStore) ResolveGrant(userName, projectName string) (domain.Role, bool, error) {
	role, ok := g.grants[[2]string{userName, projectName}]
	return role, ok, nil
}

func jsonBody(v interface{}) *bytes.Reader {
	b, _ := json.Marshal(v)
	return bytes.NewReader(b)
}

func withURLParam(r *http.Request, key, value string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, value)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

// ── requireUserStore / nil-dependency branches ──────────────────────

func TestRequireUserStore_NilStore(t *testing.T) {
	srv := &Server{}
	w := httptest.NewRecorder()
	if got := srv.requireUserStore(w); got != nil {
		t.Fatalf("requireUserStore = %v, want nil", got)
	}
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", w.Code)
	}
}

func TestHandleListUsers_NilStore(t *testing.T) {
	srv := &Server{}
	w := httptest.NewRecorder()
	srv.handleListUsers(w, httptest.NewRequest(http.MethodGet, "/api/users", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", w.Code)
	}
}

func TestHandleListUsers_StoreError(t *testing.T) {
	store := newErrUserStore()
	store.ListErr = errors.New("boom")
	srv := &Server{userStore: store}
	w := httptest.NewRecorder()
	srv.handleListUsers(w, httptest.NewRequest(http.MethodGet, "/api/users", nil))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
}

func TestHandleCreateUser_NilStore(t *testing.T) {
	srv := &Server{}
	w := httptest.NewRecorder()
	srv.handleCreateUser(w, httptest.NewRequest(http.MethodPost, "/api/users", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", w.Code)
	}
}

func TestHandleCreateUser_NilPasswordHasher(t *testing.T) {
	srv := &Server{userStore: newErrUserStore()}
	w := httptest.NewRecorder()
	body := jsonBody(createUserRequest{Name: "a", Password: "p", Role: domain.RoleViewer})
	srv.handleCreateUser(w, httptest.NewRequest(http.MethodPost, "/api/users", body))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", w.Code)
	}
}

func TestHandleCreateUser_InvalidJSON(t *testing.T) {
	srv := &Server{userStore: newErrUserStore(), passwordHasher: &fakeHasher{}}
	w := httptest.NewRecorder()
	srv.handleCreateUser(w, httptest.NewRequest(http.MethodPost, "/api/users", bytes.NewReader([]byte("{bad"))))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestHandleCreateUser_MissingFields(t *testing.T) {
	srv := &Server{userStore: newErrUserStore(), passwordHasher: &fakeHasher{}}
	w := httptest.NewRecorder()
	body := jsonBody(createUserRequest{Name: "", Password: "", Role: domain.RoleViewer})
	srv.handleCreateUser(w, httptest.NewRequest(http.MethodPost, "/api/users", body))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestHandleCreateUser_HashFails(t *testing.T) {
	srv := &Server{userStore: newErrUserStore(), passwordHasher: &fakeHasher{hashErr: errors.New("boom")}}
	w := httptest.NewRecorder()
	body := jsonBody(createUserRequest{Name: "a", Password: "p", Role: domain.RoleViewer})
	srv.handleCreateUser(w, httptest.NewRequest(http.MethodPost, "/api/users", body))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
}

func TestHandleCreateUser_StoreErrorOtherThanExists(t *testing.T) {
	store := newErrUserStore()
	store.CreateErr = errors.New("boom")
	srv := &Server{userStore: store, passwordHasher: &fakeHasher{}}
	w := httptest.NewRecorder()
	body := jsonBody(createUserRequest{Name: "a", Password: "p", Role: domain.RoleViewer})
	srv.handleCreateUser(w, httptest.NewRequest(http.MethodPost, "/api/users", body))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
}

func TestHandleUpdateUserRole_NilStore(t *testing.T) {
	srv := &Server{}
	w := httptest.NewRecorder()
	srv.handleUpdateUserRole(w, httptest.NewRequest(http.MethodPut, "/api/users/a/role", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", w.Code)
	}
}

func TestHandleUpdateUserRole_InvalidJSON(t *testing.T) {
	srv := &Server{userStore: newErrUserStore()}
	w := httptest.NewRecorder()
	req := withURLParam(httptest.NewRequest(http.MethodPut, "/api/users/a/role", bytes.NewReader([]byte("{bad"))), "user", "a")
	srv.handleUpdateUserRole(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestHandleUpdateUserRole_InvalidRole(t *testing.T) {
	srv := &Server{userStore: newErrUserStore()}
	w := httptest.NewRecorder()
	req := withURLParam(httptest.NewRequest(http.MethodPut, "/api/users/a/role", jsonBody(updateUserRoleRequest{Role: "bogus"})), "user", "a")
	srv.handleUpdateUserRole(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestHandleUpdateUserRole_StoreErrorOtherThanNotFound(t *testing.T) {
	store := newErrUserStore()
	store.UpdateRoleErr = errors.New("boom")
	srv := &Server{userStore: store}
	w := httptest.NewRecorder()
	req := withURLParam(httptest.NewRequest(http.MethodPut, "/api/users/a/role", jsonBody(updateUserRoleRequest{Role: domain.RoleAdmin})), "user", "a")
	srv.handleUpdateUserRole(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
}

func TestHandleUpdateUserRole_GetAfterUpdateFails(t *testing.T) {
	store := newErrUserStore()
	if _, err := store.CreateUser("a", domain.RoleViewer, "h"); err != nil {
		t.Fatal(err)
	}
	srv := &Server{userStore: store}
	// Fail GetUser only after the role update itself has already succeeded.
	store.GetErr = errors.New("boom")
	w := httptest.NewRecorder()
	req := withURLParam(httptest.NewRequest(http.MethodPut, "/api/users/a/role", jsonBody(updateUserRoleRequest{Role: domain.RoleAdmin})), "user", "a")
	srv.handleUpdateUserRole(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
}

func TestHandleSetUserGrants_NilStore(t *testing.T) {
	srv := &Server{}
	w := httptest.NewRecorder()
	srv.handleSetUserGrants(w, httptest.NewRequest(http.MethodPut, "/api/users/a/grants", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", w.Code)
	}
}

func TestHandleSetUserGrants_NilGrantStore(t *testing.T) {
	srv := &Server{userStore: newErrUserStore()}
	w := httptest.NewRecorder()
	srv.handleSetUserGrants(w, httptest.NewRequest(http.MethodPut, "/api/users/a/grants", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", w.Code)
	}
}

func TestHandleSetUserGrants_UserNotFound(t *testing.T) {
	srv := &Server{userStore: newErrUserStore(), grantStore: newErrGrantStore()}
	w := httptest.NewRecorder()
	req := withURLParam(httptest.NewRequest(http.MethodPut, "/api/users/ghost/grants", nil), "user", "ghost")
	srv.handleSetUserGrants(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

func setGrantsBody(entries ...struct {
	ProjectName string
	Role        domain.Role
}) *bytes.Reader {
	req := setGrantsRequest{}
	for _, e := range entries {
		req.Grants = append(req.Grants, struct {
			ProjectName string      `json:"project_name"`
			Role        domain.Role `json:"role"`
		}{ProjectName: e.ProjectName, Role: e.Role})
	}
	return jsonBody(req)
}

func TestHandleSetUserGrants_InvalidJSON(t *testing.T) {
	store := newErrUserStore()
	store.users["a"] = domain.User{Name: "a"}
	srv := &Server{userStore: store, grantStore: newErrGrantStore()}
	w := httptest.NewRecorder()
	req := withURLParam(httptest.NewRequest(http.MethodPut, "/api/users/a/grants", bytes.NewReader([]byte("{bad"))), "user", "a")
	srv.handleSetUserGrants(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestHandleSetUserGrants_InvalidGrantRole(t *testing.T) {
	store := newErrUserStore()
	store.users["a"] = domain.User{Name: "a"}
	srv := &Server{userStore: store, grantStore: newErrGrantStore()}
	w := httptest.NewRecorder()
	body := setGrantsBody(struct {
		ProjectName string
		Role        domain.Role
	}{"alpha", "bogus"})
	req := withURLParam(httptest.NewRequest(http.MethodPut, "/api/users/a/grants", body), "user", "a")
	srv.handleSetUserGrants(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestHandleSetUserGrants_ListError(t *testing.T) {
	store := newErrUserStore()
	store.users["a"] = domain.User{Name: "a"}
	grants := newErrGrantStore()
	grants.ListErr = errors.New("boom")
	srv := &Server{userStore: store, grantStore: grants}
	w := httptest.NewRecorder()
	req := withURLParam(httptest.NewRequest(http.MethodPut, "/api/users/a/grants", setGrantsBody()), "user", "a")
	srv.handleSetUserGrants(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
}

func TestHandleSetUserGrants_RemoveError(t *testing.T) {
	store := newErrUserStore()
	store.users["a"] = domain.User{Name: "a"}
	grants := newErrGrantStore()
	grants.grants[[2]string{"a", "alpha"}] = domain.RoleViewer
	grants.RemoveErr = errors.New("boom")
	srv := &Server{userStore: store, grantStore: grants}
	w := httptest.NewRecorder()
	// Desired set is empty, so the existing "alpha" grant must be removed.
	req := withURLParam(httptest.NewRequest(http.MethodPut, "/api/users/a/grants", setGrantsBody()), "user", "a")
	srv.handleSetUserGrants(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
}

func TestHandleSetUserGrants_SetError(t *testing.T) {
	store := newErrUserStore()
	store.users["a"] = domain.User{Name: "a"}
	grants := newErrGrantStore()
	grants.SetErr = errors.New("boom")
	srv := &Server{userStore: store, grantStore: grants}
	w := httptest.NewRecorder()
	body := setGrantsBody(struct {
		ProjectName string
		Role        domain.Role
	}{"alpha", domain.RoleOperator})
	req := withURLParam(httptest.NewRequest(http.MethodPut, "/api/users/a/grants", body), "user", "a")
	srv.handleSetUserGrants(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
}

func TestHandleSetUserGrants_FinalGetUserFails(t *testing.T) {
	store := newErrUserStore()
	store.users["a"] = domain.User{Name: "a"}
	// The handler calls GetUser twice: once for the membership check, once
	// for the final read after reconciling grants. Let the first succeed and
	// only the second fail.
	store.GetErr = errors.New("boom")
	store.GetErrAfter = 1
	srv := &Server{userStore: store, grantStore: newErrGrantStore()}
	w := httptest.NewRecorder()
	req := withURLParam(httptest.NewRequest(http.MethodPut, "/api/users/a/grants", setGrantsBody()), "user", "a")
	srv.handleSetUserGrants(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (final GetUser fails after successful reconciliation)", w.Code)
	}
}

func TestHandleDeleteUser_NilStore(t *testing.T) {
	srv := &Server{}
	w := httptest.NewRecorder()
	srv.handleDeleteUser(w, httptest.NewRequest(http.MethodDelete, "/api/users/a", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", w.Code)
	}
}

func TestHandleDeleteUser_StoreErrorOtherThanNotFound(t *testing.T) {
	store := newErrUserStore()
	store.DeleteErr = errors.New("boom")
	srv := &Server{userStore: store}
	w := httptest.NewRecorder()
	req := withURLParam(httptest.NewRequest(http.MethodDelete, "/api/users/a", nil), "user", "a")
	srv.handleDeleteUser(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
}

// fakeHasher is a minimal domain.PasswordHasher with an injectable Hash error.
type fakeHasher struct {
	hashErr error
}

func (h *fakeHasher) Hash(password string) (string, error) {
	if h.hashErr != nil {
		return "", h.hashErr
	}
	return "hashed:" + password, nil
}

func (h *fakeHasher) Verify(hash, password string) (bool, error) {
	return hash == "hashed:"+password, nil
}

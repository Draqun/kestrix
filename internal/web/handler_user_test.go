package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"kestrix/internal/domain"
)

func adminRequest(t *testing.T, srv *Server, method, path string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Authorization", authToken(t, srv, domain.RoleAdmin))
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)
	return w
}

func TestHandleCreateUser_Happy(t *testing.T) {
	srv := NewServer(Config{})
	body, _ := json.Marshal(createUserRequest{Name: "carol", Password: "s3cret", Role: domain.RoleOperator})
	w := adminRequest(t, srv, http.MethodPost, "/api/users", body)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body=%s)", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "s3cret") || strings.Contains(w.Body.String(), "argon2id") {
		t.Fatalf("response leaked password material: %s", w.Body.String())
	}
	var view userView
	if err := json.Unmarshal(w.Body.Bytes(), &view); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if view.Name != "carol" || view.Role != domain.RoleOperator {
		t.Fatalf("view = %+v, want carol/operator", view)
	}
}

func TestHandleCreateUser_DuplicateName(t *testing.T) {
	srv := NewServer(Config{})
	body, _ := json.Marshal(createUserRequest{Name: "dana", Password: "s3cret", Role: domain.RoleViewer})
	if w := adminRequest(t, srv, http.MethodPost, "/api/users", body); w.Code != http.StatusCreated {
		t.Fatalf("first create status = %d, want 201", w.Code)
	}
	w := adminRequest(t, srv, http.MethodPost, "/api/users", body)
	if w.Code != http.StatusConflict {
		t.Fatalf("second create status = %d, want 409", w.Code)
	}
}

func TestHandleCreateUser_InvalidRole(t *testing.T) {
	srv := NewServer(Config{})
	body, _ := json.Marshal(createUserRequest{Name: "erin", Password: "s3cret", Role: domain.Role("superadmin")})
	w := adminRequest(t, srv, http.MethodPost, "/api/users", body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for an invalid role", w.Code)
	}
}

func TestHandleListUsers_NeverLeaksPasswordHash(t *testing.T) {
	srv := NewServer(Config{})
	body, _ := json.Marshal(createUserRequest{Name: "frank", Password: "correct-horse", Role: domain.RoleAdmin})
	if w := adminRequest(t, srv, http.MethodPost, "/api/users", body); w.Code != http.StatusCreated {
		t.Fatalf("create status = %d", w.Code)
	}
	w := adminRequest(t, srv, http.MethodGet, "/api/users", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("list status = %d", w.Code)
	}
	if strings.Contains(w.Body.String(), "correct-horse") || strings.Contains(w.Body.String(), "argon2id") {
		t.Fatalf("list response leaked password material: %s", w.Body.String())
	}
}

func TestHandleUpdateUserRole_Happy(t *testing.T) {
	srv := NewServer(Config{})
	body, _ := json.Marshal(createUserRequest{Name: "gina", Password: "s3cret", Role: domain.RoleViewer})
	if w := adminRequest(t, srv, http.MethodPost, "/api/users", body); w.Code != http.StatusCreated {
		t.Fatalf("create status = %d", w.Code)
	}
	roleBody, _ := json.Marshal(updateUserRoleRequest{Role: domain.RoleAdmin})
	w := adminRequest(t, srv, http.MethodPut, "/api/users/gina/role", roleBody)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	var view userView
	if err := json.Unmarshal(w.Body.Bytes(), &view); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if view.Role != domain.RoleAdmin {
		t.Fatalf("role = %s, want admin", view.Role)
	}
}

func TestHandleUpdateUserRole_NotFound(t *testing.T) {
	srv := NewServer(Config{})
	roleBody, _ := json.Marshal(updateUserRoleRequest{Role: domain.RoleAdmin})
	w := adminRequest(t, srv, http.MethodPut, "/api/users/ghost/role", roleBody)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

func TestHandleDeleteUser_Happy(t *testing.T) {
	srv := NewServer(Config{})
	body, _ := json.Marshal(createUserRequest{Name: "hank", Password: "s3cret", Role: domain.RoleViewer})
	if w := adminRequest(t, srv, http.MethodPost, "/api/users", body); w.Code != http.StatusCreated {
		t.Fatalf("create status = %d", w.Code)
	}
	w := adminRequest(t, srv, http.MethodDelete, "/api/users/hank", nil)
	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", w.Code)
	}
	w2 := adminRequest(t, srv, http.MethodDelete, "/api/users/hank", nil)
	if w2.Code != http.StatusNotFound {
		t.Fatalf("second delete status = %d, want 404", w2.Code)
	}
}

func TestHandleSetUserGrants_ReplacesFullSet(t *testing.T) {
	srv := NewServer(Config{})
	createBody, _ := json.Marshal(createUserRequest{Name: "iris", Password: "s3cret", Role: domain.RoleViewer})
	if w := adminRequest(t, srv, http.MethodPost, "/api/users", createBody); w.Code != http.StatusCreated {
		t.Fatalf("create status = %d", w.Code)
	}

	grantsBody, _ := json.Marshal(setGrantsRequest{Grants: []struct {
		ProjectName string      `json:"project_name"`
		Role        domain.Role `json:"role"`
	}{{ProjectName: "alpha", Role: domain.RoleOperator}, {ProjectName: "beta", Role: domain.RoleAdmin}}})
	w := adminRequest(t, srv, http.MethodPut, "/api/users/iris/grants", grantsBody)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	var view userView
	if err := json.Unmarshal(w.Body.Bytes(), &view); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(view.Grants) != 2 {
		t.Fatalf("grants = %+v, want 2 entries", view.Grants)
	}

	// Replacing with a narrower set removes what is no longer listed.
	secondBody, _ := json.Marshal(setGrantsRequest{Grants: []struct {
		ProjectName string      `json:"project_name"`
		Role        domain.Role `json:"role"`
	}{{ProjectName: "alpha", Role: domain.RoleViewer}}})
	w2 := adminRequest(t, srv, http.MethodPut, "/api/users/iris/grants", secondBody)
	if w2.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w2.Code)
	}
	var view2 userView
	if err := json.Unmarshal(w2.Body.Bytes(), &view2); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(view2.Grants) != 1 || view2.Grants[0].ProjectName != "alpha" || view2.Grants[0].Role != domain.RoleViewer {
		t.Fatalf("grants = %+v, want only alpha/viewer", view2.Grants)
	}
}

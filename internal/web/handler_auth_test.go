package web

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
	w1 := httptest.NewRecorder()
	srv.router.ServeHTTP(w1, req1)

	req2 := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(unknownAccount))
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
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
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
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
}

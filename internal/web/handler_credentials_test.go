package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"kestrix/internal/credentials"
	"kestrix/internal/domain"
	"kestrix/internal/mocks"
)

type fakeCredentialStore struct {
	items                                   map[string]domain.CredentialMetadata
	secret                                  string
	err                                     error
	createErr, rotateErr, getErr, revokeErr error
	// resolveResult and resolveErr control ResolveCredential, so tests can
	// simulate a runtime-secrets resolution (including a rotated value
	// returned by a later call) without a real credential store.
	resolveResult domain.Credential
	resolveErr    error
}

func (f *fakeCredentialStore) CreateCredential(input domain.CredentialInput) (domain.CredentialMetadata, error) {
	if f.createErr != nil {
		return domain.CredentialMetadata{}, f.createErr
	}
	normalized, err := credentials.Normalize("cred_test", input)
	if err != nil {
		return domain.CredentialMetadata{}, err
	}
	m := normalized.Metadata
	m.CreatedAt, m.UpdatedAt = time.Now(), time.Now()
	f.items[m.Ref] = m
	f.secret = input.Token + input.Password + input.Value
	return m, nil
}
func (f *fakeCredentialStore) RotateCredential(ref string, input domain.CredentialInput) (domain.CredentialMetadata, error) {
	if f.rotateErr != nil {
		return domain.CredentialMetadata{}, f.rotateErr
	}
	return f.items[ref], nil
}
func (f *fakeCredentialStore) GetCredentialMetadata(ref string) (domain.CredentialMetadata, error) {
	if f.getErr != nil {
		return domain.CredentialMetadata{}, f.getErr
	}
	return f.items[ref], nil
}
func (f *fakeCredentialStore) ListCredentialMetadata() ([]domain.CredentialMetadata, error) {
	if f.err != nil {
		return nil, f.err
	}
	result := make([]domain.CredentialMetadata, 0, len(f.items))
	for _, m := range f.items {
		result = append(result, m)
	}
	return result, nil
}
func (f *fakeCredentialStore) RevokeCredential(ref string) error {
	if f.revokeErr != nil {
		return f.revokeErr
	}
	m := f.items[ref]
	m.Revoked = true
	f.items[ref] = m
	return nil
}
func (f *fakeCredentialStore) ResolveCredential(context.Context, string) (domain.Credential, error) {
	if f.resolveErr != nil {
		return domain.Credential{}, f.resolveErr
	}
	return f.resolveResult, nil
}

func credentialRequest(method, path, body string) *http.Request {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	ctx := chi.NewRouteContext()
	ctx.URLParams.Add("ref", "cred_test")
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, ctx))
}

func TestCredentialHandlersLifecycleAndErrors(t *testing.T) {
	store := &fakeCredentialStore{items: map[string]domain.CredentialMetadata{"cred_test": {Ref: "cred_test", Name: "test", Type: "token"}}}
	s := &Server{credentialStore: store}
	w := httptest.NewRecorder()
	s.handleListCredentials(w, credentialRequest("GET", "/api/credentials", ""))
	if w.Code != 200 {
		t.Fatalf("list=%d", w.Code)
	}
	w = httptest.NewRecorder()
	s.handleGetCredential(w, credentialRequest("GET", "/api/credentials/cred_test", ""))
	if w.Code != 200 {
		t.Fatalf("get=%d", w.Code)
	}
	w = httptest.NewRecorder()
	s.handleRotateCredential(w, credentialRequest("PUT", "/api/credentials/cred_test", `{}`))
	if w.Code != 200 {
		t.Fatalf("rotate=%d", w.Code)
	}
	w = httptest.NewRecorder()
	s.handleRevokeCredential(w, credentialRequest("POST", "/api/credentials/cred_test/revoke", ""))
	if w.Code != 204 {
		t.Fatalf("revoke=%d", w.Code)
	}
	noStore := &Server{}
	w = httptest.NewRecorder()
	noStore.handleListCredentials(w, credentialRequest("GET", "/api/credentials", ""))
	if w.Code != 503 {
		t.Fatalf("no store=%d", w.Code)
	}
	for name, call := range map[string]func(*Server, *httptest.ResponseRecorder){
		"create": func(s *Server, w *httptest.ResponseRecorder) {
			s.handleCreateCredential(w, credentialRequest("POST", "/api/credentials", `{}`))
		},
		"get": func(s *Server, w *httptest.ResponseRecorder) {
			s.handleGetCredential(w, credentialRequest("GET", "/api/credentials/cred_test", ""))
		},
		"rotate": func(s *Server, w *httptest.ResponseRecorder) {
			s.handleRotateCredential(w, credentialRequest("PUT", "/api/credentials/cred_test", `{}`))
		},
		"revoke": func(s *Server, w *httptest.ResponseRecorder) {
			s.handleRevokeCredential(w, credentialRequest("POST", "/api/credentials/cred_test/revoke", ""))
		},
	} {
		w := httptest.NewRecorder()
		call(noStore, w)
		if w.Code != 503 {
			t.Fatalf("no store %s=%d", name, w.Code)
		}
	}
	for name, check := range map[string]func(*Server, *httptest.ResponseRecorder){
		"list": func(s *Server, w *httptest.ResponseRecorder) {
			s.handleListCredentials(w, credentialRequest("GET", "/api/credentials", ""))
		},
		"get": func(s *Server, w *httptest.ResponseRecorder) {
			s.handleGetCredential(w, credentialRequest("GET", "/api/credentials/cred_test", ""))
		},
		"revoke": func(s *Server, w *httptest.ResponseRecorder) {
			s.handleRevokeCredential(w, credentialRequest("POST", "/api/credentials/cred_test/revoke", ""))
		},
	} {
		bad := &Server{credentialStore: &fakeCredentialStore{items: map[string]domain.CredentialMetadata{}, err: errors.New(name), getErr: errors.New(name), revokeErr: errors.New(name)}}
		w := httptest.NewRecorder()
		check(bad, w)
		if w.Code < 400 {
			t.Fatalf("%s error status=%d", name, w.Code)
		}
	}
	badCreate := &Server{credentialStore: &fakeCredentialStore{items: map[string]domain.CredentialMetadata{}, createErr: errors.New("create")}}
	w = httptest.NewRecorder()
	badCreate.handleCreateCredential(w, credentialRequest("POST", "/api/credentials", "not json"))
	if w.Code != 400 {
		t.Fatalf("bad JSON=%d", w.Code)
	}
	w = httptest.NewRecorder()
	badCreate.handleCreateCredential(w, credentialRequest("POST", "/api/credentials", `{}`))
	if w.Code != 400 {
		t.Fatalf("create error=%d", w.Code)
	}
	badRotate := &Server{credentialStore: &fakeCredentialStore{items: map[string]domain.CredentialMetadata{}, rotateErr: errors.New("rotate")}}
	w = httptest.NewRecorder()
	badRotate.handleRotateCredential(w, credentialRequest("PUT", "/api/credentials/cred_test", "not json"))
	if w.Code != 400 {
		t.Fatalf("bad rotate JSON=%d", w.Code)
	}
	w = httptest.NewRecorder()
	badRotate.handleRotateCredential(w, credentialRequest("PUT", "/api/credentials/cred_test", `{}`))
	if w.Code != 400 {
		t.Fatalf("rotate error=%d", w.Code)
	}
}

func TestCredentialCreateResponseIsMetadataOnly(t *testing.T) {
	store := &fakeCredentialStore{items: map[string]domain.CredentialMetadata{}}
	s := &Server{credentialStore: store}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/credentials", strings.NewReader(`{"name":"GitHub","type":"token","token":"top-secret"}`))
	s.handleCreateCredential(w, r)
	if w.Code != 201 || strings.Contains(w.Body.String(), "top-secret") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var metadata domain.CredentialMetadata
	if err := json.Unmarshal(w.Body.Bytes(), &metadata); err != nil || metadata.Ref == "" {
		t.Fatalf("metadata=%+v err=%v", metadata, err)
	}
}

// TestCreateSecretCredential_RoleAndValidation pins runtime-secrets spec.md
// "Non-administrator is refused" and "Empty value is rejected": an operator
// is refused before a `secret` credential is created, an admin can create
// one, and an empty value is rejected with no value echoed in the response.
func TestCreateSecretCredential_RoleAndValidation(t *testing.T) {
	srv := NewServer(Config{})
	srv.credentialStore = &fakeCredentialStore{items: map[string]domain.CredentialMetadata{}}
	admin := authToken(t, srv, domain.RoleAdmin)
	operator := authToken(t, srv, domain.RoleOperator)

	req := httptest.NewRequest("POST", "/api/credentials", strings.NewReader(`{"name":"Deploy","type":"secret","value":"opaque-value"}`))
	req.Header.Set("Authorization", operator)
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("operator status = %d, want 403", w.Code)
	}

	req = httptest.NewRequest("POST", "/api/credentials", strings.NewReader(`{"name":"Deploy","type":"secret","value":"opaque-value"}`))
	req.Header.Set("Authorization", admin)
	w = httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("admin status = %d, body=%s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest("POST", "/api/credentials", strings.NewReader(`{"name":"Deploy","type":"secret","value":""}`))
	req.Header.Set("Authorization", admin)
	w = httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("empty value status = %d, want 400", w.Code)
	}
	if strings.Contains(w.Body.String(), "opaque-value") {
		t.Fatalf("response echoed a value: %s", w.Body.String())
	}
}

type combinedCredentialStore struct {
	*mocks.MockProjectStore
	*mocks.MockRunStore
	*fakeCredentialStore
	provider credentials.MasterKeyProvider
}

func (c *combinedCredentialStore) SetCredentialMasterKeyProvider(provider credentials.MasterKeyProvider) {
	c.provider = provider
}

func TestServerWiresCredentialStoreAndMasterKeyPath(t *testing.T) {
	combined := &combinedCredentialStore{MockProjectStore: &mocks.MockProjectStore{}, MockRunStore: &mocks.MockRunStore{}, fakeCredentialStore: &fakeCredentialStore{items: map[string]domain.CredentialMetadata{}}}
	old := newPGStores
	newPGStores = func(string) (domain.ProjectStore, domain.RunStore, error) { return combined, combined, nil }
	t.Cleanup(func() { newPGStores = old })
	s, err := NewServerE(Config{DatabaseURL: "postgres://test", MasterKeyPath: validMasterKeyPath(t), JWTSigningKeyPath: validSigningKeyPath(t)})
	if err != nil {
		t.Fatal(err)
	}
	if s.credentialResolver() == nil {
		t.Fatal("credential resolver not wired")
	}
	if combined.provider == nil {
		t.Fatal("master-key provider was not wired")
	}
}

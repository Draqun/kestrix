package web

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"kestrix/internal/auth"
	"kestrix/internal/domain"
	"kestrix/internal/mocks"
	"kestrix/internal/runner"
	"kestrix/internal/service"
)

// testSigningKey is a fixed, non-secret key used only to mint and verify
// tokens inside unit tests. It never guards anything real.
func testSigningKey() ([]byte, error) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	return key, nil
}

// authToken mints a bearer token for role, scoped to a synthetic test user,
// for tests that exercise the router (and therefore RequireAuth/RequireRole)
// directly. Handler-level tests that call a handleXxx method with a
// hand-built request do not go through the middleware and do not need one.
//
// Since RequireAuth resolves its role from the user store rather than the
// token (design.md D1), the synthetic account is created there first - or
// reused, at role, if a prior call already created it - so the token it
// mints actually verifies.
func authToken(t interface{ Fatalf(string, ...interface{}) }, srv *Server, role domain.Role) string {
	name := "test-" + string(role)
	user := ensureTestUser(t, srv, name, role)
	token, _, err := srv.tokenIssuer.Issue(user)
	if err != nil {
		t.Fatalf("mint test token: %v", err)
	}
	return "Bearer " + token
}

// ensureTestUser returns the account named name from srv.userStore, creating
// it at role if absent, or updating it to role if it already exists at a
// different one. It exists so router-level tests can mint a token whose
// account genuinely resolves through RequireAuth's user-store lookup.
func ensureTestUser(t interface{ Fatalf(string, ...interface{}) }, srv *Server, name string, role domain.Role) domain.User {
	if srv.userStore == nil {
		t.Fatalf("ensureTestUser: server has no userStore configured")
	}
	if existing, err := srv.userStore.GetUser(name); err == nil {
		if existing.Role != role {
			if err := srv.userStore.UpdateUserRole(name, role); err != nil {
				t.Fatalf("update test user role: %v", err)
			}
			existing, err = srv.userStore.GetUser(name)
			if err != nil {
				t.Fatalf("reload test user: %v", err)
			}
		}
		return existing
	}
	created, err := srv.userStore.CreateUser(name, role, "unused-test-hash")
	if err != nil {
		t.Fatalf("create test user: %v", err)
	}
	return created
}

// authed attaches an admin bearer token to r and returns it. It exists so
// pre-existing router-level tests, written before RequireAuth/RequireRole
// existed, keep exercising full request/response behavior without each one
// asserting on an authorization boundary - those boundaries are covered
// explicitly by middleware_test.go and the route-role table test. Many of
// those tests build a *Server literal directly rather than through
// NewServer, predating tokenIssuer and userStore; authed wires in the fixed
// test signing key and an in-memory user store lazily rather than requiring
// every such literal to set them.
func authed(t interface{ Fatalf(string, ...interface{}) }, srv *Server, r *http.Request) *http.Request {
	if srv.tokenIssuer == nil {
		srv.tokenIssuer = auth.JWTIssuer{Key: testSigningKey}
	}
	if srv.userStore == nil {
		srv.userStore = mocks.NewMockUserStore()
	}
	r.Header.Set("Authorization", authToken(t, srv, domain.RoleAdmin))
	return r
}

type testSourceSessions struct {
	mu         sync.Mutex
	root       string
	requests   []domain.SourceRequest
	cleaned    int
	pinned     []int
	released   []int
	err        error
	failAt     int
	nilCleanup bool
}

func (m *testSourceSessions) Open(_ context.Context, request domain.SourceRequest) (domain.SourceSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.requests = append(m.requests, request)
	if m.err != nil && (m.failAt == 0 || len(m.requests) == m.failAt) {
		return domain.SourceSession{}, m.err
	}
	root := m.root
	if strings.HasPrefix(request.URL, "file://") {
		root = strings.TrimPrefix(request.URL, "file://")
	}
	owned := false
	if root == "" {
		var err error
		root, err = os.MkdirTemp("", "kestrix-web-source-test-")
		if err != nil {
			return domain.SourceSession{}, err
		}
		owned = true
		if err := os.MkdirAll(filepath.Join(root, "ci"), 0o700); err != nil {
			return domain.SourceSession{}, err
		}
		definition := []byte("project \"" + request.RepoName + "\" {}\n")
		if err := os.WriteFile(filepath.Join(root, "ci", "pipeline.tf"), definition, 0o600); err != nil {
			return domain.SourceSession{}, err
		}
		if err := os.WriteFile(filepath.Join(root, "pipeline.tf"), definition, 0o600); err != nil {
			return domain.SourceSession{}, err
		}
	}
	var once sync.Once
	cleanup := func() {
		once.Do(func() {
			m.mu.Lock()
			defer m.mu.Unlock()
			m.cleaned++
			if owned {
				_ = os.RemoveAll(root)
			}
		})
	}
	if m.nilCleanup {
		cleanup = nil
	}
	return domain.SourceSession{Source: domain.AcquiredSource{
		RepoName: request.RepoName, URL: request.URL, Path: root,
		RequestedRef: request.RequestedRef, ResolvedRevision: "0123456789abcdef",
		CredentialRef: request.CredentialRef, Primary: request.Primary,
	}, Cleanup: cleanup}, nil
}

func (m *testSourceSessions) Pin(_ context.Context, runID int, _ []domain.AcquiredSource) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pinned = append(m.pinned, runID)
	return nil
}

func (m *testSourceSessions) Release(_ context.Context, runID int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.released = append(m.released, runID)
	return nil
}

func (m *testSourceSessions) Maintain(context.Context) error { return nil }

// NewServer provides dependency-injected wiring for unit tests. Production
// startup must use NewServerE, which requires PostgreSQL.
func NewServer(cfg Config) *Server {
	projectStore := &mocks.MockProjectStore{}
	projectStore.Projects = make(map[string]*domain.ProjectConfig)
	if cfg.StorePath != "" {
		if data, err := os.ReadFile(cfg.StorePath); err == nil {
			var projects map[string]*domain.ProjectConfig
			if json.Unmarshal(data, &projects) == nil {
				projectStore.Projects = projects
			}
		}
	}
	runStore := &mocks.MockRunStore{CreateRunID: 1}
	webhookLog := &mocks.MockWebhookLogSink{}
	pipelineRunner := &runner.PipelineExecutor{Mode: "local"}
	srv := NewServerWithDeps(
		cfg,
		projectStore,
		runStore,
		service.NewProjectService(projectStore),
		pipelineRunner,
		pipelineRunner,
		service.NewRunService(runStore, pipelineRunner, pipelineRunner),
		service.NewWebhookService(webhookLog),
	)
	srv.webhookLogStore = webhookLog
	srv.sourceSessions = &testSourceSessions{}
	srv.tokenIssuer = auth.JWTIssuer{Key: testSigningKey}
	srv.userStore = mocks.NewMockUserStore()
	srv.grantStore = mocks.NewMockGrantStore()
	srv.passwordHasher = auth.NewArgon2Hasher()
	srv.authService = service.NewAuthService(srv.userStore, srv.passwordHasher, srv.tokenIssuer)
	return srv
}

// testWebhookLog returns the in-memory webhook history behind a test server,
// so tests can assert on recorded events without reaching for the HTTP API.
func testWebhookLog(t interface{ Fatalf(string, ...interface{}) }, srv *Server) *mocks.MockWebhookLogSink {
	log, ok := srv.webhookLogStore.(*mocks.MockWebhookLogSink)
	if !ok {
		t.Fatalf("webhook log store is %T, want *mocks.MockWebhookLogSink", srv.webhookLogStore)
	}
	return log
}

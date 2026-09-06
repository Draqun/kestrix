package web

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"kestrix/internal/domain"
	"kestrix/internal/mocks"
	"kestrix/internal/runner"
	"kestrix/internal/service"
)

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
	pipelineRunner := &runner.PipelineExecutor{Mode: "local", GenerateYAML: GenerateGitLabCI}
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

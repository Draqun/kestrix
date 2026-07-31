package web

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"kestrix/internal/domain"
	"kestrix/internal/mocks"
	"kestrix/internal/service"
)

func TestOpenProjectSourceUsesRemoteSessionContract(t *testing.T) {
	root := t.TempDir()
	manager := &testSourceSessions{root: root}
	s := &Server{sourceSessions: manager}
	cfg := &domain.ProjectConfig{
		Name: "remote", RepoURL: "https://git.example.test/team/repo.git",
		RepoRef: "main", RepoCredentialRef: "gitlab-build",
	}
	session, err := s.openProjectSource(context.Background(), cfg, "deadbeef", "definition-read")
	if err != nil {
		t.Fatal(err)
	}
	if session.Root != root || session.Source.Path != root || !session.Source.Primary {
		t.Fatalf("session = %+v", session)
	}
	if len(manager.requests) != 1 {
		t.Fatalf("requests = %+v", manager.requests)
	}
	request := manager.requests[0]
	if request.Revision != "deadbeef" || request.RequestedRef != "main" || request.CredentialRef != "gitlab-build" || request.Purpose != "definition-read" {
		t.Fatalf("request = %+v", request)
	}
	session.Cleanup()
	session.Cleanup()
	if manager.cleaned != 1 {
		t.Fatalf("cleanup count = %d", manager.cleaned)
	}
}

func TestLoadProjectPipelineCleansSessionOnParseFailure(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "ci"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "ci", "pipeline.tf"), []byte("invalid {"), 0o600); err != nil {
		t.Fatal(err)
	}
	manager := &testSourceSessions{root: root}
	s := &Server{sourceSessions: manager}
	_, _, err := s.loadProjectPipeline(context.Background(), &domain.ProjectConfig{
		Name: "broken", RepoURL: "https://git.example.test/broken.git", RepoRef: "main", PipelineDir: "ci",
	}, "", "validation")
	if err == nil {
		t.Fatal("expected parse failure")
	}
	if manager.cleaned != 1 {
		t.Fatalf("cleanup count = %d", manager.cleaned)
	}
}

func TestOpenProjectSourceRejectsMissingContractAndManagerFailure(t *testing.T) {
	s := &Server{}
	if _, err := s.openProjectSource(context.Background(), nil, "", "test"); err == nil {
		t.Fatal("expected nil project error")
	}
	if _, err := s.openProjectSource(context.Background(), &domain.ProjectConfig{Name: "missing"}, "", "test"); err == nil {
		t.Fatal("expected missing remote source error")
	}
	if _, err := s.openProjectSource(context.Background(), &domain.ProjectConfig{Name: "remote", RepoURL: "https://git.test/repo.git", RepoRef: "main"}, "", "test"); err == nil || !strings.Contains(err.Error(), "manager") {
		t.Fatalf("missing manager error = %v", err)
	}
	s.sourceSessions = &testSourceSessions{err: errors.New("refresh denied")}
	if _, err := s.openProjectSource(context.Background(), &domain.ProjectConfig{Name: "remote", RepoURL: "https://git.test/repo.git", RepoRef: "main"}, "", "test"); err == nil || !strings.Contains(err.Error(), "refresh denied") {
		t.Fatalf("err = %v", err)
	}
}

func TestOpenProjectSourceSuppliesNoopCleanup(t *testing.T) {
	s := &Server{sourceSessions: &testSourceSessions{root: t.TempDir(), nilCleanup: true}}
	session, err := s.openProjectSource(context.Background(), &domain.ProjectConfig{
		Name: "remote", RepoURL: "https://git.test/repo.git", RepoRef: "main",
	}, "", "test")
	if err != nil {
		t.Fatal(err)
	}
	session.Cleanup()
}

func TestOpenRecordedRunSourcesErrorAndCleanupBranches(t *testing.T) {
	s := &Server{}
	if _, _, _, err := s.openRecordedRunSources(context.Background(), nil); err == nil {
		t.Fatal("expected missing snapshot error")
	}
	run := &domain.Run{PipelineDef: &domain.Project{Name: "demo"}}
	if _, _, _, err := s.openRecordedRunSources(context.Background(), run); err == nil {
		t.Fatal("expected missing source manager error")
	}

	manager := &testSourceSessions{root: t.TempDir(), err: errors.New("second source failed"), failAt: 2}
	s.sourceSessions = manager
	run.Sources = []domain.AcquiredSource{
		{RepoName: "empty"},
		{RepoName: "demo", URL: "https://git.test/demo.git", RequestedRef: "main", ResolvedRevision: "one", Primary: true},
		{RepoName: "docs", URL: "https://git.test/docs.git", RequestedRef: "main", ResolvedRevision: "two"},
	}
	if _, _, _, err := s.openRecordedRunSources(context.Background(), run); err == nil || !strings.Contains(err.Error(), "second source") {
		t.Fatalf("acquisition error = %v", err)
	}
	if manager.cleaned != 1 {
		t.Fatalf("partial cleanup count = %d", manager.cleaned)
	}

	manager = &testSourceSessions{root: t.TempDir()}
	s.sourceSessions = manager
	run.Sources = []domain.AcquiredSource{{RepoName: "docs", URL: "https://git.test/docs.git", RequestedRef: "main", ResolvedRevision: "two"}}
	if _, _, _, err := s.openRecordedRunSources(context.Background(), run); err == nil || !strings.Contains(err.Error(), "primary") {
		t.Fatalf("missing primary error = %v", err)
	}
	if manager.cleaned != 1 {
		t.Fatalf("missing-primary cleanup count = %d", manager.cleaned)
	}
}

func TestOpenRecordedRunSourcesReturnsPrimaryAndAdditionalPaths(t *testing.T) {
	manager := &testSourceSessions{root: t.TempDir(), nilCleanup: true}
	s := &Server{sourceSessions: manager}
	run := &domain.Run{
		PipelineDef: &domain.Project{Name: "demo"},
		Sources: []domain.AcquiredSource{
			{RepoName: "demo", URL: "https://git.test/demo.git", RequestedRef: "main", ResolvedRevision: "one", Primary: true},
			{RepoName: "docs", URL: "https://git.test/docs.git", RequestedRef: "main", ResolvedRevision: "two"},
		},
	}
	root, paths, cleanup, err := s.openRecordedRunSources(context.Background(), run)
	if err != nil {
		t.Fatal(err)
	}
	if root == "" || paths["docs"] == "" {
		t.Fatalf("root=%q paths=%v", root, paths)
	}
	cleanup()
	cleanup()
}

func TestRunHandlersRejectUnmaterializableProject(t *testing.T) {
	s := systemHandlerServer(t)
	if err := s.projectStore.Upsert(&domain.ProjectConfig{Name: "broken"}); err != nil {
		t.Fatal(err)
	}
	ctx := chi.NewRouteContext()
	ctx.URLParams.Add("name", "broken")
	ctx.URLParams.Add("task", "build")
	request := func(method, path, body string) *http.Request {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, ctx))
	}
	w := httptest.NewRecorder()
	s.handleRunProject(w, request("POST", "/api/projects/broken/run", `{}`))
	if w.Code != 400 {
		t.Fatalf("run project=%d", w.Code)
	}
	w = httptest.NewRecorder()
	s.handleRunTask(w, request("POST", "/api/projects/broken/tasks/build/run", ""))
	if w.Code != 400 {
		t.Fatalf("run task=%d", w.Code)
	}
}

func TestParseProjectPipelineRejectsUnmaterializableProject(t *testing.T) {
	s := systemHandlerServer(t)
	if _, err := s.parseProjectPipeline(&domain.ProjectConfig{Name: "broken"}); err == nil {
		t.Fatal("expected materialization error")
	}
}

func TestParseProjectPipelineSuccessCleansSource(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "pipeline.tf"), []byte(`project "demo" {}`), 0o600); err != nil {
		t.Fatal(err)
	}
	manager := &testSourceSessions{root: root}
	s := &Server{sourceSessions: manager}
	project, err := s.parseProjectPipeline(&domain.ProjectConfig{Name: "demo", RepoURL: "https://git.test/demo.git", RepoRef: "main", PipelineDir: "."})
	if err != nil || project.Name != "demo" {
		t.Fatalf("project=%+v error=%v", project, err)
	}
	if manager.cleaned != 1 {
		t.Fatalf("cleanup count = %d", manager.cleaned)
	}
}

type failingProjectReader struct{}

func (failingProjectReader) Read([]byte) (int, error) { return 0, errors.New("read failed") }

func TestDecodeProjectConfigReadFailure(t *testing.T) {
	if err := decodeProjectConfig(failingProjectReader{}, &domain.ProjectConfig{}); err == nil || !strings.Contains(err.Error(), "read failed") {
		t.Fatalf("decode error = %v", err)
	}
}

func TestGetProjectPipelineReportsSourceAcquisitionFailure(t *testing.T) {
	store := &mocks.MockProjectStore{GetResult: &domain.ProjectConfig{Name: "demo", RepoURL: "https://git.test/demo.git", RepoRef: "main"}}
	s := &Server{projectStore: store, sourceSessions: &testSourceSessions{err: errors.New("source denied")}}
	w := httptest.NewRecorder()
	s.handleGetProjectPipeline(w, runCtx(httptest.NewRequest("GET", "/", nil), "name", "demo"))
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "source denied") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestWebhookLoadsPipelineAtImmutableEventRevision(t *testing.T) {
	root := t.TempDir()
	pipeline := `project "demo" {
  ci_repo "app" { path = "." }
  ci_task "build" {
    trigger = "repo.app.changed"
    execute {
      repo = "app"
      script = ":"
    }
  }
}`
	if err := os.WriteFile(filepath.Join(root, "pipeline.tf"), []byte(pipeline), 0o600); err != nil {
		t.Fatal(err)
	}
	projectStore := &mocks.MockProjectStore{FindByGitLabIDResult: &domain.ProjectConfig{
		Name: "demo", RepoURL: "https://git.example.test/demo.git", RepoRef: "main", PipelineDir: ".",
	}}
	manager := &testSourceSessions{root: root}
	s := &Server{projectStore: projectStore, projectService: service.NewProjectService(projectStore), sourceSessions: manager}
	payload := `{"object_kind":"push","ref":"refs/heads/main","checkout_sha":"event-immutable-sha","project":{"id":42,"path_with_namespace":"demo"},"commits":[{"id":"event-immutable-sha","modified":["main.go"]}]}`
	w := httptest.NewRecorder()
	s.handleWebhook(w, httptest.NewRequest("POST", "/webhook", strings.NewReader(payload)))
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if len(manager.requests) != 1 || manager.requests[0].Revision != "event-immutable-sha" || manager.requests[0].Purpose != "webhook" {
		t.Fatalf("requests = %+v", manager.requests)
	}
	if manager.cleaned != 1 {
		t.Fatalf("cleanup count = %d", manager.cleaned)
	}

	manager.err = errors.New("revision unavailable")
	w = httptest.NewRecorder()
	s.handleWebhook(w, httptest.NewRequest("POST", "/webhook", strings.NewReader(payload)))
	if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), "revision unavailable") {
		t.Fatalf("failed acquisition status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestWebhookRejectsRegisteredPushWithoutImmutableRevision(t *testing.T) {
	projectStore := &mocks.MockProjectStore{FindByGitLabIDResult: &domain.ProjectConfig{
		Name: "demo", RepoURL: "https://git.test/demo.git", RepoRef: "main",
	}}
	s := &Server{projectStore: projectStore, projectService: service.NewProjectService(projectStore)}
	w := httptest.NewRecorder()
	s.handleWebhook(w, httptest.NewRequest("POST", "/webhook", strings.NewReader(`{"object_kind":"push","ref":"refs/heads/main","project":{"id":42}}`)))
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "immutable checkout SHA") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

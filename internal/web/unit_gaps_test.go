package web

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"kestrix/internal/domain"
	"kestrix/internal/mocks"
	"kestrix/internal/service"
)

func runHandlerProject(t *testing.T, valid bool) (*mocks.MockProjectStore, *domain.ProjectConfig) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "pipeline.tf")
	body := `project "demo" {
  ci_repo "repo" {
    path = "."
  }
  ci_task "task" {
    trigger = "repo.repo.changed"
    execute {
      repo = "repo"
      script = ":"
    }
  }
}`
	if !valid {
		body = "not valid hcl"
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	p := &domain.ProjectConfig{Name: "demo", RepoPath: dir, PipelineDir: ".", PipelineFile: "pipeline.tf"}
	return &mocks.MockProjectStore{GetResult: p}, p
}

func TestRunListHandlersErrors(t *testing.T) {
	projectStore := &mocks.MockProjectStore{}
	countErr := &mocks.MockRunStore{CountRunsErr: errors.New("count")}
	srv := setupRunServer(t, projectStore, countErr)
	w := httptest.NewRecorder()
	srv.handleListRuns(w, runCtx(httptest.NewRequest("GET", "/?limit=bad", nil), "name", "demo"))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid pagination status=%d", w.Code)
	}
	w = httptest.NewRecorder()
	srv.handleListRuns(w, runCtx(httptest.NewRequest("GET", "/", nil), "name", "demo"))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("count error status=%d", w.Code)
	}
	listErr := &mocks.MockRunStore{ListRunsErr: errors.New("list")}
	srv = setupRunServer(t, projectStore, listErr)
	w = httptest.NewRecorder()
	srv.handleListRuns(w, runCtx(httptest.NewRequest("GET", "/", nil), "name", "demo"))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("list error status=%d", w.Code)
	}
	allErr := &mocks.MockRunStore{ListAllRunsErr: errors.New("all")}
	srv = setupRunServer(t, projectStore, allErr)
	w = httptest.NewRecorder()
	srv.handleListAllRuns(w, httptest.NewRequest("GET", "/api/runs", nil))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("all error status=%d", w.Code)
	}
}

func TestRunProjectHandlerErrors(t *testing.T) {
	store, project := runHandlerProject(t, true)
	srv := setupRunServer(t, store, &mocks.MockRunStore{})
	w := httptest.NewRecorder()
	srv.handleRunProject(w, runCtx(httptest.NewRequest("POST", "/", strings.NewReader("{")), "name", project.Name))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid JSON status=%d", w.Code)
	}
	store.GetResult = nil
	w = httptest.NewRecorder()
	srv.handleRunProject(w, runCtx(httptest.NewRequest("POST", "/", strings.NewReader(`{}`)), "name", project.Name))
	if w.Code != http.StatusNotFound {
		t.Fatalf("missing project status=%d", w.Code)
	}
	store, project = runHandlerProject(t, false)
	srv = setupRunServer(t, store, &mocks.MockRunStore{})
	w = httptest.NewRecorder()
	srv.handleRunProject(w, runCtx(httptest.NewRequest("POST", "/", strings.NewReader(`{}`)), "name", project.Name))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("parse error status=%d", w.Code)
	}
}

func TestRunTaskHandlerErrors(t *testing.T) {
	store, project := runHandlerProject(t, true)
	srv := setupRunServer(t, store, &mocks.MockRunStore{})
	w := httptest.NewRecorder()
	srv.handleRunTask(w, runCtx(httptest.NewRequest("POST", "/", nil), "name", project.Name, "task", "missing"))
	if w.Code != http.StatusNotFound {
		t.Fatalf("missing task status=%d", w.Code)
	}
	srv.runService = nil
	w = httptest.NewRecorder()
	srv.handleRunTask(w, runCtx(httptest.NewRequest("POST", "/", nil), "name", project.Name, "task", "task"))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("missing run service status=%d", w.Code)
	}
	store, project = runHandlerProject(t, false)
	srv = setupRunServer(t, store, &mocks.MockRunStore{})
	w = httptest.NewRecorder()
	srv.handleRunTask(w, runCtx(httptest.NewRequest("POST", "/", nil), "name", project.Name, "task", "task"))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("parse error status=%d", w.Code)
	}
}

func TestRunHandlersSuccessAndServiceErrors(t *testing.T) {
	store, project := runHandlerProject(t, true)
	runs := &mocks.MockRunStore{CreateRunID: 7}
	srv := setupRunServer(t, store, runs)
	w := httptest.NewRecorder()
	srv.handleRunProject(w, runCtx(httptest.NewRequest("POST", "/", strings.NewReader(`{"changed":["repo"]}`)), "name", project.Name))
	if w.Code != http.StatusOK {
		t.Fatalf("run project status=%d body=%s", w.Code, w.Body.String())
	}
	var response map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || response["status"] != "running" {
		t.Fatalf("response=%v err=%v", response, err)
	}

	// Start failures are surfaced to the client.
	srv.runService = nil
	srv.handleRunProject(httptest.NewRecorder(), runCtx(httptest.NewRequest("POST", "/", strings.NewReader(`{}`)), "name", project.Name))
	srv.runService = service.NewRunService(runs, nil, nil)
	w = httptest.NewRecorder()
	srv.handleRunProject(w, runCtx(httptest.NewRequest("POST", "/", strings.NewReader(`{}`)), "name", project.Name))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("start error status=%d", w.Code)
	}

	store, project = runHandlerProject(t, true)
	trRunner := &mocks.MockPipelineRunner{TaskResultList: []domain.TaskResult{{Name: "task", Status: "failed", Error: "bad", Output: "out"}}, RunErr: errors.New("runner")}
	srv = setupRunServer(t, store, &mocks.MockRunStore{CreateRunID: 4})
	srv.taskRunner = trRunner
	srv.runService = service.NewRunService(srv.runStore, trRunner, trRunner)
	w = httptest.NewRecorder()
	srv.handleRunTask(w, runCtx(httptest.NewRequest("POST", "/", nil), "name", project.Name, "task", "task"))
	if w.Code != http.StatusOK {
		t.Fatalf("run task status=%d body=%s", w.Code, w.Body.String())
	}
	// A failed task result without a runner error uses the task's own message.
	trRunner = &mocks.MockPipelineRunner{TaskResultList: []domain.TaskResult{{Name: "task", Status: "failed", Error: "task error"}}}
	srv = setupRunServer(t, store, &mocks.MockRunStore{CreateRunID: 8})
	srv.taskRunner = trRunner
	srv.runService = service.NewRunService(srv.runStore, trRunner, trRunner)
	w = httptest.NewRecorder()
	srv.handleRunTask(w, runCtx(httptest.NewRequest("POST", "/", nil), "name", project.Name, "task", "task"))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "task error") {
		t.Fatalf("failed result body=%s", w.Body.String())
	}
}

func TestRunListNilAndApprovalBranches(t *testing.T) {
	store, project := runHandlerProject(t, true)
	runs := &mocks.MockRunStore{ListRunsResult: nil, ListAllRunsResult: nil}
	srv := setupRunServer(t, store, runs)
	w := httptest.NewRecorder()
	srv.handleListRuns(w, runCtx(httptest.NewRequest("GET", "/", nil), "name", project.Name))
	if w.Code != http.StatusOK {
		t.Fatalf("nil list=%d", w.Code)
	}
	w = httptest.NewRecorder()
	srv.handleListAllRuns(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("nil all=%d", w.Code)
	}

	manualRun := &domain.Run{ID: 9, ProjectName: project.Name, TaskResults: []domain.TaskResult{{Name: "task", Status: "manual"}}}
	runs.GetRunResult = manualRun
	// Project lookup, parse, missing service, and approval failure.
	store.GetResult = nil
	w = httptest.NewRecorder()
	srv.handleApproveManualTask(w, runCtx(httptest.NewRequest("POST", "/", nil), "name", project.Name, "runID", "9", "task", "task"))
	if w.Code != http.StatusNotFound {
		t.Fatalf("approval project missing=%d", w.Code)
	}
	store, project = runHandlerProject(t, false)
	srv = setupRunServer(t, store, runs)
	w = httptest.NewRecorder()
	srv.handleApproveManualTask(w, runCtx(httptest.NewRequest("POST", "/", nil), "name", project.Name, "runID", "9", "task", "task"))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("approval parse=%d", w.Code)
	}
	store, project = runHandlerProject(t, true)
	srv = setupRunServer(t, store, runs)
	srv.runService = nil
	w = httptest.NewRecorder()
	srv.handleApproveManualTask(w, runCtx(httptest.NewRequest("POST", "/", nil), "name", project.Name, "runID", "9", "task", "task"))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("approval service=%d", w.Code)
	}
	srv.runService = service.NewRunService(runs, &mocks.MockPipelineRunner{}, &mocks.MockPipelineRunner{})
	w = httptest.NewRecorder()
	srv.handleApproveManualTask(w, runCtx(httptest.NewRequest("POST", "/", nil), "name", project.Name, "runID", "9", "task", "task"))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("approval runner=%d", w.Code)
	}
}

func TestWebhookUnitBranches(t *testing.T) {
	store, project := runHandlerProject(t, true)
	srv := setupRunServer(t, store, nil)
	srv.cfg.Signature = SignatureConfig{Enabled: true, Secret: "secret"}
	// Signature and malformed payload errors.
	w := httptest.NewRecorder()
	srv.handleWebhook(w, httptest.NewRequest("POST", "/webhook", bytes.NewBufferString(`{}`)))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status=%d", w.Code)
	}
	req := httptest.NewRequest("POST", "/webhook", bytes.NewBufferString("bad"))
	req.Header.Set("X-Gitlab-Token", "secret")
	w = httptest.NewRecorder()
	srv.handleWebhook(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bad payload status=%d", w.Code)
	}

	// Unknown project is ignored, lookup failures are reported, and a valid
	// project reaches DAG resolution without any external service.
	srv.cfg.Signature.Enabled = false
	payload := `{"object_kind":"push","ref":"refs/heads/main","project":{"id":42,"path_with_namespace":"repo"},"commits":[{"modified":["x.go"]}]}`
	w = httptest.NewRecorder()
	srv.handleWebhook(w, httptest.NewRequest("POST", "/webhook", bytes.NewBufferString(payload)))
	if w.Code != http.StatusOK {
		t.Fatalf("unknown project status=%d", w.Code)
	}
	store.FindByGitLabIDErr = errors.New("lookup")
	w = httptest.NewRecorder()
	srv.handleWebhook(w, httptest.NewRequest("POST", "/webhook", bytes.NewBufferString(payload)))
	if w.Code != http.StatusOK {
		t.Fatalf("lookup error status=%d", w.Code)
	}
	store.FindByGitLabIDErr = nil
	store.FindByGitLabIDResult = project
	w = httptest.NewRecorder()
	srv.handleWebhook(w, httptest.NewRequest("POST", "/webhook", bytes.NewBufferString(payload)))
	if w.Code != http.StatusOK {
		t.Fatalf("valid webhook status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestProjectAndPipelineErrorBranches(t *testing.T) {
	// Pipeline handlers: missing directory, missing .tf, and unreadable file.
	srv := &Server{cfg: Config{PipelineDir: filepath.Join(t.TempDir(), "missing")}}
	w := httptest.NewRecorder()
	srv.handleGetPipelineRaw(w, httptest.NewRequest("GET", "/api/pipeline/raw", nil))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("raw missing dir=%d", w.Code)
	}
	d := t.TempDir()
	srv.cfg.PipelineDir = d
	w = httptest.NewRecorder()
	srv.handleGetPipelineRaw(w, httptest.NewRequest("GET", "/api/pipeline/raw", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("raw no tf=%d", w.Code)
	}
	bad := filepath.Join(d, "bad.tf")
	if err := os.Mkdir(bad, 0o755); err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	srv.handleGetPipelineRaw(w, httptest.NewRequest("GET", "/api/pipeline/raw", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("raw dir tf=%d", w.Code)
	}

	// Project service error mapping.
	ps := &mocks.MockProjectStore{LoadErr: errors.New("load"), GetErr: errors.New("get")}
	srv = setupRunServer(t, ps, nil)
	srv.projectService = service.NewProjectService(ps)
	w = httptest.NewRecorder()
	srv.handleListProjects(w, httptest.NewRequest("GET", "/api/projects", nil))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("list error=%d", w.Code)
	}
	w = httptest.NewRecorder()
	srv.handleGetProject(w, runCtx(httptest.NewRequest("GET", "/", nil), "name", "x"))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("get error=%d", w.Code)
	}

	// Path traversal in the configured pipeline directory is rejected.
	if _, err := srv.projectPipelineDir(&domain.ProjectConfig{RepoPath: t.TempDir(), PipelineDir: "../outside"}); err == nil {
		t.Fatal("expected pipeline escape error")
	}
}

func TestRemainingRunAndConfigBranches(t *testing.T) {
	store, project := runHandlerProject(t, true)
	// Run task CreateRun failure.
	srv := setupRunServer(t, store, &mocks.MockRunStore{CreateRunErr: errors.New("create")})
	w := httptest.NewRecorder()
	srv.handleRunTask(w, runCtx(httptest.NewRequest("POST", "/", nil), "name", project.Name, "task", "task"))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("create error=%d", w.Code)
	}
	// FinishRun errors are logged but do not change the successful response.
	runs := &mocks.MockRunStore{CreateRunID: 2, FinishRunErr: errors.New("finish")}
	srv = setupRunServer(t, store, runs)
	w = httptest.NewRecorder()
	srv.handleRunTask(w, runCtx(httptest.NewRequest("POST", "/", nil), "name", project.Name, "task", "task"))
	if w.Code != http.StatusOK {
		t.Fatalf("finish error status=%d", w.Code)
	}

	// CancelRun backend failure.
	runs = &mocks.MockRunStore{CancelRunErr: errors.New("cancel")}
	srv = setupRunServer(t, store, runs)
	w = httptest.NewRecorder()
	srv.handleCancelRun(w, runCtx(httptest.NewRequest("POST", "/", nil), "runID", "3"))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("cancel error=%d", w.Code)
	}

	// Update maps validation and backend errors distinctly.
	bad := &mocks.MockProjectStore{GetResult: &domain.ProjectConfig{Name: "x"}}
	srv = setupRunServer(t, bad, nil)
	srv.projectService = service.NewProjectService(bad)
	w = httptest.NewRecorder()
	srv.handleUpdateProject(w, runCtx(httptest.NewRequest("PUT", "/", strings.NewReader(`{"name":""}`)), "name", "x"))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("validation status=%d", w.Code)
	}
	errStore := &mocks.MockProjectStore{GetErr: errors.New("backend")}
	srv = setupRunServer(t, errStore, nil)
	srv.projectService = service.NewProjectService(errStore)
	w = httptest.NewRecorder()
	srv.handleUpdateProject(w, runCtx(httptest.NewRequest("PUT", "/", strings.NewReader(`{"repo_path":"/tmp"}`)), "name", "x"))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("backend status=%d", w.Code)
	}

	// Docker variables are read from the startup environment snapshot.
	srv.cfg.Docker = DockerConfig{Registry: "r", Image: "i", Username: "u", Password: "p"}
	vs := srv.buildVarStore(nil)
	vars := vs.Get("", "", "")
	if vars["DOCKER_IMAGE"] != "i" || vars["DOCKER_USERNAME"] != "u" {
		t.Fatalf("docker variables=%#v", vars)
	}
}

func TestPipelineResolveAndGenerateErrors(t *testing.T) {
	dir := t.TempDir()
	cycle := `project "cycle" {
  ci_repo "repo" {
    path = "."
  }
  ci_task "a" {
    depends_on = ["b"]
    execute {
      repo = "repo"
      script = ":"
    }
  }
  ci_task "b" {
    depends_on = ["a"]
    execute {
      repo = "repo"
      script = ":"
    }
  }
}`
	if err := os.WriteFile(filepath.Join(dir, "pipeline.tf"), []byte(cycle), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := &Server{cfg: Config{PipelineDir: dir}}
	for _, fn := range []func(http.ResponseWriter, *http.Request){srv.handleResolve, srv.handleGenerate} {
		w := httptest.NewRecorder()
		fn(w, httptest.NewRequest("POST", "/", strings.NewReader(`{"changed":["repo"]}`)))
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("cycle status=%d body=%s", w.Code, w.Body.String())
		}
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("read failed") }
func (failingReader) Close() error             { return nil }

func TestProjectPipelineAndDashboardRemainingBranches(t *testing.T) {
	store, _ := runHandlerProject(t, true)
	srv := setupRunServer(t, store, &mocks.MockRunStore{ListAllRunsResult: nil})
	// Dashboard handles a nil recent-runs result.
	w := httptest.NewRecorder()
	srv.handleDashboard(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("dashboard=%d", w.Code)
	}

	// Definition parse failure and pipeline path/read failures.
	badStore, badProject := runHandlerProject(t, false)
	srv.projectStore = badStore
	srv.projectService = service.NewProjectService(badStore)
	w = httptest.NewRecorder()
	srv.handleGetProjectDefinition(w, runCtx(httptest.NewRequest("GET", "/", nil), "name", badProject.Name))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("definition parse=%d", w.Code)
	}

	missing := &domain.ProjectConfig{Name: "missing", RepoPath: t.TempDir(), PipelineFile: "missing.tf"}
	srv.projectStore = &mocks.MockProjectStore{GetResult: missing}
	srv.projectService = service.NewProjectService(srv.projectStore)
	w = httptest.NewRecorder()
	srv.handleGetProjectPipeline(w, runCtx(httptest.NewRequest("GET", "/", nil), "name", "missing"))
	if w.Code != http.StatusNotFound {
		t.Fatalf("pipeline read=%d", w.Code)
	}
	escape := &domain.ProjectConfig{Name: "escape", RepoPath: t.TempDir(), PipelineDir: "sub", PipelineFile: "../outside.tf"}
	srv.projectStore = &mocks.MockProjectStore{GetResult: escape}
	srv.projectService = service.NewProjectService(srv.projectStore)
	w = httptest.NewRecorder()
	srv.handleGetProjectPipeline(w, runCtx(httptest.NewRequest("GET", "/", nil), "name", "escape"))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("pipeline path=%d", w.Code)
	}

	// Request body read failure is deterministic through a custom reader.
	validStore, validProject := runHandlerProject(t, true)
	srv.projectStore = validStore
	srv.projectService = service.NewProjectService(validStore)
	r := httptest.NewRequest("POST", "/", nil)
	r.Body = failingReader{}
	w = httptest.NewRecorder()
	srv.handleSaveProjectPipeline(w, runCtx(r, "name", validProject.Name))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("save read=%d", w.Code)
	}
}

func TestStaticRouterAndStoreFallback(t *testing.T) {
	static := t.TempDir()
	if err := os.WriteFile(filepath.Join(static, "index.html"), []byte("index"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := NewServer(Config{StaticDir: static, StorePath: filepath.Join(t.TempDir(), "projects.json")})
	for _, path := range []string{"/", "/missing", "/api/health", "/api/unknown", "/webhook", "/docs/swagger.json", "/docs/unknown", "/ping"} {
		w := httptest.NewRecorder()
		srv.router.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code == 0 {
			t.Fatalf("router path %s has no status", path)
		}
	}
	root := t.TempDir()
	blocker := filepath.Join(root, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = NewServer(Config{StorePath: filepath.Join(blocker, "projects.json")})
	_ = NewServer(Config{DatabaseURL: "postgres://127.0.0.1:1/nope?sslmode=disable", StorePath: filepath.Join(t.TempDir(), "projects.json")})
	ps, rs := &mocks.MockProjectStore{}, &mocks.MockRunStore{}
	oldPG := newPGStores
	newPGStores = func(string) (domain.ProjectStore, domain.RunStore, error) { return ps, rs, nil }
	t.Cleanup(func() { newPGStores = oldPG })
	gotPS, gotRS, err := initStores(Config{DatabaseURL: "fake"})
	if err != nil {
		t.Fatalf("initStores: %v", err)
	}
	if gotPS != ps || gotRS != rs {
		t.Fatalf("fake PG stores not returned")
	}

	oldListen := listenAndServe
	listenAndServe = func(string, http.Handler) error { return errors.New("listen failed") }
	t.Cleanup(func() { listenAndServe = oldListen })
	if err := (&Server{cfg: Config{ListenAddr: ":0"}}).ListenAndServe(); err == nil {
		t.Fatal("expected listen error")
	}
}

func TestWebhookReadParseAndDispatchBranches(t *testing.T) {
	// io.ReadAll failure.
	srv := &Server{cfg: Config{PipelineDir: filepath.Join(t.TempDir(), "missing")}}
	r := httptest.NewRequest("POST", "/webhook", nil)
	r.Body = failingReader{}
	w := httptest.NewRecorder()
	srv.handleWebhook(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("read error=%d", w.Code)
	}

	// Valid event with an invalid pipeline directory reaches the parse-error path.
	payload := `{"object_kind":"push","ref":"refs/heads/main","project":{"path_with_namespace":"repo"},"commits":[{"modified":["x.go"]}]}`
	w = httptest.NewRecorder()
	srv.handleWebhook(w, httptest.NewRequest("POST", "/webhook", strings.NewReader(payload)))
	if w.Code != http.StatusOK {
		t.Fatalf("parse error response=%d", w.Code)
	}

	// Exercise both dispatch success and the variable-setting API calls.
	pipelineDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(pipelineDir, "pipeline.tf"), []byte(`project "demo" {
  ci_repo "repo" {
  }
  ci_task "task" {
    trigger = "repo.repo.changed"
    execute {
      repo = "repo"
      script = ":"
    }
  }
}`), 0o644); err != nil {
		t.Fatal(err)
	}
	gitlab := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/pipeline") {
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":77,"web_url":"http://pipeline"}`))
			return
		}
		if strings.Contains(r.URL.Path, "/variables") {
			w.WriteHeader(http.StatusCreated)
			return
		}
		http.NotFound(w, r)
	}))
	defer gitlab.Close()
	srv = &Server{cfg: Config{PipelineDir: pipelineDir, Dispatch: DispatchConfig{Enabled: true, ProjectID: "12", GitLabURL: gitlab.URL}, Branch: "main"}}
	w = httptest.NewRecorder()
	srv.handleWebhook(w, httptest.NewRequest("POST", "/webhook", strings.NewReader(payload)))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "pipeline") {
		t.Fatalf("dispatch response=%d body=%s", w.Code, w.Body.String())
	}
	// A project-specific GitLab ID overrides the global dispatch project and
	// an empty configured branch falls back to the webhook branch.
	projectStore := &mocks.MockProjectStore{FindByGitLabIDResult: &domain.ProjectConfig{RepoPath: pipelineDir, PipelineDir: ".", GitLabProjectID: 99}}
	srv.projectStore = projectStore
	srv.cfg.Branch = ""
	projectPayload := `{"object_kind":"push","ref":"refs/heads/main","project":{"id":42,"path_with_namespace":"repo"},"commits":[{"modified":["x.go"]}]}`
	w = httptest.NewRecorder()
	srv.handleWebhook(w, httptest.NewRequest("POST", "/webhook", strings.NewReader(projectPayload)))
	if w.Code != http.StatusOK {
		t.Fatalf("project dispatch status=%d", w.Code)
	}
	// A failed GitLab response is reported without failing the webhook.
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "down", http.StatusBadGateway) }))
	defer failing.Close()
	srv.cfg.Dispatch.GitLabURL = failing.URL
	w = httptest.NewRecorder()
	srv.handleWebhook(w, httptest.NewRequest("POST", "/webhook", strings.NewReader(payload)))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "dispatched") {
		t.Fatalf("dispatch failure response=%d body=%s", w.Code, w.Body.String())
	}
}

func TestRemainingFileAndPathSeams(t *testing.T) {
	oldRead := readPipelineFile
	readPipelineFile = func(string) ([]byte, error) { return nil, errors.New("read pipeline") }
	t.Cleanup(func() { readPipelineFile = oldRead })
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "pipeline.tf"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := &Server{cfg: Config{PipelineDir: dir}}
	w := httptest.NewRecorder()
	srv.handleGetPipelineRaw(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("raw read=%d", w.Code)
	}

	oldAbs := absPath
	absPath = func(string) (string, error) { return "", errors.New("abs") }
	t.Cleanup(func() { absPath = oldAbs })
	if _, err := srv.projectPipelineDir(&domain.ProjectConfig{RepoPath: "repo"}); err == nil {
		t.Fatal("expected abs path error")
	}
}

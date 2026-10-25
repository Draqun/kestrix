package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"kestrix/internal/dispatch"
	"kestrix/internal/domain"
	"kestrix/internal/mocks"
	"kestrix/internal/runner"
	"kestrix/internal/service"
)

type cleanupPipelineRunner struct {
	*mocks.MockPipelineRunner
}

func (r *cleanupPipelineRunner) Run(ctx context.Context, project domain.Project, changed map[string]bool, opts domain.RunnerOptions) ([]domain.TaskResult, error) {
	if opts.WorkspaceCleanup != nil {
		opts.WorkspaceCleanup()
	}
	return r.MockPipelineRunner.Run(ctx, project, changed, opts)
}

func testWebhookServer(t *testing.T, runStore *mocks.MockRunStore, projects ...*domain.ProjectConfig) (*Server, *mocks.MockProjectStore, *mocks.MockWebhookLogSink, *testSourceSessions) {
	t.Helper()
	srv, store, log, sessions := sourcesServer(t, projects...)
	if runStore == nil {
		runStore = &mocks.MockRunStore{CreateRunID: 101, FinishRunCh: make(chan struct{}, 1)}
	}
	pipelineRunner := &cleanupPipelineRunner{MockPipelineRunner: &mocks.MockPipelineRunner{}}
	srv.runStore = runStore
	srv.pipelineRunner = pipelineRunner
	srv.taskRunner = pipelineRunner
	srv.runService = service.NewRunService(runStore, pipelineRunner, pipelineRunner)
	srv.runService.SetLocalExecutionPolicy(true, "local")
	return srv, store, log, sessions
}

// 3.2.1 Push to repo_ref starts one run with the webhook's changed set and actor.
func TestWebhookStartsRun_PrimaryRepoRefStartsRun(t *testing.T) {
	const singleRepoPipeline = `project "shop" {
	ci_repo "app" {
		path = "app"
	}
	ci_task "build" {
		trigger = "repo.app.changed"
		execute {
			repo = "app"
			script = ":"
		}
	}
}`
	proj := registeredProject(t, "shop", singleRepoPipeline, 0)
	runStore := &mocks.MockRunStore{CreateRunID: 42, FinishRunCh: make(chan struct{}, 1)}
	srv, _, log, sessions := testWebhookServer(t, runStore, proj)

	body := sourcePush(0, []string{proj.RepoURL}, "abc111", "app/main.go")
	rec := postWebhook(t, srv, body)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	resp := decodeBody(t, rec)
	if resp["status"] != "ok" {
		t.Fatalf("status = %v, want ok", resp["status"])
	}
	if resp["run_id"] != float64(42) {
		t.Fatalf("run_id = %v, want 42", resp["run_id"])
	}
	results := resultsOf(t, resp)
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if results[0]["run_id"] != float64(42) {
		t.Fatalf("result[0].run_id = %v, want 42", results[0]["run_id"])
	}

	// Verify run in store
	if len(runStore.CreateRunCalls) != 1 {
		t.Fatalf("CreateRun calls = %d, want 1", len(runStore.CreateRunCalls))
	}
	call := runStore.CreateRunCalls[0]
	if call.ProjectName != "shop" {
		t.Errorf("project name = %q, want shop", call.ProjectName)
	}
	if call.StartedBy != "webhook:shop" {
		t.Errorf("started_by = %q, want webhook:shop", call.StartedBy)
	}
	if len(call.Changed) != 1 || call.Changed[0] != "app" {
		t.Errorf("changed = %v, want [app]", call.Changed)
	}

	// Wait for runner to complete and assert session cleaned up exactly once
	<-runStore.FinishRunCh
	sessions.mu.Lock()
	cleaned := sessions.cleaned
	sessions.mu.Unlock()
	if cleaned != 1 {
		t.Errorf("sessions.cleaned = %d, want 1", cleaned)
	}

	// History recorded
	events := log.List()
	if len(events) != 1 || events[0].Status != domain.WebhookStatusTriggered {
		t.Errorf("events = %+v", events)
	}
}

// 3.2.2 Another branch, a tag, zero triggered tasks, and dispatch-configured case start no run.
func TestWebhookStartsRun_NonStartingCases(t *testing.T) {
	t.Run("push to other branch starts nothing", func(t *testing.T) {
		proj := registeredProject(t, "shop", multiSourcePipeline, 0)
		runStore := &mocks.MockRunStore{CreateRunID: 42}
		srv, _, log, sessions := testWebhookServer(t, runStore, proj)

		// Push to refs/heads/feature-x
		body := `{"object_kind":"push","ref":"refs/heads/feature-x","checkout_sha":"abc111","project":{"id":0,"path_with_namespace":"team/pushed","default_branch":"main","git_http_url":"` + proj.RepoURL + `"},"commits":[{"id":"abc111","modified":["lib/main.go"]}]}`
		rec := postWebhook(t, srv, body)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
		}
		resp := decodeBody(t, rec)
		if resp["status"] != "ok" {
			t.Fatalf("status = %v", resp["status"])
		}
		if _, hasRunID := resp["run_id"]; hasRunID {
			t.Errorf("response unexpectedly carries run_id: %v", resp["run_id"])
		}
		if len(runStore.CreateRunCalls) != 0 {
			t.Errorf("runs created = %d, want 0", len(runStore.CreateRunCalls))
		}
		sessions.mu.Lock()
		cleaned := sessions.cleaned
		sessions.mu.Unlock()
		if cleaned != 1 {
			t.Errorf("sessions.cleaned = %d, want 1", cleaned)
		}
		events := log.List()
		if len(events) != 1 || events[0].Status != domain.WebhookStatusTriggered {
			t.Errorf("events = %+v", events)
		}
	})

	t.Run("tag named like the branch starts nothing", func(t *testing.T) {
		proj := registeredProject(t, "shop", multiSourcePipeline, 0)
		runStore := &mocks.MockRunStore{CreateRunID: 42}
		srv, _, _, sessions := testWebhookServer(t, runStore, proj)

		// Push to refs/tags/main
		body := `{"object_kind":"push","ref":"refs/tags/main","checkout_sha":"abc111","project":{"id":0,"path_with_namespace":"team/pushed","default_branch":"main","git_http_url":"` + proj.RepoURL + `"},"commits":[{"id":"abc111","modified":["lib/main.go"]}]}`
		rec := postWebhook(t, srv, body)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
		}
		resp := decodeBody(t, rec)
		if _, hasRunID := resp["run_id"]; hasRunID {
			t.Errorf("response unexpectedly carries run_id: %v", resp["run_id"])
		}
		if len(runStore.CreateRunCalls) != 0 {
			t.Errorf("runs created = %d, want 0", len(runStore.CreateRunCalls))
		}
		sessions.mu.Lock()
		cleaned := sessions.cleaned
		sessions.mu.Unlock()
		if cleaned != 1 {
			t.Errorf("sessions.cleaned = %d, want 1", cleaned)
		}
	})

	t.Run("zero triggered tasks starts nothing", func(t *testing.T) {
		proj := registeredProject(t, "shop", multiSourcePipeline, 0)
		runStore := &mocks.MockRunStore{CreateRunID: 42}
		srv, _, log, sessions := testWebhookServer(t, runStore, proj)

		// Push touching only docs/README.md
		body := sourcePush(0, []string{proj.RepoURL}, "abc111", "docs/README.md")
		rec := postWebhook(t, srv, body)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
		}
		resp := decodeBody(t, rec)
		if _, hasRunID := resp["run_id"]; hasRunID {
			t.Errorf("response unexpectedly carries run_id: %v", resp["run_id"])
		}
		if len(runStore.CreateRunCalls) != 0 {
			t.Errorf("runs created = %d, want 0", len(runStore.CreateRunCalls))
		}
		sessions.mu.Lock()
		cleaned := sessions.cleaned
		sessions.mu.Unlock()
		if cleaned != 1 {
			t.Errorf("sessions.cleaned = %d, want 1", cleaned)
		}
		events := log.List()
		if len(events) != 1 || events[0].Status != domain.WebhookStatusSkipped {
			t.Errorf("events = %+v", events)
		}
	})

	t.Run("dispatch-configured case starts no run", func(t *testing.T) {
		proj := registeredProject(t, "shop", multiSourcePipeline, 77)
		runStore := &mocks.MockRunStore{CreateRunID: 42}
		srv, _, _, sessions := testWebhookServer(t, runStore, proj)
		srv.cfg.Dispatch.Enabled = true
		srv.cfg.Dispatch.GitLabToken = "token"

		// Mock GitLab dispatch endpoint
		gitlabServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(dispatch.DispatchResponse{PipelineID: 999, WebURL: "https://gitlab.example/pipelines/999"})
		}))
		defer gitlabServer.Close()
		srv.cfg.Dispatch.GitLabURL = gitlabServer.URL

		body := sourcePush(0, []string{proj.RepoURL}, "abc111", "lib/main.go")
		rec := postWebhook(t, srv, body)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
		}
		resp := decodeBody(t, rec)
		if resp["dispatched"] != true {
			t.Errorf("dispatched = %v, want true", resp["dispatched"])
		}
		if _, hasRunID := resp["run_id"]; hasRunID {
			t.Errorf("response unexpectedly carries run_id: %v", resp["run_id"])
		}
		if len(runStore.CreateRunCalls) != 0 {
			t.Errorf("runs created = %d, want 0", len(runStore.CreateRunCalls))
		}
		sessions.mu.Lock()
		cleaned := sessions.cleaned
		sessions.mu.Unlock()
		if cleaned != 1 {
			t.Errorf("sessions.cleaned = %d, want 1", cleaned)
		}
	})
}

// 3.2.3 Secondary push to its ci_repo ref starts a run.
func TestWebhookStartsRun_SecondaryPushStartsRun(t *testing.T) {
	const secondaryPipeline = `project "shop" {
	ci_repo "shared" {
		url = "https://git.example.test/team/shared.git"
		ref = "release"
	}
	ci_task "sync" {
		trigger = "repo.shared.changed"
		execute {
			repo = "shared"
			script = ":"
		}
	}
}`
	proj := registeredProject(t, "shop", secondaryPipeline, 0)
	runStore := &mocks.MockRunStore{CreateRunID: 55, FinishRunCh: make(chan struct{}, 1)}
	srv, _, _, sessions := testWebhookServer(t, runStore, proj)

	// Push to refs/heads/release of the secondary repo
	body := `{"object_kind":"push","ref":"refs/heads/release","checkout_sha":"sec123","project":{"id":0,"path_with_namespace":"team/shared","default_branch":"main","git_http_url":"https://git.example.test/team/shared.git"},"commits":[{"id":"sec123","modified":["pkg/util.go"]}]}`
	rec := postWebhook(t, srv, body)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	resp := decodeBody(t, rec)
	if resp["run_id"] != float64(55) {
		t.Fatalf("run_id = %v, want 55", resp["run_id"])
	}
	if len(runStore.CreateRunCalls) != 1 {
		t.Fatalf("CreateRun calls = %d, want 1", len(runStore.CreateRunCalls))
	}
	call := runStore.CreateRunCalls[0]
	if call.StartedBy != "webhook:shared" {
		t.Errorf("started_by = %q, want webhook:shared", call.StartedBy)
	}
	if len(call.Changed) != 1 || call.Changed[0] != "shared" {
		t.Errorf("changed = %v, want [shared]", call.Changed)
	}

	<-runStore.FinishRunCh
	sessions.mu.Lock()
	cleaned := sessions.cleaned
	sessions.mu.Unlock()
	if cleaned != 1 {
		t.Errorf("sessions.cleaned = %d, want 1", cleaned)
	}
}

// 3.2.4 Fan-out to two projects starts one run each.
func TestWebhookStartsRun_FanOutStartsOneRunEach(t *testing.T) {
	proj1 := registeredProject(t, "billing", multiSourcePipeline, 0)
	proj2 := registeredProject(t, "checkout", multiSourcePipeline, 0)

	runStore := &mocks.MockRunStore{CreateRunID: 10, FinishRunCh: make(chan struct{}, 2)}
	srv, _, _, sessions := testWebhookServer(t, runStore, proj1, proj2)

	// Delivery to shared repoURL
	body := sourcePush(0, []string{sharedRepoURL}, "shared-sha", "lib/main.go")
	rec := postWebhook(t, srv, body)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	resp := decodeBody(t, rec)
	results := resultsOf(t, resp)
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}

	for _, r := range results {
		if r["status"] != "ok" {
			t.Errorf("result status = %v, want ok", r["status"])
		}
		if r["run_id"] == nil || r["run_id"].(float64) == 0 {
			t.Errorf("result missing run_id: %v", r)
		}
	}

	if len(runStore.CreateRunCalls) != 2 {
		t.Fatalf("runs created = %d, want 2", len(runStore.CreateRunCalls))
	}

	seenProjects := map[string]bool{}
	for _, call := range runStore.CreateRunCalls {
		seenProjects[call.ProjectName] = true
		if call.StartedBy != "webhook:shared" {
			t.Errorf("started_by = %q, want webhook:shared", call.StartedBy)
		}
	}
	if !seenProjects["billing"] || !seenProjects["checkout"] {
		t.Errorf("seen projects = %v, want billing and checkout", seenProjects)
	}

	for i := 0; i < 100; i++ {
		sessions.mu.Lock()
		cleaned := sessions.cleaned
		sessions.mu.Unlock()
		if cleaned == 2 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	sessions.mu.Lock()
	cleaned := sessions.cleaned
	sessions.mu.Unlock()
	if cleaned != 2 {
		t.Errorf("sessions.cleaned = %d, want 2", cleaned)
	}
}

// 3.2.5 Secret resolution failure records a failed run.
func TestWebhookStartsRun_SecretResolutionFailureRecordsFailedRun(t *testing.T) {
	const secretPipeline = `project "shop" {
	variable "secret_token" {
		secret_ref = "my_secret"
	}
	ci_repo "app" {
		path = "."
	}
	ci_task "deploy" {
		trigger = "repo.app.changed"
		execute {
			repo = "app"
			script = "echo deploy"
		}
	}
}`
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "pipeline.tf"), []byte(secretPipeline), 0o600); err != nil {
		t.Fatal(err)
	}
	url := "file://" + dir
	proj := &domain.ProjectConfig{
		Name: "shop", RepoURL: url, RepoRef: "main", PipelineDir: ".",
		Sources: []domain.ProjectSource{{Name: "shop", URL: url, Primary: true}},
	}

	runStore := &mocks.MockRunStore{CreateRunID: 88}
	srv, _, log, sessions := testWebhookServer(t, runStore, proj)
	srv.credentialStore = &fakeCredentialStore{
		items:      map[string]domain.CredentialMetadata{},
		resolveErr: fmt.Errorf("%w: my_secret", domain.ErrCredentialRevoked),
	}

	body := sourcePush(0, []string{url}, "revoked-sha", "main.go")
	rec := postWebhook(t, srv, body)

	// Webhook response fails with 400
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body = %s", rec.Code, rec.Body.String())
	}
	resp := decodeBody(t, rec)
	if resp["status"] != "error" {
		t.Errorf("response status = %v, want error", resp["status"])
	}

	// Verify failed run recorded in runStore
	if len(runStore.CreateRunCalls) != 1 {
		t.Fatalf("runs created = %d, want 1", len(runStore.CreateRunCalls))
	}
	if runStore.CreateRunCalls[0].StartedBy != "webhook:shop" {
		t.Errorf("started_by = %q, want webhook:shop", runStore.CreateRunCalls[0].StartedBy)
	}
	if len(runStore.FinishRunCalls) != 1 {
		t.Fatalf("FinishRun calls = %d, want 1", len(runStore.FinishRunCalls))
	}
	finish := runStore.FinishRunCalls[0]
	if finish.Status != "failed" {
		t.Errorf("finish status = %q, want failed", finish.Status)
	}
	if !strings.Contains(finish.ErrMsg, "secret credential is revoked") {
		t.Errorf("finish error = %q, want secret credential is revoked", finish.ErrMsg)
	}

	// Session is cleaned up exactly once on secret resolution error
	sessions.mu.Lock()
	cleaned := sessions.cleaned
	sessions.mu.Unlock()
	if cleaned != 1 {
		t.Errorf("sessions.cleaned = %d, want 1", cleaned)
	}

	// Webhook history recorded as error
	events := log.List()
	if len(events) != 1 || events[0].Status != domain.WebhookStatusError {
		t.Errorf("events = %+v", events)
	}
}

// 3.2.6 Verify secondary push where matched ci_repo has no ref falls back to default_branch.
func TestWebhookStartsRun_SecondaryPushFallsBackToDefaultBranch(t *testing.T) {
	const unsetRefPipeline = `project "shop" {
	ci_repo "shared" {
		path = "shared"
	}
	ci_task "sync" {
		trigger = "repo.shared.changed"
		execute {
			repo = "shared"
			script = ":"
		}
	}
}`
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "pipeline.tf"), []byte(unsetRefPipeline), 0o600); err != nil {
		t.Fatal(err)
	}
	url := "file://" + dir
	proj := &domain.ProjectConfig{
		Name: "shop", RepoURL: url, RepoRef: "main", PipelineDir: ".",
		Sources: []domain.ProjectSource{
			{Name: "shop", URL: url, Primary: true},
			{Name: "shared", URL: sharedRepoURL, Primary: false},
		},
	}
	runStore := &mocks.MockRunStore{CreateRunID: 66, FinishRunCh: make(chan struct{}, 1)}
	srv, _, _, sessions := testWebhookServer(t, runStore, proj)

	// Push to default branch (main)
	body := `{"object_kind":"push","ref":"refs/heads/main","checkout_sha":"sec456","project":{"id":0,"path_with_namespace":"team/shared","default_branch":"main","git_http_url":"` + sharedRepoURL + `"},"commits":[{"id":"sec456","modified":["pkg/util.go"]}]}`
	rec := postWebhook(t, srv, body)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	resp := decodeBody(t, rec)
	if resp["run_id"] != float64(66) {
		t.Fatalf("run_id = %v, want 66", resp["run_id"])
	}
	if len(runStore.CreateRunCalls) != 1 {
		t.Fatalf("CreateRun calls = %d, want 1", len(runStore.CreateRunCalls))
	}
	<-runStore.FinishRunCh
	sessions.mu.Lock()
	cleaned := sessions.cleaned
	sessions.mu.Unlock()
	if cleaned != 1 {
		t.Errorf("sessions.cleaned = %d, want 1", cleaned)
	}
}

// 3.3 Integration test against Postgres: a signed delivery to POST /webhook
// creates a run row with started_by webhook:<project> that reaches a terminal status
// in local mode.
func TestWebhookStartsRun_PostgresIntegration(t *testing.T) {
	pg := testWebPG(t)
	projectName := fmt.Sprintf("webhook-pg-%d", time.Now().UnixNano())
	pipelineHCL := fmt.Sprintf(`project "%s" {
	ci_repo "app" {
		path = "."
	}
	ci_task "hello" {
		trigger = "repo.app.changed"
		execute {
			repo = "app"
			script = "echo hello-webhook"
		}
	}
}`, projectName)

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "pipeline.tf"), []byte(pipelineHCL), 0o600); err != nil {
		t.Fatal(err)
	}
	repoURL := "file://" + dir
	cfg := &domain.ProjectConfig{
		Name:          projectName,
		RepoURL:       repoURL,
		RepoRef:       "main",
		PipelineDir:   ".",
		PipelineFile:  "pipeline.tf",
		Mode:          "local",
		WebhookSecret: "secret-pg",
		Sources: []domain.ProjectSource{
			{Name: projectName, URL: repoURL, Primary: true},
		},
	}
	if err := pg.Upsert(cfg); err != nil {
		t.Fatalf("Upsert project: %v", err)
	}

	pipelineExec := &runner.PipelineExecutor{Mode: "local"}
	runService := service.NewRunService(pg, pipelineExec, pipelineExec)
	runService.SetLocalExecutionPolicy(true, "local")
	projectService := service.NewProjectService(pg)
	projectService.SetAllowLocal(true)

	srv := &Server{
		cfg: Config{
			ListenAddr: ":0",
			Signature:  SignatureConfig{Method: "token", Secret: "secret-pg"},
			Runner:     RunnerConfig{Mode: "local", AllowLocal: true},
		},
		projectStore:   pg,
		runStore:       pg,
		runService:     runService,
		projectService: projectService,
		sourceSessions: &testSourceSessions{root: dir},
	}
	srv.router = srv.buildRouter()

	payload := fmt.Sprintf(`{
		"object_kind": "push",
		"ref": "refs/heads/main",
		"checkout_sha": "pg-test-sha",
		"project": {
			"id": 0,
			"path_with_namespace": "%s",
			"default_branch": "main",
			"git_http_url": "%s"
		},
		"commits": [{"id": "pg-test-sha", "modified": ["main.go"]}]
	}`, projectName, repoURL)

	req := httptest.NewRequest("POST", "/webhook", strings.NewReader(payload))
	req.Header.Set("X-Gitlab-Token", "secret-pg")
	rec := httptest.NewRecorder()
	srv.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	runIDVal, ok := resp["run_id"].(float64)
	if !ok || runIDVal == 0 {
		t.Fatalf("response missing run_id: %v", resp)
	}
	runID := int(runIDVal)

	// Poll until terminal status reached in local mode
	var finalRun *domain.Run
	for start := time.Now(); time.Since(start) < 10*time.Second; time.Sleep(50 * time.Millisecond) {
		r, err := pg.GetRun(runID)
		if err != nil {
			t.Fatalf("GetRun: %v", err)
		}
		if r != nil && r.Status != "running" && r.Status != "queued" {
			finalRun = r
			break
		}
	}
	if finalRun == nil {
		t.Fatalf("run %d did not reach terminal status within 10s", runID)
	}
	if finalRun.Status != "passed" {
		t.Errorf("run status = %q, want passed", finalRun.Status)
	}
	wantActor := "webhook:" + projectName
	if finalRun.StartedBy != wantActor {
		t.Errorf("started_by = %q, want %q", finalRun.StartedBy, wantActor)
	}
}

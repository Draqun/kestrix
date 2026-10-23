package web

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"kestrix/internal/credentials"
	"kestrix/internal/domain"
	"kestrix/internal/model"
	"kestrix/internal/service"
	"kestrix/internal/store"
)

func testWebPG(t *testing.T) *store.PGStore {
	t.Helper()
	if testing.Short() {
		t.Skip("integration test")
	}
	dbURL := os.Getenv("KESTRIX_TEST_DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://kestrix:kestrix@localhost:5433/kestrix_test?sslmode=disable&connect_timeout=2"
	}
	parsed, err := url.Parse(dbURL)
	if err == nil {
		dbName := strings.TrimPrefix(parsed.Path, "/")
		if !strings.HasSuffix(dbName, "_test") {
			t.Fatalf("tests must use a database ending with _test (got %q); set KESTRIX_TEST_DATABASE_URL", dbName)
		}
	}
	pg, err := store.NewPG(dbURL, "local")
	if err != nil {
		t.Skipf("PostgreSQL not available: %v", err)
	}

	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	keyPath := filepath.Join(t.TempDir(), "master.key")
	if err := os.WriteFile(keyPath, key, 0o600); err != nil {
		t.Fatal(err)
	}
	pg.SetCredentialMasterKeyProvider(credentials.FileMasterKeyProvider{Path: keyPath, Version: 1})

	return pg
}

// 3.1 Handler test on Postgres with a fake source session: approving the recorded gate
// of a remote run re-opens the recorded source, creates a job for the gate, leaves
// the task pending and the run running; approving a skipped gate answers 400.
func TestHandleApproveManualTask_RemoteGateOnPostgres(t *testing.T) {
	pg := testWebPG(t)
	proj := &model.Project{
		Name: "remote-gate-approval-proj",
		Repos: []domain.Repo{
			{Name: "app"},
		},
		Tasks: []model.Task{
			{Name: "build", Trigger: "repo.app.changed", Execute: domain.ExecuteSpec{Repo: "app", Script: "echo build"}},
			{Name: "deploy", Trigger: "repo.app.changed", DependsOn: []string{"build"}, Manual: true, Execute: domain.ExecuteSpec{Repo: "app", Script: "echo deploy"}},
		},
	}
	if err := pg.Upsert(&domain.ProjectConfig{
		Name:    proj.Name,
		RepoURL: "https://git.example/app.git",
		RepoRef: "main",
		Mode:    "remote",
	}); err != nil {
		t.Fatalf("Upsert project: %v", err)
	}

	runService := service.NewRunService(pg, nil, nil)
	runService.SetRemoteJobStore(pg)
	projectService := service.NewProjectService(pg)

	fakeSessions := &testSourceSessions{root: t.TempDir()}

	srv := &Server{
		cfg:            Config{ListenAddr: ":0", Runner: RunnerConfig{Mode: "local"}},
		projectStore:   pg,
		runStore:       pg,
		remoteJobStore: pg,
		runService:     runService,
		projectService: projectService,
		sourceSessions: fakeSessions,
	}

	// ── Case 1: approve recorded manual gate ──
	initialSources := []domain.AcquiredSource{
		{RepoName: "app", URL: "https://git.example/app.git", RequestedRef: "main", ResolvedRevision: "rev-1234", Primary: true},
	}
	runID, err := runService.Start(context.Background(), proj.Name, proj, []string{"app"}, domain.RunnerOptions{
		Mode:           "remote",
		WorkDir:        t.TempDir(),
		InitialSources: initialSources,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	runnerID, _, err := pg.CreateRunner("runner-approve", nil)
	if err != nil {
		t.Fatalf("CreateRunner: %v", err)
	}

	// build passes
	buildJob, err := pg.ClaimNextJob(runnerID)
	if err != nil || buildJob == nil {
		t.Fatalf("ClaimNextJob(build): %v, job: %+v", err, buildJob)
	}
	if ok, err := runService.CompleteRemoteJob(buildJob.ID, buildJob.ClaimEpoch, "passed", "built", "", &runnerID); err != nil || !ok {
		t.Fatalf("CompleteRemoteJob(build): %v, ok: %v", err, ok)
	}

	run, err := pg.GetRun(runID)
	if err != nil || run == nil {
		t.Fatalf("GetRun: %v", err)
	}
	if run.Status != "running" {
		t.Fatalf("run status after build passed = %q, want running", run.Status)
	}

	// Clear recorded source sessions so we specifically verify the approval call's re-open
	fakeSessions.mu.Lock()
	fakeSessions.requests = nil
	fakeSessions.mu.Unlock()

	// Approve deploy gate
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", fmt.Sprintf("/api/projects/%s/runs/%d/tasks/deploy/run", proj.Name, runID), nil)
	req = req.WithContext(contextWithClaims(req.Context(), domain.Claims{UserName: "operator1", Role: domain.RoleOperator}))
	r := runCtx(req, "name", proj.Name, "runID", fmt.Sprint(runID), "task", "deploy")
	srv.handleApproveManualTask(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("handleApproveManualTask status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}

	// Verify fake source session was re-opened with recorded revision
	fakeSessions.mu.Lock()
	reqCount := len(fakeSessions.requests)
	var lastReq domain.SourceRequest
	if reqCount > 0 {
		lastReq = fakeSessions.requests[reqCount-1]
	}
	fakeSessions.mu.Unlock()
	if reqCount == 0 {
		t.Fatal("expected fake source session to have been re-opened, got 0 requests")
	}
	if lastReq.Revision != "rev-1234" || lastReq.Purpose != "manual-approval" {
		t.Fatalf("re-opened source request = %+v, want Revision=rev-1234, Purpose=manual-approval", lastReq)
	}

	// Verify the task is pending and the run is running right after approval
	runAfter, err := pg.GetRun(runID)
	if err != nil || runAfter == nil {
		t.Fatalf("GetRun: %v", err)
	}
	if runAfter.Status != "running" {
		t.Fatalf("run status after approval = %q, want running", runAfter.Status)
	}
	results := make(map[string]domain.TaskResult)
	for _, tr := range runAfter.TaskResults {
		results[tr.Name] = tr
	}
	if results["deploy"].Status != "pending" {
		t.Fatalf("deploy task status = %q, want pending", results["deploy"].Status)
	}

	// Verify a remote job was created for the gate and is claimable
	deployJob, err := pg.ClaimNextJob(runnerID)
	if err != nil || deployJob == nil {
		t.Fatalf("expected deploy job to be created and claimable, err: %v, job: %+v", err, deployJob)
	}
	if deployJob.TaskName != "deploy" || deployJob.RunID != runID {
		t.Fatalf("claimed job = %+v, want deploy for run %d", deployJob, runID)
	}

	// ── Case 2: approving a skipped gate answers 400 ──
	failRunID, err := runService.Start(context.Background(), proj.Name, proj, []string{"app"}, domain.RunnerOptions{
		Mode:           "remote",
		WorkDir:        t.TempDir(),
		InitialSources: initialSources,
	})
	if err != nil {
		t.Fatalf("Start(failRun): %v", err)
	}

	failBuildJob, err := pg.ClaimNextJob(runnerID)
	if err != nil || failBuildJob == nil {
		t.Fatalf("ClaimNextJob(failBuildJob): %v, job: %+v", err, failBuildJob)
	}
	if ok, err := runService.CompleteRemoteJob(failBuildJob.ID, failBuildJob.ClaimEpoch, "failed", "broke", "exit 1", &runnerID); err != nil || !ok {
		t.Fatalf("CompleteRemoteJob(failBuildJob): %v, ok: %v", err, ok)
	}

	failRun, err := pg.GetRun(failRunID)
	if err != nil || failRun == nil {
		t.Fatalf("GetRun(failRun): %v", err)
	}
	if failRun.Status != "failed" {
		t.Fatalf("failRun status = %q, want failed", failRun.Status)
	}

	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("POST", fmt.Sprintf("/api/projects/%s/runs/%d/tasks/deploy/run", proj.Name, failRunID), nil)
	req2 = req2.WithContext(contextWithClaims(req2.Context(), domain.Claims{UserName: "operator1", Role: domain.RoleOperator}))
	r2 := runCtx(req2, "name", proj.Name, "runID", fmt.Sprint(failRunID), "task", "deploy")
	srv.handleApproveManualTask(w2, r2)

	if w2.Code != http.StatusBadRequest {
		t.Fatalf("approving skipped gate status = %d, want 400 (body=%s)", w2.Code, w2.Body.String())
	}
}

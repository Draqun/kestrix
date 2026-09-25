package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"kestrix/internal/config"
	"kestrix/internal/domain"
	"kestrix/internal/mocks"
	"kestrix/internal/service"
	"kestrix/internal/web"
)

// ── buildRunnerOptions carries no control-plane fields ──────────────

func TestBuildRunnerOptions_NoControlPlaneFields(t *testing.T) {
	payload := domain.RemoteJobPayload{
		Task:      domain.Task{Name: "build", Execute: domain.ExecuteSpec{Repo: "app", Script: "echo hi"}},
		Variables: map[string]string{"KEY": "value"},
	}
	opts := buildRunnerOptions("/tmp/workdir", payload, "local", io.Discard)

	if opts.SourceSessions != nil {
		t.Fatal("SourceSessions must be zero: the runner never acquires source itself (D6)")
	}
	if opts.SourceAcquirer != nil {
		t.Fatal("SourceAcquirer must be zero")
	}
	if opts.CredentialResolver != nil {
		t.Fatal("CredentialResolver must be zero: the runner never resolves a credential")
	}
	if opts.RepoPaths != nil {
		t.Fatal("RepoPaths must be zero")
	}
	if !opts.SourcesPrepared {
		t.Fatal("SourcesPrepared must be true: workDir is already the unpacked archive")
	}
	if opts.WorkDir != "/tmp/workdir" {
		t.Fatalf("WorkDir = %q, want /tmp/workdir", opts.WorkDir)
	}
	if got := opts.Variables.Get("", "", ""); got["KEY"] != "value" {
		t.Fatalf("Variables.Get = %v, want KEY=value", got)
	}
	if opts.Mode != "local" {
		t.Fatalf("Mode = %q, want local", opts.Mode)
	}
}

// ── flatVariableResolver ─────────────────────────────────────────────

func TestFlatVariableResolver_IgnoresScopeArgs(t *testing.T) {
	r := flatVariableResolver{"A": "1"}
	got1 := r.Get("repo1", "job1", "env1")
	got2 := r.Get("repo2", "job2", "env2")
	if got1["A"] != "1" || got2["A"] != "1" {
		t.Fatalf("expected the same flat map regardless of scope args, got %v / %v", got1, got2)
	}
}

// ── unpackArchive / safeJoin ─────────────────────────────────────────

func buildTestArchive(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, content := range files {
		hdr := &tar.Header{Name: name, Mode: 0o644, Size: int64(len(content))}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestUnpackArchive_RoundTrips(t *testing.T) {
	archive := buildTestArchive(t, map[string]string{
		"app/main.go":  "package main",
		"app/sub/x.go": "package sub",
	})
	dest := t.TempDir()
	if err := unpackArchive(archive, dest); err != nil {
		t.Fatalf("unpackArchive: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dest, "app", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "package main" {
		t.Fatalf("content = %q, want %q", data, "package main")
	}
	if _, err := os.Stat(filepath.Join(dest, "app", "sub", "x.go")); err != nil {
		t.Fatalf("nested file missing: %v", err)
	}
}

func TestUnpackArchive_EmptyIsNoOp(t *testing.T) {
	if err := unpackArchive(nil, t.TempDir()); err != nil {
		t.Fatalf("unpackArchive(nil): %v", err)
	}
}

func TestUnpackArchive_MalformedGzipErrors(t *testing.T) {
	if err := unpackArchive([]byte("not a gzip stream"), t.TempDir()); err == nil {
		t.Fatal("expected an error for a malformed archive")
	}
}

func TestSafeJoin_RejectsPathTraversal(t *testing.T) {
	if _, err := safeJoin("/tmp/dest", "../../etc/passwd"); err == nil {
		t.Fatal("expected an error for a path-traversal entry")
	}
}

func TestSafeJoin_AllowsOrdinaryPath(t *testing.T) {
	got, err := safeJoin("/tmp/dest", "app/main.go")
	if err != nil {
		t.Fatalf("safeJoin: %v", err)
	}
	if got != filepath.Join("/tmp/dest", "app", "main.go") {
		t.Fatalf("safeJoin = %q", got)
	}
}

func buildTestArchiveEntries(t *testing.T, entries []tar.Header, bodies map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for i := range entries {
		hdr := entries[i]
		body := bodies[hdr.Name]
		if hdr.Typeflag == 0 {
			hdr.Typeflag = tar.TypeReg
		}
		hdr.Size = int64(len(body))
		if err := tw.WriteHeader(&hdr); err != nil {
			t.Fatal(err)
		}
		if body != "" {
			if _, err := tw.Write([]byte(body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestUnpackArchive_RejectsAbsoluteSymlinkTarget(t *testing.T) {
	archive := buildTestArchiveEntries(t, []tar.Header{
		{Name: "escape", Typeflag: tar.TypeSymlink, Linkname: "/etc", Mode: 0o777},
	}, nil)
	dest := t.TempDir()
	err := unpackArchive(archive, dest)
	if err == nil {
		t.Fatal("expected an error for a symlink with an absolute escaping target")
	}
	if _, statErr := os.Lstat(filepath.Join(dest, "escape")); statErr == nil {
		t.Fatal("escaping symlink must not be created")
	}
}

func TestUnpackArchive_RejectsRelativeSymlinkEscape(t *testing.T) {
	archive := buildTestArchiveEntries(t, []tar.Header{
		{Name: "escape", Typeflag: tar.TypeSymlink, Linkname: "../../../outside", Mode: 0o777},
	}, nil)
	dest := t.TempDir()
	if err := unpackArchive(archive, dest); err == nil {
		t.Fatal("expected an error for a symlink escaping via relative traversal")
	}
}

func TestUnpackArchive_RejectsWriteThroughPlantedSymlink(t *testing.T) {
	outsideDir := t.TempDir()
	dest := t.TempDir()
	archive := buildTestArchiveEntries(t, []tar.Header{
		{Name: "escape", Typeflag: tar.TypeSymlink, Linkname: outsideDir, Mode: 0o777},
		{Name: "escape/passwd", Typeflag: tar.TypeReg, Mode: 0o644},
	}, map[string]string{"escape/passwd": "pwned"})
	err := unpackArchive(archive, dest)
	if err == nil {
		t.Fatal("expected an error before the planted symlink could be written through")
	}
	if _, statErr := os.Stat(filepath.Join(outsideDir, "passwd")); statErr == nil {
		t.Fatal("write must not land outside destDir via the planted symlink")
	}
}

func TestUnpackArchive_AllowsSymlinkWithinDestDir(t *testing.T) {
	archive := buildTestArchiveEntries(t, []tar.Header{
		{Name: "app/", Typeflag: tar.TypeDir, Mode: 0o755},
		{Name: "app/main.go", Typeflag: tar.TypeReg, Mode: 0o644},
		{Name: "link", Typeflag: tar.TypeSymlink, Linkname: "app/main.go", Mode: 0o777},
	}, map[string]string{"app/main.go": "package main"})
	dest := t.TempDir()
	if err := unpackArchive(archive, dest); err != nil {
		t.Fatalf("unpackArchive: %v", err)
	}
	target, err := os.Readlink(filepath.Join(dest, "link"))
	if err != nil {
		t.Fatalf("Readlink: %v", err)
	}
	if target != "app/main.go" {
		t.Fatalf("symlink target = %q, want app/main.go", target)
	}
}

func TestSafeSymlinkTarget_RejectsAbsoluteEscape(t *testing.T) {
	if err := safeSymlinkTarget("/tmp/dest", "/tmp/dest/link", "/etc/passwd"); err == nil {
		t.Fatal("expected an error for an absolute escaping target")
	}
}

func TestSafeSymlinkTarget_AllowsRelativeWithinDestDir(t *testing.T) {
	if err := safeSymlinkTarget("/tmp/dest", "/tmp/dest/sub/link", "../app/main.go"); err != nil {
		t.Fatalf("safeSymlinkTarget: %v", err)
	}
}

// ── end-to-end: one real task through a real Kestrix HTTP server ────

// TestRunJob_EndToEndAgainstRealServer pins tasks.md 5.1's integration
// coverage: runJob executes a real task (unpacking a real archive) and
// reports completion to a real *web.Server's runner-job endpoints - not
// Postgres-backed, but every HTTP handler and internal/runner code path is
// the genuine production one.
func TestRunJob_EndToEndAgainstRealServer(t *testing.T) {
	archive := buildTestArchive(t, map[string]string{"app/.keep": ""})
	runStore := &mocks.MockRunStore{}
	runStore.AuthenticateRunnerID = 7
	runStore.AuthenticateRunnerOK = true
	runStore.Jobs = map[int]*domain.RemoteJob{
		1: {
			ID: 1, RunID: 1, TaskName: "build", ClaimEpoch: 1,
			Payload: domain.RemoteJobPayload{
				Task:    domain.Task{Name: "build", Execute: domain.ExecuteSpec{Repo: "app", Script: "echo hello-from-runner"}},
				Repos:   []domain.Repo{{Name: "app", Path: "app"}},
				Archive: archive,
			},
		},
	}
	runStore.AppendJobTraceOK = true
	runStore.CompleteJobOK = true

	projectStore := &mocks.MockProjectStore{}
	pipeline := &mocks.MockPipelineRunner{}
	runService := service.NewRunService(runStore, pipeline, pipeline)
	runService.SetRemoteJobStore(runStore)
	webhookService := service.NewWebhookService(nil)

	srv := web.NewServerWithDeps(web.Config{ListenAddr: ":0"}, projectStore, runStore,
		service.NewProjectService(projectStore), pipeline, pipeline, runService, webhookService)
	ts := httptest.NewServer(srv.Router())
	defer ts.Close()

	runJob(context.Background(), ts.Client(), ts.URL, "any-token-mock-accepts-all", "local", runStore.Jobs[1])

	if len(runStore.CompleteJobCalls) != 1 {
		t.Fatalf("expected one CompleteJob call, got %d: %+v", len(runStore.CompleteJobCalls), runStore.CompleteJobCalls)
	}
	call := runStore.CompleteJobCalls[0]
	if call.JobID != 1 || call.Epoch != 1 {
		t.Fatalf("CompleteJob call = %+v, want job 1 epoch 1", call)
	}
	if call.Status != "passed" {
		t.Fatalf("Status = %q, want passed (body of the trace calls carried the real output instead): %+v", call.Status, runStore.AppendTaskCalls)
	}
}

// ── pollNextJob ───────────────────────────────────────────────────────

func TestPollNextJob_ReturnsJobOn200(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/runner-jobs/next" || r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("unexpected request: %s %s auth=%q", r.Method, r.URL.Path, r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(domain.RemoteJob{ID: 42, TaskName: "build", ClaimEpoch: 3})
	}))
	defer ts.Close()

	job, status, err := pollNextJob(context.Background(), ts.Client(), ts.URL, "tok")
	if err != nil {
		t.Fatalf("pollNextJob: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if job == nil || job.ID != 42 || job.TaskName != "build" {
		t.Fatalf("job = %+v, want ID=42 TaskName=build", job)
	}
}

func TestPollNextJob_Returns204NoContent(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer ts.Close()

	job, status, err := pollNextJob(context.Background(), ts.Client(), ts.URL, "tok")
	if err != nil {
		t.Fatalf("pollNextJob: %v", err)
	}
	if status != http.StatusNoContent || job != nil {
		t.Fatalf("got (%v, %d), want (nil, 204)", job, status)
	}
}

func TestPollNextJob_NonOKNon204ReturnsError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("boom"))
	}))
	defer ts.Close()

	job, status, err := pollNextJob(context.Background(), ts.Client(), ts.URL, "tok")
	if err == nil {
		t.Fatal("expected an error for a non-200/204 response")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Fatalf("error = %v, want it to include the response body", err)
	}
	if status != http.StatusInternalServerError || job != nil {
		t.Fatalf("got (%v, %d), want (nil, 500)", job, status)
	}
}

func TestPollNextJob_MalformedBodyReturnsDecodeError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("{not json"))
	}))
	defer ts.Close()

	_, _, err := pollNextJob(context.Background(), ts.Client(), ts.URL, "tok")
	if err == nil {
		t.Fatal("expected a decode error for a malformed job body")
	}
}

func TestPollNextJob_RequestErrorPropagates(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := pollNextJob(ctx, http.DefaultClient, "http://127.0.0.1:1", "tok")
	if err == nil {
		t.Fatal("expected client.Do's error (already-canceled context) to propagate")
	}
}

// ── runPollLoop ──────────────────────────────────────────────────────

func TestRunPollLoop_ExitsOnContextCancel(t *testing.T) {
	old := pollErrorRetryDelay
	pollErrorRetryDelay = time.Millisecond
	defer func() { pollErrorRetryDelay = old }()

	var requests atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer ts.Close()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		runPollLoop(ctx, ts.Client(), ts.URL, "tok", "local")
		close(done)
	}()

	deadline := time.Now().Add(2 * time.Second)
	for requests.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("runPollLoop did not return after context cancellation")
	}
}

func TestRunPollLoop_ClaimsAndRunsJob(t *testing.T) {
	old := pollErrorRetryDelay
	pollErrorRetryDelay = time.Millisecond
	defer func() { pollErrorRetryDelay = old }()

	archive := buildTestArchive(t, map[string]string{"app/.keep": ""})
	runStore := &mocks.MockRunStore{}
	runStore.AuthenticateRunnerID = 7
	runStore.AuthenticateRunnerOK = true
	runStore.Jobs = map[int]*domain.RemoteJob{
		1: {
			ID: 1, RunID: 1, TaskName: "build", ClaimEpoch: 1,
			Payload: domain.RemoteJobPayload{
				Task:    domain.Task{Name: "build", Execute: domain.ExecuteSpec{Repo: "app", Script: "echo hi"}},
				Repos:   []domain.Repo{{Name: "app", Path: "app"}},
				Archive: archive,
			},
		},
	}
	runStore.AppendJobTraceOK = true
	runStore.CompleteJobOK = true
	// The mock's ClaimNextJob returns this fixed result on every call - it
	// does not simulate a real store's pending-to-claimed transition, so
	// runPollLoop may claim and run this same job more than once before the
	// test cancels it. That is fine: this test is about runPollLoop's
	// claim -> run -> report wiring, not exactly-once claiming (the real
	// store's SELECT ... FOR UPDATE SKIP LOCKED, covered by
	// internal/store's own tests, owns that).
	runStore.ClaimNextJobResult = runStore.Jobs[1]

	projectStore := &mocks.MockProjectStore{}
	pipeline := &mocks.MockPipelineRunner{}
	runService := service.NewRunService(runStore, pipeline, pipeline)
	runService.SetRemoteJobStore(runStore)
	webhookService := service.NewWebhookService(nil)
	srv := web.NewServerWithDeps(web.Config{ListenAddr: ":0"}, projectStore, runStore,
		service.NewProjectService(projectStore), pipeline, pipeline, runService, webhookService)
	ts := httptest.NewServer(srv.Router())
	defer ts.Close()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		runPollLoop(ctx, ts.Client(), ts.URL, "any-token-mock-accepts-all", "local")
		close(done)
	}()

	deadline := time.Now().Add(5 * time.Second)
	for runStore.CompleteJobCallCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("runPollLoop did not return after context cancellation")
	}

	if len(runStore.CompleteJobCalls) == 0 || runStore.CompleteJobCalls[0].Status != "passed" {
		t.Fatalf("expected at least one passed CompleteJob call, got %+v", runStore.CompleteJobCalls)
	}
}

func TestRunPollLoop_PollErrorRetriesThenExits(t *testing.T) {
	old := pollErrorRetryDelay
	pollErrorRetryDelay = time.Millisecond
	defer func() { pollErrorRetryDelay = old }()

	var requests atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		runPollLoop(ctx, ts.Client(), ts.URL, "tok", "local")
		close(done)
	}()

	deadline := time.Now().Add(2 * time.Second)
	for requests.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if requests.Load() < 2 {
		t.Fatal("expected runPollLoop to retry after a poll error")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("runPollLoop did not return after context cancellation")
	}
}

// ── reportComplete ───────────────────────────────────────────────────

func TestReportComplete_NonNoContentReturnsError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		w.Write([]byte("stale epoch"))
	}))
	defer ts.Close()

	err := reportComplete(context.Background(), ts.Client(), ts.URL, "tok", 1, 1, "passed", "")
	if err == nil {
		t.Fatal("expected an error for a non-204 response")
	}
	if !strings.Contains(err.Error(), "stale epoch") {
		t.Fatalf("error = %v, want it to include the response body", err)
	}
}

func TestReportComplete_NetworkErrorPropagates(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	badURL := ts.URL
	ts.Close() // closed before use: every request against it now fails to connect.

	err := reportComplete(context.Background(), http.DefaultClient, badURL, "tok", 1, 1, "passed", "")
	if err == nil {
		t.Fatal("expected client.Do's connection error to propagate")
	}
}

func TestReportComplete_RequestBuildErrorPropagates(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := reportComplete(ctx, http.DefaultClient, "http://127.0.0.1:1", "tok", 1, 1, "passed", "")
	if err == nil {
		t.Fatal("expected an error from an already-canceled context")
	}
}

func TestReportComplete_Success(t *testing.T) {
	var gotBody map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer ts.Close()

	if err := reportComplete(context.Background(), ts.Client(), ts.URL, "tok", 5, 2, "failed", "boom"); err != nil {
		t.Fatalf("reportComplete: %v", err)
	}
	if gotBody["status"] != "failed" || gotBody["output"] != "boom" {
		t.Fatalf("request body = %+v, want status=failed output=boom", gotBody)
	}
}

// ── traceWriter.Write ────────────────────────────────────────────────

func TestTraceWriter_Write_NetworkErrorStillReturnsLenPNil(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	badURL := ts.URL
	ts.Close()

	tw := &traceWriter{client: http.DefaultClient, baseURL: badURL, token: "tok", jobID: 1, epoch: 1}
	n, err := tw.Write([]byte("hello"))
	if err != nil {
		t.Fatalf("Write must never fail on a push error, got %v", err)
	}
	if n != 5 {
		t.Fatalf("n = %d, want 5 (len(p)), even though the push failed", n)
	}
	if tw.offset != 0 {
		t.Fatalf("offset = %d, want 0: a failed push must not advance it", tw.offset)
	}
}

func TestTraceWriter_Write_NonNoContentStillReturnsLenPNil(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
	}))
	defer ts.Close()

	tw := &traceWriter{client: ts.Client(), baseURL: ts.URL, token: "tok", jobID: 1, epoch: 1}
	n, err := tw.Write([]byte("hello"))
	if err != nil {
		t.Fatalf("Write must never fail on a rejected push, got %v", err)
	}
	if n != 5 {
		t.Fatalf("n = %d, want 5 (len(p))", n)
	}
	if tw.offset != 0 {
		t.Fatalf("offset = %d, want 0: a rejected push must not advance it", tw.offset)
	}
}

func TestTraceWriter_Write_SuccessAdvancesOffset(t *testing.T) {
	var gotOffset int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		gotOffset = int(body["offset"].(float64))
		w.WriteHeader(http.StatusNoContent)
	}))
	defer ts.Close()

	tw := &traceWriter{client: ts.Client(), baseURL: ts.URL, token: "tok", jobID: 1, epoch: 1}
	if _, err := tw.Write([]byte("hello")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if tw.offset != 5 {
		t.Fatalf("offset = %d, want 5 after a successful push", tw.offset)
	}
	if _, err := tw.Write([]byte("!")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if gotOffset != 5 {
		t.Fatalf("second push's offset field = %d, want 5 (the first write's length)", gotOffset)
	}
	if tw.offset != 6 {
		t.Fatalf("offset = %d, want 6", tw.offset)
	}
}

// ── resolveRunnerFlags (REVIEW.md R9) ───────────────────────────────

func TestResolveRunnerFlags_FlagsWinOverEnv(t *testing.T) {
	env := &config.Environment{RunnerServer: "https://env.example.com", RunnerToken: "env-tok", RunnerMode: "docker"}
	server, token, mode := resolveRunnerFlags("https://flag.example.com", "flag-tok", "local", env)
	if server != "https://flag.example.com" || token != "flag-tok" || mode != "local" {
		t.Fatalf("got (%q, %q, %q), want the flag values to win", server, token, mode)
	}
}

func TestResolveRunnerFlags_FallsBackToEnvWhenFlagsEmpty(t *testing.T) {
	env := &config.Environment{RunnerServer: "https://env.example.com", RunnerToken: "env-tok", RunnerMode: "docker"}
	server, token, mode := resolveRunnerFlags("", "", "", env)
	if server != "https://env.example.com" || token != "env-tok" || mode != "docker" {
		t.Fatalf("got (%q, %q, %q), want the env values", server, token, mode)
	}
}

func TestResolveRunnerFlags_ModeDefaultsToLocal(t *testing.T) {
	env := &config.Environment{}
	_, _, mode := resolveRunnerFlags("https://x.example.com", "tok", "", env)
	if mode != "local" {
		t.Fatalf("mode = %q, want local when neither flag nor env sets it", mode)
	}
}

func TestResolveRunnerFlags_TrimsTrailingSlash(t *testing.T) {
	env := &config.Environment{}
	server, _, _ := resolveRunnerFlags("https://x.example.com/", "tok", "local", env)
	if server != "https://x.example.com" {
		t.Fatalf("server = %q, want the trailing slash trimmed", server)
	}
}

// ── cmdRunner validation (subprocess: cmdRunner os.Exits on failure) ──

func TestCLI_Runner_MissingServerAndTokenExits2(t *testing.T) {
	bin := buildKestrix(t)
	cmd := exec.Command(bin, "runner")
	cmd.Env = filterEnv(os.Environ(), "KESTRIX_RUNNER_SERVER", "KESTRIX_RUNNER_TOKEN")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	ee, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("expected an *exec.ExitError, got %v (stderr=%s)", err, stderr.String())
	}
	if ee.ExitCode() != 2 {
		t.Fatalf("exit code = %d, want 2 (stderr=%s)", ee.ExitCode(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "--server and --token") {
		t.Fatalf("stderr = %q, want it to name the missing required flags", stderr.String())
	}
}

// filterEnv returns env with every KEY=value pair named in drop removed -
// used to force KESTRIX_RUNNER_SERVER/TOKEN unset for a subprocess
// regardless of what the parent test process's own environment carries.
func filterEnv(env []string, drop ...string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		skip := false
		for _, d := range drop {
			if strings.HasPrefix(kv, d+"=") {
				skip = true
				break
			}
		}
		if !skip {
			out = append(out, kv)
		}
	}
	return out
}

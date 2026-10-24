package web

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"kestrix/internal/domain"
	"kestrix/internal/mocks"
	"kestrix/internal/service"
)

func buildTestArtifactArchive(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	for name, content := range files {
		hdr := &tar.Header{
			Name: name,
			Mode: 0o644,
			Size: int64(len(content)),
		}
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
	if err := gw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func artifactReq(body []byte, jobIDStr, epochQuery string, runnerID int) *http.Request {
	url := fmt.Sprintf("/api/runner-jobs/%s/artifacts", jobIDStr)
	if epochQuery != "" {
		url += "?" + epochQuery
	}
	r := httptest.NewRequest("PUT", url, bytes.NewReader(body))
	if jobIDStr != "" {
		r = runCtx(r, "id", jobIDStr)
	}
	if runnerID > 0 {
		r = r.WithContext(contextWithRunnerID(r.Context(), runnerID))
	}
	return r
}

func defaultArtifactJob() *domain.RemoteJob {
	runnerID := 1
	return &domain.RemoteJob{
		ID:         1,
		RunID:      10,
		TaskName:   "build",
		Status:     "running",
		RunnerID:   &runnerID,
		ClaimEpoch: 1,
		Payload: domain.RemoteJobPayload{
			Task: domain.Task{
				Name:      "build",
				Artifacts: []string{"dist/"},
				Execute:   domain.ExecuteSpec{Repo: "app"},
			},
			Repos: []domain.Repo{{Name: "app", Path: "app"}},
		},
	}
}

func TestHandleRunnerJobArtifacts_Success(t *testing.T) {
	job := defaultArtifactJob()
	remoteJobs := &mocks.MockRemoteJobStore{
		Jobs: map[int]*domain.RemoteJob{1: job},
	}
	srv := setupRunnerJobServer(t, &mocks.MockRunnerStore{}, remoteJobs, nil)

	body := buildTestArtifactArchive(t, map[string]string{
		"app/dist/bundle.js": "console.log('hi')",
	})

	w := httptest.NewRecorder()
	srv.handleRunnerJobArtifacts(w, artifactReq(body, "1", "epoch=1", 1))

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204: %s", w.Code, w.Body.String())
	}
	if !bytes.Equal(remoteJobs.Artifacts["10:build"], body) {
		t.Fatalf("stored artifact does not match body")
	}
}

func TestHandleRunnerJobArtifacts_InvalidJobID(t *testing.T) {
	srv := setupRunnerJobServer(t, &mocks.MockRunnerStore{}, &mocks.MockRemoteJobStore{}, nil)
	w := httptest.NewRecorder()
	srv.handleRunnerJobArtifacts(w, artifactReq([]byte("data"), "not-a-number", "epoch=1", 1))

	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "invalid job id") {
		t.Fatalf("status = %d, want 400 invalid job id: %s", w.Code, w.Body.String())
	}
}

func TestHandleRunnerJobArtifacts_InvalidOrMissingEpoch(t *testing.T) {
	srv := setupRunnerJobServer(t, &mocks.MockRunnerStore{}, &mocks.MockRemoteJobStore{}, nil)

	// Missing epoch
	w1 := httptest.NewRecorder()
	srv.handleRunnerJobArtifacts(w1, artifactReq([]byte("data"), "1", "", 1))
	if w1.Code != http.StatusBadRequest || !strings.Contains(w1.Body.String(), "invalid or missing epoch") {
		t.Fatalf("status = %d, want 400 invalid or missing epoch: %s", w1.Code, w1.Body.String())
	}

	// Non-numeric epoch
	w2 := httptest.NewRecorder()
	srv.handleRunnerJobArtifacts(w2, artifactReq([]byte("data"), "1", "epoch=abc", 1))
	if w2.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for epoch=abc", w2.Code)
	}

	// Non-positive epoch
	w3 := httptest.NewRecorder()
	srv.handleRunnerJobArtifacts(w3, artifactReq([]byte("data"), "1", "epoch=0", 1))
	if w3.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for epoch=0", w3.Code)
	}
}

func TestHandleRunnerJobArtifacts_Unauthenticated(t *testing.T) {
	srv := setupRunnerJobServer(t, &mocks.MockRunnerStore{}, &mocks.MockRemoteJobStore{}, nil)
	w := httptest.NewRecorder()
	// No runner ID in context
	srv.handleRunnerJobArtifacts(w, artifactReq([]byte("data"), "1", "epoch=1", 0))

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 when unauthenticated direct call: %s", w.Code, w.Body.String())
	}
}

func TestHandleRunnerJobArtifacts_StaleEpochOrNotOwnedOrTerminal(t *testing.T) {
	job := defaultArtifactJob()
	remoteJobs := &mocks.MockRemoteJobStore{
		Jobs: map[int]*domain.RemoteJob{1: job},
	}
	srv := setupRunnerJobServer(t, &mocks.MockRunnerStore{}, remoteJobs, nil)
	body := buildTestArtifactArchive(t, map[string]string{"app/dist/bundle.js": "ok"})

	// Stale epoch (request epoch 2, job epoch 1)
	w1 := httptest.NewRecorder()
	srv.handleRunnerJobArtifacts(w1, artifactReq(body, "1", "epoch=2", 1))
	if w1.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 for stale epoch", w1.Code)
	}

	// Not owned (request runner 2, job runner 1)
	w2 := httptest.NewRecorder()
	srv.handleRunnerJobArtifacts(w2, artifactReq(body, "1", "epoch=1", 2))
	if w2.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 for wrong runner", w2.Code)
	}

	// Terminal job (status passed)
	job.Status = "passed"
	w3 := httptest.NewRecorder()
	srv.handleRunnerJobArtifacts(w3, artifactReq(body, "1", "epoch=1", 1))
	if w3.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 for terminal job", w3.Code)
	}

	// Unknown job
	w4 := httptest.NewRecorder()
	srv.handleRunnerJobArtifacts(w4, artifactReq(body, "999", "epoch=1", 1))
	if w4.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 for unknown job", w4.Code)
	}
}

func TestHandleRunnerJobArtifacts_InvalidGzip(t *testing.T) {
	job := defaultArtifactJob()
	remoteJobs := &mocks.MockRemoteJobStore{
		Jobs: map[int]*domain.RemoteJob{1: job},
	}
	srv := setupRunnerJobServer(t, &mocks.MockRunnerStore{}, remoteJobs, nil)

	w := httptest.NewRecorder()
	srv.handleRunnerJobArtifacts(w, artifactReq([]byte("not-gzip-data"), "1", "epoch=1", 1))

	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "invalid gzip archive") {
		t.Fatalf("status = %d, want 400 invalid gzip archive: %s", w.Code, w.Body.String())
	}
}

func TestHandleRunnerJobArtifacts_OutsideDeclaredPaths(t *testing.T) {
	job := defaultArtifactJob()
	remoteJobs := &mocks.MockRemoteJobStore{
		Jobs: map[int]*domain.RemoteJob{1: job},
	}
	srv := setupRunnerJobServer(t, &mocks.MockRunnerStore{}, remoteJobs, nil)

	// File in app/secret.txt when task declared only app/dist/
	body := buildTestArtifactArchive(t, map[string]string{
		"app/secret.txt": "leak",
	})

	w := httptest.NewRecorder()
	srv.handleRunnerJobArtifacts(w, artifactReq(body, "1", "epoch=1", 1))

	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "outside declared paths") {
		t.Fatalf("status = %d, want 400 outside declared paths: %s", w.Code, w.Body.String())
	}
}

func TestHandleRunnerJobArtifacts_OverCapRejection413(t *testing.T) {
	job := defaultArtifactJob()
	runnerStore := &mocks.MockRunnerStore{
		AuthenticateRunnerID: 1,
		AuthenticateRunnerOK: true,
	}
	remoteJobs := &mocks.MockRemoteJobStore{
		Jobs: map[int]*domain.RemoteJob{1: job},
	}
	runStore := &mocks.MockRunStore{}
	pipeline := &mocks.MockPipelineRunner{}
	runService := service.NewRunService(runStore, pipeline, pipeline)
	runService.SetRemoteJobStore(remoteJobs)

	// Configure server with small artifact limit (64 bytes)
	srv := NewServerWithDeps(Config{ListenAddr: ":0", RunnerArtifactMaxBytes: 64}, nil, runStore, nil, pipeline, pipeline, runService, nil)
	srv.runnerStore = runnerStore
	srv.remoteJobStore = remoteJobs

	bigBody := buildTestArtifactArchive(t, map[string]string{
		"app/dist/large.txt": strings.Repeat("x", 200),
	})

	req := httptest.NewRequest("PUT", "/api/runner-jobs/1/artifacts?epoch=1", bytes.NewReader(bigBody))
	req.Header.Set("Authorization", "Bearer tok")
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "64 bytes") {
		t.Fatalf("expected error body to name limit 64 bytes: %s", w.Body.String())
	}
}

func TestHandleRunnerJobArtifacts_StoreErrors(t *testing.T) {
	job := defaultArtifactJob()

	// GetRemoteJob error
	remoteJobsErr := &mocks.MockRemoteJobStore{
		GetRemoteJobErr: fmt.Errorf("db error"),
	}
	srv1 := setupRunnerJobServer(t, &mocks.MockRunnerStore{}, remoteJobsErr, nil)
	w1 := httptest.NewRecorder()
	srv1.handleRunnerJobArtifacts(w1, artifactReq([]byte("data"), "1", "epoch=1", 1))
	if w1.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 on GetRemoteJob error", w1.Code)
	}

	// PutRemoteArtifact error
	remoteJobsPutErr := &mocks.MockRemoteJobStore{
		Jobs:           map[int]*domain.RemoteJob{1: job},
		PutArtifactErr: fmt.Errorf("disk full"),
	}
	srv2 := setupRunnerJobServer(t, &mocks.MockRunnerStore{}, remoteJobsPutErr, nil)
	body := buildTestArtifactArchive(t, map[string]string{"app/dist/ok.txt": "ok"})
	w2 := httptest.NewRecorder()
	srv2.handleRunnerJobArtifacts(w2, artifactReq(body, "1", "epoch=1", 1))
	if w2.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 on PutRemoteArtifact error", w2.Code)
	}
}

func TestHandleRunnerJobArtifacts_EscapingEntryAndCorruptTar(t *testing.T) {
	runnerID := 1
	job := &domain.RemoteJob{
		ID:         1,
		RunID:      10,
		TaskName:   "root-task",
		Status:     "running",
		RunnerID:   &runnerID,
		ClaimEpoch: 1,
		Payload: domain.RemoteJobPayload{
			Task: domain.Task{
				Name:      "root-task",
				Artifacts: []string{"."},
				Execute:   domain.ExecuteSpec{Repo: "root"},
			},
			Repos: []domain.Repo{{Name: "root", Path: "."}},
		},
	}
	remoteJobs := &mocks.MockRemoteJobStore{
		Jobs: map[int]*domain.RemoteJob{1: job},
	}
	srv := setupRunnerJobServer(t, &mocks.MockRunnerStore{}, remoteJobs, nil)

	// Escaping entry with ..
	bodyEscape := buildTestArtifactArchive(t, map[string]string{
		"../escape.txt": "leak",
	})
	w1 := httptest.NewRecorder()
	srv.handleRunnerJobArtifacts(w1, artifactReq(bodyEscape, "1", "epoch=1", 1))
	if w1.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for escaping entry", w1.Code)
	}

	// Corrupt tar (valid gzip header, but invalid tar content)
	var corruptBuf bytes.Buffer
	gw := gzip.NewWriter(&corruptBuf)
	gw.Write([]byte("not a valid tar header at all"))
	gw.Close()

	w2 := httptest.NewRecorder()
	srv.handleRunnerJobArtifacts(w2, artifactReq(corruptBuf.Bytes(), "1", "epoch=1", 1))
	if w2.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for corrupt tar", w2.Code)
	}

	// Success with "." artifact (allowedPrefixes "")
	bodyRoot := buildTestArtifactArchive(t, map[string]string{
		"dist/bundle.js": "ok",
	})
	w3 := httptest.NewRecorder()
	srv.handleRunnerJobArtifacts(w3, artifactReq(bodyRoot, "1", "epoch=1", 1))
	if w3.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 for root task: %s", w3.Code, w3.Body.String())
	}
}

func TestHandleRunnerJobArtifacts_BodyReadError(t *testing.T) {
	job := defaultArtifactJob()
	remoteJobs := &mocks.MockRemoteJobStore{
		Jobs: map[int]*domain.RemoteJob{1: job},
	}
	srv := setupRunnerJobServer(t, &mocks.MockRunnerStore{}, remoteJobs, nil)

	req := httptest.NewRequest("PUT", "/api/runner-jobs/1/artifacts?epoch=1", errReader{})
	req = runCtx(req, "id", "1")
	req = req.WithContext(contextWithRunnerID(req.Context(), 1))

	w := httptest.NewRecorder()
	srv.handleRunnerJobArtifacts(w, req)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "read artifact body") {
		t.Fatalf("status = %d, want 400 read artifact body: %s", w.Code, w.Body.String())
	}
}

func TestServer_RunnerArtifactMaxBytes_Wiring(t *testing.T) {
	srvDefault := NewServer(Config{})
	if srvDefault.runnerArtifactMaxBytes() != defaultRunnerArtifactMaxBytes {
		t.Errorf("default runnerArtifactMaxBytes = %d, want %d", srvDefault.runnerArtifactMaxBytes(), defaultRunnerArtifactMaxBytes)
	}

	srvConfigured := NewServer(Config{RunnerArtifactMaxBytes: 12345})
	if srvConfigured.runnerArtifactMaxBytes() != 12345 {
		t.Errorf("configured runnerArtifactMaxBytes = %d, want 12345", srvConfigured.runnerArtifactMaxBytes())
	}
}

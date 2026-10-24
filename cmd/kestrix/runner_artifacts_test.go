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
	"strings"
	"sync"
	"testing"

	"kestrix/internal/domain"
)

func TestRunJob_UploadsArtifactsOnPassedTask(t *testing.T) {
	var mu sync.Mutex
	var uploadedArtifacts []byte
	var completeCall map[string]any

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if strings.HasSuffix(r.URL.Path, "/artifacts") {
			if r.Method != http.MethodPut {
				t.Errorf("method = %s, want PUT", r.Method)
			}
			if r.URL.Query().Get("epoch") != "1" {
				t.Errorf("epoch = %q, want 1", r.URL.Query().Get("epoch"))
			}
			uploadedArtifacts, _ = io.ReadAll(r.Body)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/complete") {
			_ = json.NewDecoder(r.Body).Decode(&completeCall)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer ts.Close()

	job := &domain.RemoteJob{
		ID:         42,
		ClaimEpoch: 1,
		Payload: domain.RemoteJobPayload{
			Task: domain.Task{
				Name:      "build",
				Artifacts: []string{"dist/"},
				Execute: domain.ExecuteSpec{
					Repo:   "app",
					Script: "mkdir -p dist && echo 'artifact-data' > dist/output.txt",
				},
			},
			Repos:   []domain.Repo{{Name: "app", Path: "app"}},
			Archive: buildTestArchive(t, map[string]string{"app/.keep": ""}),
		},
	}

	runJob(context.Background(), ts.Client(), ts.URL, "tok", "local", job)

	mu.Lock()
	defer mu.Unlock()

	if completeCall["status"] != "passed" {
		t.Fatalf("expected status passed, got %v (err=%v)", completeCall["status"], completeCall["error"])
	}

	if len(uploadedArtifacts) == 0 {
		t.Fatal("expected uploaded artifacts, got none")
	}

	// Verify tar.gz contents
	gz, err := gzip.NewReader(bytes.NewReader(uploadedArtifacts))
	if err != nil {
		t.Fatalf("open gzip: %v", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	foundFile := false
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read tar: %v", err)
		}
		if hdr.Name == "app/dist/output.txt" {
			foundFile = true
			content, _ := io.ReadAll(tr)
			if !strings.Contains(string(content), "artifact-data") {
				t.Fatalf("unexpected content: %q", string(content))
			}
		}
	}
	if !foundFile {
		t.Fatal("expected app/dist/output.txt in uploaded archive")
	}
}

func TestRunJob_ArtifactUpload413_FailsTask(t *testing.T) {
	var mu sync.Mutex
	var completeCall map[string]any

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if strings.HasSuffix(r.URL.Path, "/artifacts") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			json.NewEncoder(w).Encode(map[string]string{
				"error": "artifact archive exceeds limit of 52428800 bytes",
			})
			return
		}
		if strings.HasSuffix(r.URL.Path, "/complete") {
			_ = json.NewDecoder(r.Body).Decode(&completeCall)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer ts.Close()

	job := &domain.RemoteJob{
		ID:         42,
		ClaimEpoch: 1,
		Payload: domain.RemoteJobPayload{
			Task: domain.Task{
				Name:      "build",
				Artifacts: []string{"dist/"},
				Execute: domain.ExecuteSpec{
					Repo:   "app",
					Script: "mkdir -p dist && echo 'data' > dist/output.txt",
				},
			},
			Repos:   []domain.Repo{{Name: "app", Path: "app"}},
			Archive: buildTestArchive(t, map[string]string{"app/.keep": ""}),
		},
	}

	runJob(context.Background(), ts.Client(), ts.URL, "tok", "local", job)

	mu.Lock()
	defer mu.Unlock()

	if completeCall["status"] != "failed" {
		t.Fatalf("expected status failed, got %v", completeCall["status"])
	}
	errMsg, _ := completeCall["error"].(string)
	if !strings.Contains(errMsg, "exceeds limit of 52428800 bytes") {
		t.Fatalf("expected error naming limit, got %q", errMsg)
	}
}

func TestRunJob_ArtifactUploadFailed_FailsTask(t *testing.T) {
	var mu sync.Mutex
	var completeCall map[string]any

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if strings.HasSuffix(r.URL.Path, "/artifacts") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{
				"error": "storage backend unavailable",
			})
			return
		}
		if strings.HasSuffix(r.URL.Path, "/complete") {
			_ = json.NewDecoder(r.Body).Decode(&completeCall)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer ts.Close()

	job := &domain.RemoteJob{
		ID:         42,
		ClaimEpoch: 1,
		Payload: domain.RemoteJobPayload{
			Task: domain.Task{
				Name:      "build",
				Artifacts: []string{"dist/"},
				Execute: domain.ExecuteSpec{
					Repo:   "app",
					Script: "mkdir -p dist && echo 'data' > dist/output.txt",
				},
			},
			Repos:   []domain.Repo{{Name: "app", Path: "app"}},
			Archive: buildTestArchive(t, map[string]string{"app/.keep": ""}),
		},
	}

	runJob(context.Background(), ts.Client(), ts.URL, "tok", "local", job)

	mu.Lock()
	defer mu.Unlock()

	if completeCall["status"] != "failed" {
		t.Fatalf("expected status failed, got %v", completeCall["status"])
	}
	errMsg, _ := completeCall["error"].(string)
	if !strings.Contains(errMsg, "storage backend unavailable") {
		t.Fatalf("expected error naming reason, got %q", errMsg)
	}
}

func TestRunJob_UnpacksArtifactsInOrder_DiamondOverwrite(t *testing.T) {
	ts, _, complete := recordingRunnerServer(t)

	// Source archive has out/x with "initial"
	sourceArch := buildTestArchive(t, map[string]string{
		"app/out/x": "initial",
	})

	// Artifact from 'a' has out/x with "from-a"
	artA := buildTestArchive(t, map[string]string{
		"app/out/x": "from-a",
	})

	// Artifact from 'b' has out/x with "from-b"
	artB := buildTestArchive(t, map[string]string{
		"app/out/x": "from-b",
	})

	job := &domain.RemoteJob{
		ID:         1,
		ClaimEpoch: 1,
		Payload: domain.RemoteJobPayload{
			Task: domain.Task{
				Name: "c",
				Execute: domain.ExecuteSpec{
					Repo:   "app",
					Script: `test "$(cat out/x)" = "from-b"`,
				},
			},
			Repos:     []domain.Repo{{Name: "app", Path: "app"}},
			Archive:   sourceArch,
			Artifacts: [][]byte{artA, artB}, // D4 order: a unpacked first, b overwrites a
		},
	}

	runJob(context.Background(), ts.Client(), ts.URL, "tok", "local", job)

	if (*complete)["status"] != "passed" {
		t.Fatalf("task c failed: %v", (*complete)["error"])
	}
}

func TestRunJob_UnpackArtifactFailure_FailsTask(t *testing.T) {
	ts, _, complete := recordingRunnerServer(t)

	sourceArch := buildTestArchive(t, map[string]string{
		"app/main.go": "package main",
	})

	// Corrupt artifact
	corruptArt := []byte("not-gzip-at-all")

	job := &domain.RemoteJob{
		ID:         1,
		ClaimEpoch: 1,
		Payload: domain.RemoteJobPayload{
			Task: domain.Task{
				Name:    "deploy",
				Execute: domain.ExecuteSpec{Repo: "app", Script: "true"},
			},
			Repos:     []domain.Repo{{Name: "app", Path: "app"}},
			Archive:   sourceArch,
			Artifacts: [][]byte{corruptArt},
		},
	}

	runJob(context.Background(), ts.Client(), ts.URL, "tok", "local", job)

	if (*complete)["status"] != "failed" {
		t.Fatalf("expected failed, got %v", (*complete)["status"])
	}
	msg, _ := (*complete)["error"].(string)
	if !strings.Contains(msg, "unpack artifact") {
		t.Fatalf("expected error mentioning unpack artifact, got %q", msg)
	}
}

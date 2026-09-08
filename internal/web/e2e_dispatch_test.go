package web

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

const testPipeline = `
project "test-app" {
  ci_repo "auth_service" {
    path = "auth_service"
  }

  ci_task "build_auth" {
    trigger = "repo.auth_service.changed"
    repo    = "auth_service"
    script  = "go build ./..."
  }
}
`

func TestWebhookDispatchIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	// 1. Mock GitLab — captures pipeline creation.
	var capturedRef string
	mockGitLab := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v4/projects/12345/pipeline" && r.Method == "POST" {
			var body map[string]interface{}
			json.NewDecoder(r.Body).Decode(&body)
			capturedRef = body["ref"].(string)
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"id":      1,
				"iid":     1,
				"status":  "created",
				"ref":     body["ref"],
				"web_url": "https://gitlab.com/project/-/pipelines/1",
			})
			return
		}
		// Accept pipeline variables endpoint.
		if r.URL.Path == "/api/v4/projects/12345/pipelines/1/variables" && r.Method == "POST" {
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]interface{}{})
			return
		}
		http.NotFound(w, r)
	}))
	defer mockGitLab.Close()

	// 2. Temp project.
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "pipeline.tf"), []byte(testPipeline), 0o644)

	// 3. Server with dispatch.
	server := NewServer(Config{
		ListenAddr:  ":0",
		PipelineDir: tmpDir,
		StorePath:   filepath.Join(tmpDir, "projects.json"),
		Signature:   SignatureConfig{Secret: "tok"},
		Dispatch: DispatchConfig{
			Enabled:     true,
			ProjectID:   "12345",
			GitLabURL:   mockGitLab.URL,
			GitLabToken: "glpat-x",
		},
	})

	// 4. Send webhook.
	body := `{"object_kind":"push","ref":"refs/heads/main","commits":[{"id":"a","message":"feat:auth","added":[],"modified":["auth_service/main.go"],"removed":[]}],"repository":{"name":"test-app"},"changes":["auth_service/main.go"]}`
	req := httptest.NewRequest("POST", "/webhook", io.NopCloser(bytes.NewBufferString(body)))
	req.Header.Set("X-Gitlab-Token", "tok")
	rec := httptest.NewRecorder()
	server.router.ServeHTTP(rec, req)

	// 5. Verify.
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&resp)

	if resp["status"] != "ok" {
		t.Errorf("status=%v", resp["status"])
	}
	tasks := resp["tasks"].([]interface{})
	if len(tasks) != 1 || tasks[0] != "build_auth" {
		t.Errorf("tasks=%v, want [build_auth]", tasks)
	}
	if resp["dispatched"] != true {
		t.Errorf("dispatched=%v, want true", resp["dispatched"])
	}
	if capturedRef != "main" {
		t.Errorf("GitLab received ref=%q, want main", capturedRef)
	}
}

// TestWebhookDispatchExcludesSecretVariables pins runtime-secrets spec.md
// "Dispatch never carries a secret": a definition declaring a secret
// variable still dispatches, and the GitLab pipeline variables it receives
// carry no key or value belonging to that secret variable.
func TestWebhookDispatchExcludesSecretVariables(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	var capturedKeys []string
	mockGitLab := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v4/projects/12345/pipeline" && r.Method == "POST" {
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"id": 1, "iid": 1, "status": "created", "ref": "main",
				"web_url": "https://gitlab.com/project/-/pipelines/1",
			})
			return
		}
		if r.URL.Path == "/api/v4/projects/12345/pipelines/1/variables" && r.Method == "POST" {
			var body map[string]string
			json.NewDecoder(r.Body).Decode(&body)
			capturedKeys = append(capturedKeys, body["key"])
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]interface{}{})
			return
		}
		http.NotFound(w, r)
	}))
	defer mockGitLab.Close()

	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "pipeline.tf"), []byte(testPipeline), 0o644)
	secretVariable := `
project "test-app" {
  variable "REGISTRY_PASSWORD" {
    secret_ref = "cred_registry"
    scope      = "project"
  }
}
`
	os.WriteFile(filepath.Join(tmpDir, "secrets.tf"), []byte(secretVariable), 0o644)

	server := NewServer(Config{
		ListenAddr:  ":0",
		PipelineDir: tmpDir,
		StorePath:   filepath.Join(tmpDir, "projects.json"),
		Signature:   SignatureConfig{Secret: "tok"},
		Dispatch: DispatchConfig{
			Enabled:     true,
			ProjectID:   "12345",
			GitLabURL:   mockGitLab.URL,
			GitLabToken: "glpat-x",
		},
	})

	body := `{"object_kind":"push","ref":"refs/heads/main","commits":[{"id":"a","message":"feat:auth","added":[],"modified":["auth_service/main.go"],"removed":[]}],"repository":{"name":"test-app"},"changes":["auth_service/main.go"]}`
	req := httptest.NewRequest("POST", "/webhook", io.NopCloser(bytes.NewBufferString(body)))
	req.Header.Set("X-Gitlab-Token", "tok")
	rec := httptest.NewRecorder()
	server.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp["dispatched"] != true {
		t.Fatalf("dispatched=%v, want true", resp["dispatched"])
	}
	if len(capturedKeys) == 0 {
		t.Fatal("expected dispatch to set at least the KESTRIX_* metadata variables")
	}
	for _, key := range capturedKeys {
		if key == "REGISTRY_PASSWORD" {
			t.Fatalf("dispatched variables leaked the secret variable key: %v", capturedKeys)
		}
	}
}

func TestWebhookNoDispatch(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "pipeline.tf"), []byte(testPipeline), 0o644)

	server := NewServer(Config{
		PipelineDir: tmpDir,
		StorePath:   filepath.Join(tmpDir, "projects.json"),
		Signature:   SignatureConfig{Secret: "tok"},
	})
	body := `{"object_kind":"push","ref":"refs/heads/main","commits":[{"id":"a","message":"x","added":[],"modified":["auth_service/main.go"],"removed":[]}],"repository":{"name":"test-app"},"changes":["auth_service/main.go"]}`
	req := httptest.NewRequest("POST", "/webhook", io.NopCloser(bytes.NewBufferString(body)))
	req.Header.Set("X-Gitlab-Token", "tok")
	rec := httptest.NewRecorder()
	server.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
	var resp map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp["dispatched"] != false {
		t.Errorf("dispatched=%v, want false", resp["dispatched"])
	}
}

func TestWebhookWrongSecret(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "pipeline.tf"), []byte(testPipeline), 0o644)

	server := NewServer(Config{
		PipelineDir: tmpDir,
		StorePath:   filepath.Join(tmpDir, "projects.json"),
		Signature:   SignatureConfig{Secret: "correct"},
	})
	body := `{"ref":"refs/heads/main","commits":[],"repository":{"name":"x"}}`
	req := httptest.NewRequest("POST", "/webhook", io.NopCloser(bytes.NewBufferString(body)))
	req.Header.Set("X-Gitlab-Token", "wrong")
	rec := httptest.NewRecorder()
	server.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status=%d, want 401", rec.Code)
	}
}

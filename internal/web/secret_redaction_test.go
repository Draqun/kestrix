package web

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"kestrix/internal/domain"
	"kestrix/internal/mocks"
)

// writeSecretPipeline writes a minimal pipeline.tf declaring one secret
// variable, so a test can drive handleRunProject through BuildVarStore's
// secret resolution.
func writeSecretPipeline(t *testing.T, dir string) {
	t.Helper()
	pipeline := `project "demo" {
  ci_repo "main" {
    path = "."
  }
  ci_task "deploy" {
    trigger = "repo.main.changed"
    execute {
      repo = "main"
      script = "echo deploying"
    }
  }
  variable "REGISTRY_PASSWORD" {
    secret_ref = "cred_registry"
    scope      = "project"
  }
}`
	if err := os.WriteFile(filepath.Join(dir, "pipeline.tf"), []byte(pipeline), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestSecretResolution_NeverWritesValueToServerLog pins runtime-secrets
// spec.md "Secret is not written to the log": across a run whose secret
// resolves successfully and a run whose secret is revoked, no server log
// record contains the resolved value.
func TestSecretResolution_NeverWritesValueToServerLog(t *testing.T) {
	const secretValue = "super-secret-runtime-value"
	tmp := t.TempDir()
	writeSecretPipeline(t, tmp)

	projCfg := &domain.ProjectConfig{
		Name: "demo", RepoURL: "file://" + tmp, RepoRef: "main",
		PipelineDir: ".", PipelineFile: "pipeline.tf",
	}

	t.Run("valid secret", func(t *testing.T) {
		mockRun := &mocks.MockRunStore{CreateRunID: 1, FinishRunCh: make(chan struct{})}
		mockProj := &mocks.MockProjectStore{GetResult: projCfg}
		srv := setupRunServer(t, mockProj, mockRun)
		srv.sourceSessions = &testSourceSessions{root: tmp}
		srv.credentialStore = &fakeCredentialStore{
			items:         map[string]domain.CredentialMetadata{},
			resolveResult: domain.Credential{Type: "secret", Value: secretValue},
		}

		buf := captureLog(t)
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/projects/demo/run", strings.NewReader(`{"changed":["main"]}`))
		srv.handleRunProject(w, runCtx(req, "name", "demo"))
		<-mockRun.FinishRunCh

		if w.Code != 200 {
			t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
		}
		if strings.Contains(buf.String(), secretValue) {
			t.Fatalf("server log leaked the secret value: %s", buf.String())
		}
	})

	t.Run("revoked secret", func(t *testing.T) {
		mockRun := &mocks.MockRunStore{CreateRunID: 2}
		mockProj := &mocks.MockProjectStore{GetResult: projCfg}
		srv := setupRunServer(t, mockProj, mockRun)
		srv.sourceSessions = &testSourceSessions{root: tmp}
		srv.credentialStore = &fakeCredentialStore{
			items:      map[string]domain.CredentialMetadata{},
			resolveErr: fmt.Errorf("%w: cred_registry", domain.ErrCredentialRevoked),
		}

		buf := captureLog(t)
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/projects/demo/run", strings.NewReader(`{"changed":["main"]}`))
		srv.handleRunProject(w, runCtx(req, "name", "demo"))

		if w.Code != 400 {
			t.Fatalf("status = %d, want 400, body=%s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		if err := json.NewDecoder(w.Body).Decode(&resp); err == nil {
			if strings.Contains(fmt.Sprint(resp["error"]), secretValue) {
				t.Fatalf("error response leaked the secret value: %v", resp["error"])
			}
		}
		if strings.Contains(buf.String(), secretValue) {
			t.Fatalf("server log leaked the secret value: %s", buf.String())
		}
	})
}

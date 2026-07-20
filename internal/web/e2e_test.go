package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"gitlab.com/lunaris-bot/metaci/internal/parser"
)

func TestFullWebhookFlow(t *testing.T) {
	// 1. Create a temp pipeline directory with a real .tf file.
	tmpDir := t.TempDir()
	pipeline := `
project "e2e-test" {
  default_image = "golang:1.22"

  ci_repo "auth" {
    path = "src/auth"
    kind = "subpath"
  }
  ci_repo "docs" {
    path = "docs"
  }

  ci_task "build" {
    trigger = "repo.auth.changed"
    execute {
      repo   = "auth"
      script = "./build.sh"
    }
  }
}
`
	os.WriteFile(filepath.Join(tmpDir, "pipeline.tf"), []byte(pipeline), 0644)

	// 2. Verify parser can read it.
	proj, err := parser.ParseDir(tmpDir)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if proj.Name != "e2e-test" {
		t.Fatalf("name = %q, want e2e-test", proj.Name)
	}

	// 3. Create server pointing at the temp dir.
	srv := NewServer(Config{
		ListenAddr:  ":0",
		PipelineDir: tmpDir,
	})

	// 4. Send a webhook for auth (code change).
	payload := `{
		"object_kind": "push",
		"ref": "refs/heads/main",
		"project": {
			"path_with_namespace": "src/auth",
			"default_branch": "main"
		},
		"commits": [{
			"id": "abc",
			"message": "fix",
			"added": [],
			"modified": ["src/auth/handler.go"],
			"removed": []
		}]
	}`

	req := httptest.NewRequest("POST", "/webhook", bytes.NewBufferString(payload))
	w := httptest.NewRecorder()
	srv.handleWebhook(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("webhook status = %d, want 200: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)

	if resp["status"] != "ok" {
		t.Errorf("status = %v, want ok", resp["status"])
	}
	if resp["project"] != "e2e-test" {
		t.Errorf("project = %v, want e2e-test", resp["project"])
	}

	tasks, ok := resp["tasks"].([]interface{})
	if !ok || len(tasks) == 0 {
		t.Fatalf("expected triggered tasks, got %v", resp["tasks"])
	}

	// Build task should be in the triggered list.
	found := false
	for _, task := range tasks {
		if task.(string) == "build" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("build task not in triggered list: %v", tasks)
	}
}

func TestWebhookDocsOnlySkips(t *testing.T) {
	tmpDir := t.TempDir()
	pipeline := `
project "p" {
  ci_repo "docs" {
    path = "docs"
  }
  ci_task "build" {
    trigger = "repo.docs.changed"
    execute {
      repo   = "docs"
      script = "./build.sh"
    }
  }
}
`
	os.WriteFile(filepath.Join(tmpDir, "pipeline.tf"), []byte(pipeline), 0644)

	srv := NewServer(Config{
		ListenAddr:  ":0",
		PipelineDir: tmpDir,
	})

	payload := `{
		"object_kind": "push",
		"ref": "refs/heads/main",
		"project": {
			"path_with_namespace": "docs",
			"default_branch": "main"
		},
		"commits": [{
			"id": "abc",
			"message": "update readme",
			"added": [],
			"modified": ["README.md"],
			"removed": []
		}]
	}`

	req := httptest.NewRequest("POST", "/webhook", bytes.NewBufferString(payload))
	w := httptest.NewRecorder()
	srv.handleWebhook(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)

	// README.md is not code → should skip.
	tasks, ok := resp["tasks"].([]interface{})
	if !ok {
		tasks = []interface{}{}
	}
	if len(tasks) != 0 {
		t.Errorf("docs-only push should trigger 0 tasks, got %d", len(tasks))
	}
}

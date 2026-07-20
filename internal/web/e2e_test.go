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
	"gitlab.com/lunaris-bot/metaci/internal/store"
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
		StorePath:   filepath.Join(tmpDir, "projects.json"),
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
		StorePath:   filepath.Join(tmpDir, "projects.json"),
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

func TestProjectLifecycle(t *testing.T) {
	repoDir := t.TempDir()

	srv := NewServer(Config{
		ListenAddr:  ":0",
		PipelineDir: "./examples/demo-mono",
		StorePath:   filepath.Join(t.TempDir(), "projects.json"),
	})

	// 1. Create project pointing at repoDir.
	createBody := `{"name":"tf-demo","repo_path":"` + repoDir + `"}`
	req := httptest.NewRequest("POST", "/api/projects", bytes.NewBufferString(createBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201: %s", w.Code, w.Body.String())
	}

	// 2. Save pipeline.tf (multi-line HCL).
	pipeline := `
project "tf-demo" {
  ci_repo "tf_plugin" {
    path = "tf-plugin"
    kind = "subpath"
  }
  ci_repo "backend" {
    path = "backend"
    kind = "subpath"
  }
  ci_task "build_tf_plugin" {
    trigger = "repo.tf_plugin.changed"
    execute {
      repo   = "tf_plugin"
      script = "make build"
    }
  }
  ci_task "build_backend" {
    trigger = "repo.backend.changed"
    execute {
      repo   = "backend"
      script = "make build"
    }
  }
}
`
	req = httptest.NewRequest("POST", "/api/projects/tf-demo/pipeline", bytes.NewBufferString(pipeline))
	req.Header.Set("Content-Type", "text/plain")
	w = httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("save status = %d, want 200: %s", w.Code, w.Body.String())
	}

	// File should exist on disk.
	if _, err := os.Stat(filepath.Join(repoDir, "pipeline.tf")); err != nil {
		t.Fatalf("pipeline.tf not written: %v", err)
	}

	// 3. Resolve with tf_plugin changed.
	resolveBody := `{"changed":["tf_plugin"]}`
	req = httptest.NewRequest("POST", "/api/projects/tf-demo/resolve", bytes.NewBufferString(resolveBody))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("resolve status = %d, want 200: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Triggered []struct {
			Name string `json:"name"`
		} `json:"triggered"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Triggered) != 1 || resp.Triggered[0].Name != "build_tf_plugin" {
		t.Fatalf("expected [build_tf_plugin], got %+v", resp.Triggered)
	}
}

func TestRunProject(t *testing.T) {
	repoDir := t.TempDir()
	pipelineDir := filepath.Join(repoDir, ".ci")
	if err := os.MkdirAll(pipelineDir, 0o755); err != nil {
		t.Fatal(err)
	}
	pipeline := `project "run-demo" {
  ci_repo "app" {
    path = "."
    kind = "subpath"
  }
  ci_task "check" {
    trigger = "repo.app.changed"
    execute {
      repo = "app"
      script = "printf passed > result.txt"
    }
  }
}`
	if err := os.WriteFile(filepath.Join(pipelineDir, "pipeline.tf"), []byte(pipeline), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "unrelated.tf"), []byte("this is not a pipeline"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := NewServer(Config{
		ListenAddr: ":0",
		StorePath:  filepath.Join(t.TempDir(), "projects.json"),
	})
	if err := srv.store.Upsert(&store.ProjectConfig{Name: "run-demo", RepoPath: repoDir, PipelineDir: ".ci", PipelineFile: "pipeline.tf"}); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "/api/projects/run-demo/run", bytes.NewBufferString(`{"changed":["app"]}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("run status = %d: %s", w.Code, w.Body.String())
	}
	var response struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Status != "passed" {
		t.Fatalf("run status = %q, body: %s", response.Status, w.Body.String())
	}
	got, err := os.ReadFile(filepath.Join(repoDir, "result.txt"))
	if err != nil || string(got) != "passed" {
		t.Fatalf("task output = %q, err = %v", got, err)
	}
}

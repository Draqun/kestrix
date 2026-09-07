package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"kestrix/internal/domain"
	"kestrix/internal/mocks"
	"kestrix/internal/parser"
	"kestrix/internal/source"
)

func gitE2E(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
	return string(bytes.TrimSpace(output))
}

func remoteWorkflowPipeline(marker string) string {
	return `project "remote-e2e" {
  ci_repo "app" {
    path = "."
  }
  ci_task "verify" {
    trigger = "repo.app.changed"
    execute {
      repo = "app"
      script = "test -f ` + marker + `"
    }
  }
}`
}

func cacheEntryCount(t *testing.T, root string) int {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, entry := range entries {
		if entry.IsDir() && len(entry.Name()) == 64 {
			count++
		}
	}
	return count
}

func TestRemoteProjectWorkflowE2E(t *testing.T) {
	root := t.TempDir()
	bare := filepath.Join(root, "remote.git")
	work := filepath.Join(root, "work")
	if err := os.MkdirAll(work, 0o700); err != nil {
		t.Fatal(err)
	}
	gitE2E(t, root, "init", "--quiet", "--bare", "--initial-branch=main", bare)
	gitE2E(t, work, "init", "--quiet", "--initial-branch=main")
	gitE2E(t, work, "remote", "add", "origin", bare)
	if err := os.MkdirAll(filepath.Join(work, "ci"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "ci", "pipeline.tf"), []byte(remoteWorkflowPipeline("version-1")), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "version-1"), []byte("one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitE2E(t, work, "add", ".")
	gitE2E(t, work, "-c", "user.name=Kestrix E2E", "-c", "user.email=e2e@example.test", "commit", "--quiet", "-m", "version one")
	gitE2E(t, work, "push", "--quiet", "-u", "origin", "main")

	srv := NewServer(Config{ListenAddr: ":0", StorePath: filepath.Join(root, "projects.json")})
	cacheRoot := filepath.Join(root, "cache")
	cache, err := source.NewGitCache(source.CacheOptions{Root: cacheRoot, MaxAge: time.Hour, MaxBytes: 1 << 30})
	if err != nil {
		t.Fatal(err)
	}
	srv.sourceSessions = cache
	runStore := srv.runStore.(*mocks.MockRunStore)
	runStore.FinishRunCh = make(chan struct{})

	createBody, _ := json.Marshal(map[string]interface{}{
		"name": "remote-e2e", "repo_url": bare, "repo_ref": "main",
		"pipeline_dir": "ci", "pipeline_file": "pipeline.tf",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/projects", bytes.NewReader(createBody))
	req.Header.Set("Content-Type", "application/json")
	srv.router.ServeHTTP(w, authed(t, srv, req))
	if w.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", w.Code, w.Body.String())
	}
	if got := cacheEntryCount(t, cacheRoot); got != 1 {
		t.Fatalf("cache entries after create=%d, want 1", got)
	}

	if err := os.WriteFile(filepath.Join(work, "ci", "pipeline.tf"), []byte(remoteWorkflowPipeline("version-2")), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "version-2"), []byte("two\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(work, "version-1")); err != nil {
		t.Fatal(err)
	}
	gitE2E(t, work, "add", "-A")
	gitE2E(t, work, "-c", "user.name=Kestrix E2E", "-c", "user.email=e2e@example.test", "commit", "--quiet", "-m", "version two")
	secondSHA := gitE2E(t, work, "rev-parse", "HEAD")
	gitE2E(t, work, "push", "--quiet", "origin", "main")

	w = httptest.NewRecorder()
	srv.router.ServeHTTP(w, authed(t, srv, httptest.NewRequest(http.MethodGet, "/api/projects/remote-e2e/definition", nil)))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "version-2") {
		t.Fatalf("updated definition status=%d body=%s", w.Code, w.Body.String())
	}
	if got := cacheEntryCount(t, cacheRoot); got != 1 {
		t.Fatalf("branch refresh created %d cache entries, want reuse of 1", got)
	}

	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/projects/remote-e2e/run", strings.NewReader(`{"changed":["app"]}`))
	req.Header.Set("Content-Type", "application/json")
	srv.router.ServeHTTP(w, authed(t, srv, req))
	if w.Code != http.StatusOK {
		t.Fatalf("run status=%d body=%s", w.Code, w.Body.String())
	}
	select {
	case <-runStore.FinishRunCh:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for remote pipeline run")
	}
	if len(runStore.FinishRunCalls) != 1 || runStore.FinishRunCalls[0].Status != "passed" {
		t.Fatalf("finish calls=%+v", runStore.FinishRunCalls)
	}
	if len(runStore.UpdateSourcesCalls) != 1 || len(runStore.UpdateSourcesCalls[0]) == 0 || runStore.UpdateSourcesCalls[0][0].ResolvedRevision != secondSHA {
		t.Fatalf("recorded sources=%+v, want primary SHA %s", runStore.UpdateSourcesCalls, secondSHA)
	}

	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/projects", strings.NewReader(`{"name":"local-rejected","repo_path":"/srv/repo","pipeline_dir":"ci"}`))
	req.Header.Set("Content-Type", "application/json")
	srv.router.ServeHTTP(w, authed(t, srv, req))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("local registration status=%d body=%s", w.Code, w.Body.String())
	}
}

// dockerAvailable mirrors the helper in internal/runner/runner_test.go and lets
// us skip end-to-end tests that depend on a working Docker daemon. Running the
// pipeline via the dispatcher requires Docker, so without it the test would
// block until the polling deadline instead of meaningfully exercising the
// endpoint.
func dockerAvailable() bool {
	if runtime.GOOS == "windows" {
		return false
	}
	if _, err := exec.LookPath("docker"); err != nil {
		return false
	}
	return exec.Command("docker", "info").Run() == nil
}

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
	if err := os.MkdirAll(filepath.Join(repoDir, "ci"), 0o755); err != nil {
		t.Fatal(err)
	}
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
	if err := os.WriteFile(filepath.Join(repoDir, "ci", "pipeline.tf"), []byte(pipeline), 0o644); err != nil {
		t.Fatal(err)
	}

	srv := NewServer(Config{
		ListenAddr:  ":0",
		PipelineDir: "./examples/demo-mono",
		StorePath:   filepath.Join(t.TempDir(), "projects.json"),
	})
	srv.sourceSessions = &testSourceSessions{root: repoDir}

	// 1. Create a remote-only project after server-side source validation.
	createBody := `{"name":"tf-demo","repo_url":"https://git.example.test/tf-demo.git","repo_ref":"main","pipeline_dir":"ci"}`
	req := httptest.NewRequest("POST", "/api/projects", bytes.NewBufferString(createBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, authed(t, srv, req))
	if w.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201: %s", w.Code, w.Body.String())
	}

	// 2. Registered pipeline content is Git-owned and read-only.
	req = httptest.NewRequest("POST", "/api/projects/tf-demo/pipeline", bytes.NewBufferString(pipeline))
	req.Header.Set("Content-Type", "text/plain")
	w = httptest.NewRecorder()
	srv.router.ServeHTTP(w, authed(t, srv, req))
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("save status = %d, want 405: %s", w.Code, w.Body.String())
	}

	// 3. Resolve with tf_plugin changed.
	resolveBody := `{"changed":["tf_plugin"]}`
	req = httptest.NewRequest("POST", "/api/projects/tf-demo/resolve", bytes.NewBufferString(resolveBody))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	srv.router.ServeHTTP(w, authed(t, srv, req))
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
	if testing.Short() {
		t.Skip("e2e test")
	}
	if !dockerAvailable() {
		t.Skip("docker not available")
	}
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
	srv.sourceSessions = &testSourceSessions{root: repoDir}
	if err := srv.projectStore.Upsert(&domain.ProjectConfig{Name: "run-demo", RepoURL: "https://git.example.test/run-demo.git", RepoRef: "main", PipelineDir: ".ci", PipelineFile: "pipeline.tf"}); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "/api/projects/run-demo/run", bytes.NewBufferString(`{"changed":["app"]}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, authed(t, srv, req))
	if w.Code != http.StatusOK {
		t.Fatalf("run status = %d: %s", w.Code, w.Body.String())
	}
	var initial struct {
		Status string `json:"status"`
		RunID  int    `json:"run_id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &initial); err != nil {
		t.Fatal(err)
	}
	if initial.Status != "running" {
		t.Fatalf("expected initial status 'running', got %q", initial.Status)
	}

	// Poll for completion.
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(200 * time.Millisecond)
		got, err := os.ReadFile(filepath.Join(repoDir, "result.txt"))
		if err == nil && string(got) == "passed" {
			break
		}
	}
	got, err := os.ReadFile(filepath.Join(repoDir, "result.txt"))
	if err != nil || string(got) != "passed" {
		t.Fatalf("task output = %q, err = %v", got, err)
	}
}

func TestResolveProjectManual(t *testing.T) {
	repoDir := t.TempDir()
	pipelineDir := filepath.Join(repoDir, "ci")
	if err := os.MkdirAll(pipelineDir, 0o755); err != nil {
		t.Fatal(err)
	}
	pipeline := `project "manual-demo" {
  ci_repo "app" {
    path = "."
    kind = "subpath"
  }
  ci_task "build" {
    trigger = "repo.app.changed"
    execute {
      repo = "app"
      script = "echo build"
    }
  }
  ci_task "deploy" {
    trigger = "repo.app.changed"
    manual = true
    depends_on = ["build"]
    execute {
      repo = "app"
      script = "echo deploy"
    }
  }
}`
	if err := os.WriteFile(filepath.Join(pipelineDir, "pipeline.tf"), []byte(pipeline), 0o644); err != nil {
		t.Fatal(err)
	}

	srv := NewServer(Config{
		ListenAddr: ":0",
		StorePath:  filepath.Join(t.TempDir(), "projects.json"),
	})
	srv.sourceSessions = &testSourceSessions{root: repoDir}
	if err := srv.projectStore.Upsert(&domain.ProjectConfig{Name: "manual-demo", RepoURL: "https://git.example.test/manual-demo.git", RepoRef: "main", PipelineDir: "ci", PipelineFile: "pipeline.tf"}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("POST", "/api/projects/manual-demo/resolve", bytes.NewBufferString(`{"changed":["app"]}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, authed(t, srv, req))
	if w.Code != http.StatusOK {
		t.Fatalf("resolve status = %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Triggered []struct {
			Name   string `json:"name"`
			Manual bool   `json:"manual"`
		} `json:"triggered"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Triggered) != 2 {
		t.Fatalf("expected 2 triggered tasks, got %+v", resp.Triggered)
	}

	manualFound := false
	for _, task := range resp.Triggered {
		if task.Name == "deploy" {
			manualFound = true
			if !task.Manual {
				t.Errorf("deploy task should be manual")
			}
		}
		if task.Name == "build" && task.Manual {
			t.Errorf("build task should not be manual")
		}
	}
	if !manualFound {
		t.Errorf("deploy task not found in triggered list")
	}
}

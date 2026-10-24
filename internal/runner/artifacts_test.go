package runner

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"kestrix/internal/model"
)

func TestLocalExecutor_MissingArtifactFailsTask(t *testing.T) {
	root := t.TempDir()
	repos := map[string]model.Repo{"app": {Name: "app", Path: ".", Kind: "subpath"}}
	local := &LocalExecutor{WorkDir: root, Repos: repos}

	task := model.Task{
		Name:      "build",
		Artifacts: []string{"dist/"},
		Execute: model.ExecuteSpec{
			Repo:   "app",
			Script: "echo 'built nothing'",
		},
	}

	opts := Options{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}
	_, err := local.RunTask(context.Background(), task, opts)
	if err == nil || !strings.Contains(err.Error(), `artifact "dist/" not found`) {
		t.Fatalf("expected error containing 'artifact \"dist/\" not found', got %v", err)
	}
}

func TestLocalExecutor_ExistingArtifactSucceeds(t *testing.T) {
	root := t.TempDir()
	repos := map[string]model.Repo{"app": {Name: "app", Path: ".", Kind: "subpath"}}
	local := &LocalExecutor{WorkDir: root, Repos: repos}

	task := model.Task{
		Name:      "build",
		Artifacts: []string{"dist/"},
		Execute: model.ExecuteSpec{
			Repo:   "app",
			Script: "mkdir dist && echo hello > dist/app.txt",
		},
	}

	opts := Options{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}
	_, err := local.RunTask(context.Background(), task, opts)
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
}

func TestDockerExecutor_MissingArtifactFailsTask(t *testing.T) {
	installFakeCommands(t, false)
	root := t.TempDir()
	repos := map[string]model.Repo{"app": {Name: "app", Path: ".", Kind: "subpath"}}
	docker := &DockerExecutor{WorkDir: root, Repos: repos}

	task := model.Task{
		Name:      "build",
		Artifacts: []string{"dist/app.txt"},
		Execute: model.ExecuteSpec{
			Repo:   "app",
			Script: "echo 'built nothing'",
		},
	}

	opts := Options{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}
	_, err := docker.RunTask(context.Background(), task, opts)
	if err == nil || !strings.Contains(err.Error(), `artifact "dist/app.txt" not found`) {
		t.Fatalf("expected error containing 'artifact \"dist/app.txt\" not found', got %v", err)
	}
}

func TestDockerExecutor_ExistingArtifactSucceeds(t *testing.T) {
	installFakeCommands(t, false)
	root := t.TempDir()
	repos := map[string]model.Repo{"app": {Name: "app", Path: ".", Kind: "subpath"}}
	docker := &DockerExecutor{WorkDir: root, Repos: repos}

	// Create artifact beforehand to simulate container creating it
	if err := os.MkdirAll(filepath.Join(root, "dist"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "dist", "app.txt"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}

	task := model.Task{
		Name:      "build",
		Artifacts: []string{"dist/app.txt"},
		Execute: model.ExecuteSpec{
			Repo:   "app",
			Script: "echo ok",
		},
	}

	opts := Options{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}
	_, err := docker.RunTask(context.Background(), task, opts)
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
}

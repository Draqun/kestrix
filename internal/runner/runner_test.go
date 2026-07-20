package runner

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"gitlab.com/lunaris-bot/metaci/internal/model"
)

func dockerAvailable() bool {
	_, err := exec.LookPath("docker")
	if err != nil {
		return false
	}
	cmd := exec.Command("docker", "info")
	return cmd.Run() == nil
}

func TestRunExecutesDependenciesInOrder(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("runner uses sh")
	}
	if !dockerAvailable() {
		t.Skip("docker not available")
	}
	dir := t.TempDir()
	project := model.Project{
		Name:  "local",
		Repos: []model.Repo{{Name: "repo", Path: ".", Kind: "subpath"}},
		Tasks: []model.Task{
			{Name: "first", Trigger: "repo.repo.changed", Execute: model.ExecuteSpec{Repo: "repo", Script: "printf first > /workspace/order"}},
			{Name: "second", Trigger: "repo.repo.changed", DependsOn: []string{"first"}, Execute: model.ExecuteSpec{Repo: "repo", Script: "printf second >> /workspace/order"}},
		},
	}
	var output bytes.Buffer
	_, err := Run(context.Background(), project, map[string]bool{"repo": true}, Options{WorkDir: dir, Stdout: &output, Stderr: &output})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "order"))
	if err != nil {
		t.Fatalf("read result: %v", err)
	}
	if string(got) != "firstsecond" {
		t.Errorf("execution order = %q, want firstsecond", string(got))
	}
}

func TestRunSkipsManualTasks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("runner uses sh")
	}
	if !dockerAvailable() {
		t.Skip("docker not available")
	}
	dir := t.TempDir()
	project := model.Project{
		Name:  "local",
		Repos: []model.Repo{{Name: "repo", Path: ".", Kind: "subpath"}},
		Tasks: []model.Task{{Name: "manual", Trigger: "repo.repo.changed", Manual: true, Execute: model.ExecuteSpec{Repo: "repo", Script: "touch /workspace/ran"}}},
	}
	var output bytes.Buffer
	_, err := Run(context.Background(), project, map[string]bool{"repo": true}, Options{WorkDir: dir, Stdout: &output})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "ran")); !os.IsNotExist(err) {
		t.Fatalf("manual task should not run, stat err = %v", err)
	}
}

func TestRunRejectsTaskWithUntriggeredDependency(t *testing.T) {
	if !dockerAvailable() {
		t.Skip("docker not available")
	}
	project := model.Project{
		Name:  "local",
		Repos: []model.Repo{{Name: "repo", Path: ".", Kind: "subpath"}},
		Tasks: []model.Task{
			{Name: "dependency", Trigger: "!repo.repo.changed", Execute: model.ExecuteSpec{Repo: "repo", Script: ":"}},
			{Name: "dependent", Trigger: "repo.repo.changed", DependsOn: []string{"dependency"}, Execute: model.ExecuteSpec{Repo: "repo", Script: ":"}},
		},
	}
	_, err := Run(context.Background(), project, map[string]bool{"repo": true}, Options{WorkDir: t.TempDir()})
	if err == nil {
		t.Fatal("Run should reject an untriggered dependency")
	}
}

func TestRunReturnsPerTaskResults(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("runner uses sh")
	}
	if !dockerAvailable() {
		t.Skip("docker not available")
	}
	dir := t.TempDir()
	project := model.Project{
		Name:  "local",
		Repos: []model.Repo{{Name: "repo", Path: ".", Kind: "subpath"}},
		Tasks: []model.Task{
			{Name: "pass", Trigger: "repo.repo.changed", Execute: model.ExecuteSpec{Repo: "repo", Script: "echo ok"}},
			{Name: "skip", Trigger: "repo.repo.changed", Manual: true, Execute: model.ExecuteSpec{Repo: "repo", Script: "echo skip"}},
		},
	}
	var output bytes.Buffer
	results, err := Run(context.Background(), project, map[string]bool{"repo": true}, Options{WorkDir: dir, Stdout: &output, Stderr: &output})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].Name != "pass" || results[0].Status != "passed" {
		t.Errorf("result[0] = %+v, want name=pass status=passed", results[0])
	}
	if results[1].Name != "skip" || results[1].Status != "manual" {
		t.Errorf("result[1] = %+v, want name=skip status=manual", results[1])
	}
}

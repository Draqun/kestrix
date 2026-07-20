package runner

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"gitlab.com/lunaris-bot/metaci/internal/model"
)

func TestRunExecutesDependenciesInOrder(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("runner uses sh")
	}
	dir := t.TempDir()
	project := model.Project{
		Name:  "local",
		Repos: []model.Repo{{Name: "repo", Path: ".", Kind: "subpath"}},
		Tasks: []model.Task{
			{Name: "first", Trigger: "repo.repo.changed", Execute: model.ExecuteSpec{Repo: "repo", Script: "printf first >> order"}},
			{Name: "second", Trigger: "repo.repo.changed", DependsOn: []string{"first"}, Execute: model.ExecuteSpec{Repo: "repo", Script: "printf second >> order"}},
		},
	}
	var output bytes.Buffer
	err := Run(context.Background(), project, map[string]bool{"repo": true}, Options{WorkDir: dir, Stdout: &output, Stderr: &output})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "order"))
	if err != nil {
		t.Fatalf("read result: %v", err)
	}
	if string(got) != "firstsecond" {
		t.Errorf("execution order = %q, want firstsecond", got)
	}
}

func TestRunSkipsManualTasks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("runner uses sh")
	}
	dir := t.TempDir()
	project := model.Project{
		Name:  "local",
		Repos: []model.Repo{{Name: "repo", Path: ".", Kind: "subpath"}},
		Tasks: []model.Task{{Name: "manual", Trigger: "repo.repo.changed", Manual: true, Execute: model.ExecuteSpec{Repo: "repo", Script: "touch ran"}}},
	}
	var output bytes.Buffer
	if err := Run(context.Background(), project, map[string]bool{"repo": true}, Options{WorkDir: dir, Stdout: &output}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "ran")); !os.IsNotExist(err) {
		t.Fatalf("manual task should not run, stat err = %v", err)
	}
}

func TestRunRejectsTaskWithUntriggeredDependency(t *testing.T) {
	project := model.Project{
		Name:  "local",
		Repos: []model.Repo{{Name: "repo", Path: ".", Kind: "subpath"}},
		Tasks: []model.Task{
			{Name: "dependency", Trigger: "!repo.repo.changed", Execute: model.ExecuteSpec{Repo: "repo", Script: ":"}},
			{Name: "dependent", Trigger: "repo.repo.changed", DependsOn: []string{"dependency"}, Execute: model.ExecuteSpec{Repo: "repo", Script: ":"}},
		},
	}
	err := Run(context.Background(), project, map[string]bool{"repo": true}, Options{WorkDir: t.TempDir()})
	if err == nil {
		t.Fatal("Run should reject an untriggered dependency")
	}
}

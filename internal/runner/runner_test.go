package runner

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"gitlab.com/lunaris-bot/metaci/internal/dag"
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

// skipOnWindows gates tests that call sh / bash.
func skipOnWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("runner uses sh")
	}
}

// simpleProject returns a project with one repo and one task — a basic
// build block for use-cases that don't need dependencies or manual
// approval.
func simpleProject(name, trigger, script string) model.Project {
	return model.Project{
		Name:  name,
		Repos: []model.Repo{{Name: "repo", Path: ".", Kind: "subpath"}},
		Tasks: []model.Task{
			{Name: "t", Trigger: trigger, Execute: model.ExecuteSpec{Repo: "repo", Script: script}},
		},
	}
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

// --- Pure helper coverage: order / assignStage / repoDir ------------------
//
// These helpers contain the structural logic of the runner and live
// below the docker-dependent paths. They are pure Go and can be
// exercised without external services.

func TestOrder_Topological(t *testing.T) {
	// a → b → c
	nodes := []dag.Node{
		{Task: model.Task{Name: "a", DependsOn: nil}},
		{Task: model.Task{Name: "b", DependsOn: []string{"a"}}},
		{Task: model.Task{Name: "c", DependsOn: []string{"b"}}},
	}
	got, err := order(nodes)
	if err != nil {
		t.Fatalf("order: %v", err)
	}
	names := make([]string, len(got))
	for i, n := range got {
		names[i] = n.Task.Name
	}
	want := []string{"a", "b", "c"}
	if fmt.Sprintf("%v", names) != fmt.Sprintf("%v", want) {
		t.Fatalf("order = %v, want %v", names, want)
	}
}

func TestOrder_IgnoresUntriggeredDependency(t *testing.T) {
	// 'a' references 'missing' which isn't in the trigger set
	// (i.e. wasn't a node). order() must report this clearly.
	nodes := []dag.Node{
		{Task: model.Task{Name: "a", DependsOn: []string{"missing"}}},
	}
	_, err := order(nodes)
	if err == nil {
		t.Fatal("expected error for untriggered dependency")
	}
	if !strings.Contains(err.Error(), "missing") {
		t.Fatalf("error should mention missing task: %v", err)
	}
}

func TestAssignStage(t *testing.T) {
	nodes := []dag.Node{
		{Task: model.Task{Name: "manual_task", Manual: true}},
		{Task: model.Task{Name: "tester_parent", DependsOn: []string{"previous"}}},
		{Task: model.Task{Name: "test_dep", DependsOn: []string{"unit_test"}}},
		{Task: model.Task{Name: "leaf"}},
	}
	cases := map[string]string{
		"manual_task":   "deploy",
		"test_dep":      "deploy", // depends on a name containing "test"
		"tester_parent": "test",   // depends but no 'test' substring
		"leaf":          "build",
	}
	for _, n := range nodes {
		got := assignStage(n.Task, nodes)
		if want, ok := cases[n.Task.Name]; ok {
			if got != want {
				t.Errorf("assignStage(%s) = %q, want %q", n.Task.Name, got, want)
			}
		}
	}
}

func TestRepoDir_GitRepo(t *testing.T) {
	repo := model.Repo{Name: "r", Kind: "git", Path: "r"}
	_, err := repoDir(t.TempDir(), repo)
	if err == nil {
		t.Fatal("expected error for git-kind repo")
	}
	if !strings.Contains(err.Error(), "remote") {
		t.Fatalf("expected 'remote' message, got %v", err)
	}
}

func TestRepoDir_PathEscape(t *testing.T) {
	root := t.TempDir()
	// Create one subdir inside root, point repo at "../foo" which escapes.
	repo := model.Repo{Name: "r", Kind: "subpath", Path: "../foo"}
	_, err := repoDir(root, repo)
	if err == nil {
		t.Fatal("expected path-escape error")
	}
	if !strings.Contains(err.Error(), "escapes") {
		t.Fatalf("expected 'escapes' in message, got %v", err)
	}
}

func TestRepoDir_Nonexistent(t *testing.T) {
	repo := model.Repo{Name: "r", Kind: "subpath", Path: "nope"}
	_, err := repoDir(t.TempDir(), repo)
	if err == nil {
		t.Fatal("expected error for nonexistent path")
	}
	if !strings.Contains(err.Error(), "nope") {
		t.Fatalf("expected missing-path error mentioning 'nope': %v", err)
	}
}

func TestRepoDir_PathIsFileNotDir(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "afile"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	repo := model.Repo{Name: "r", Kind: "subpath", Path: "afile"}
	_, err := repoDir(root, repo)
	if err == nil {
		t.Fatal("expected error when path is a file")
	}
	if !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("expected 'not a directory', got %v", err)
	}
}

func TestRepoDir_Success(t *testing.T) {
	root := t.TempDir()
	inner := filepath.Join(root, "child")
	if err := os.Mkdir(inner, 0o755); err != nil {
		t.Fatal(err)
	}
	repo := model.Repo{Name: "r", Kind: "subpath", Path: "child"}
	got, err := repoDir(root, repo)
	if err != nil {
		t.Fatalf("repoDir: %v", err)
	}
	// filepath.Clean should normalize the result.
	if !filepath.IsAbs(got) {
		t.Fatalf("expected absolute path, got %q", got)
	}
}

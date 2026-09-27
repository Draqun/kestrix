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
	"time"

	"kestrix/internal/config"
	"kestrix/internal/dag"
	"kestrix/internal/model"
	"kestrix/internal/variables"
)

func dockerAvailable() bool {
	if testing.Short() || os.Getenv("KESTRIX_UNIT_ONLY") == "1" {
		return false
	}
	_, err := exec.LookPath("docker")
	if err != nil {
		return false
	}
	cmd := exec.Command("docker", "info")
	return cmd.Run() == nil
}

// bashAvailable mirrors dockerAvailable but checks for bash. RunLocal
// shells out via bash -c so it needs a real bash (not busybox sh).
// On alpine CI images bash isn't installed; those tests skip.
func bashAvailable() bool {
	if testing.Short() || os.Getenv("KESTRIX_UNIT_ONLY") == "1" {
		return false
	}
	_, err := exec.LookPath("bash")
	return err == nil
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
			{Name: "first", Trigger: "repo.repo.changed", Execute: model.ExecuteSpec{Repo: "repo", Script: "printf first"}},
			{Name: "second", Trigger: "repo.repo.changed", DependsOn: []string{"first"}, Execute: model.ExecuteSpec{Repo: "repo", Script: "printf second"}},
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
	if results[0].Name != "first" || results[1].Name != "second" {
		t.Errorf("execution order = %q, %q; want first, second", results[0].Name, results[1].Name)
	}
	if !strings.HasSuffix(results[0].Output, "first") || !strings.HasSuffix(results[1].Output, "second") {
		t.Errorf("task outputs = %q, %q; want outputs ending in first and second", results[0].Output, results[1].Output)
	}
}

func TestRunSkipsManualTasks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("runner uses sh")
	}
	if !dockerAvailable() {
		t.Skip("docker not available")
	}
	project := model.Project{
		Name:  "local",
		Repos: []model.Repo{{Name: "repo", Path: ".", Kind: "subpath"}},
		Tasks: []model.Task{{Name: "manual", Trigger: "repo.repo.changed", Manual: true, Execute: model.ExecuteSpec{Repo: "repo", Script: "printf should-not-run"}}},
	}
	var output bytes.Buffer
	results, err := Run(context.Background(), project, map[string]bool{"repo": true}, Options{WorkDir: t.TempDir(), Stdout: &output})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(results) != 1 || results[0].Name != "manual" || results[0].Status != "manual" {
		t.Fatalf("manual task result = %+v", results)
	}
	if results[0].Output != "" {
		t.Errorf("manual task output = %q, want empty", results[0].Output)
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

// --- Pure helper coverage: order / repoDir ------------------
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

// --- RunLocal: bash-only execution path -----------------------------------
//
// RunLocal shells out via `bash -c <script>` instead of docker, so the
// full task pipeline is reachable without docker. Tests use plain
// bash expressions to exercise success, failure, manual-block, and
// dep-block branches.

func TestRunLocal_PassAndScript(t *testing.T) {
	skipOnWindows(t)
	if !bashAvailable() {
		t.Skip("bash not available")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "marker.txt"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	results, err := RunLocal(
		context.Background(),
		simpleProject("p1", "repo.repo.changed",
			`echo hi && cat marker.txt`),
		map[string]bool{"repo": true},
		Options{WorkDir: dir, Stdout: &out, Stderr: &out},
	)
	if err != nil {
		t.Fatalf("RunLocal: %v", err)
	}
	if len(results) != 1 || results[0].Status != "passed" {
		t.Fatalf("results = %+v, want one passed task", results)
	}
	if !strings.Contains(results[0].Output, "hi") {
		t.Errorf("expected captured stdout to contain 'hi', got %q", results[0].Output)
	}
}

// TestRunLocal_CancelKillsProcessGroup is an integration test excluded from
// unit coverage per runner-unit-only-coverage: it runs real sleep processes
// and waits out a real cancel grace period. See design.md D4 and the
// task-execution-isolation capability's "Cancel a running local task"
// scenario.
func TestRunLocal_CancelKillsProcessGroup(t *testing.T) {
	skipOnWindows(t)
	if !bashAvailable() {
		t.Skip("bash not available")
	}
	if _, err := exec.LookPath("pgrep"); err != nil {
		t.Skip("pgrep not available")
	}

	dir := t.TempDir()
	started := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	project := simpleProject("cancel", "repo.repo.changed", "sleep 30 & sleep 30 & wait")
	var out bytes.Buffer

	go func() {
		<-started
		time.Sleep(300 * time.Millisecond) // let both sleeps actually start
		cancel()
	}()

	grace := config.Current().RunnerCancelGrace
	results, err := RunLocal(ctx, project, map[string]bool{"repo": true}, Options{
		WorkDir: dir, Stdout: &out, Stderr: &out,
		OnTaskStart: func(string) { close(started) },
	})
	if err != nil {
		t.Fatalf("RunLocal: %v", err)
	}
	if len(results) != 1 || results[0].Status != "canceled" {
		t.Fatalf("results = %+v, want one canceled (killed) task", results)
	}

	deadline := time.Now().Add(grace + 2*time.Second)
	for {
		output, lookErr := exec.Command("pgrep", "-f", "sleep 30").Output()
		if lookErr != nil {
			return // pgrep exits non-zero when no process matches
		}
		if strings.TrimSpace(string(output)) == "" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("sleep processes still running after grace(%v)+2s: %s", grace, output)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// TestRunOne_CancelKillsProcessGroup mirrors
// TestRunLocal_CancelKillsProcessGroup for the single-task path: cancelling
// the context mid-RunOne must terminate the whole process group within
// grace plus two seconds. See the "Cancel a running single-task re-run"
// scenario. Excluded from unit coverage like its DAG-executor equivalent.
func TestRunOne_CancelKillsProcessGroup(t *testing.T) {
	skipOnWindows(t)
	if !bashAvailable() {
		t.Skip("bash not available")
	}
	if _, err := exec.LookPath("pgrep"); err != nil {
		t.Skip("pgrep not available")
	}

	dir := t.TempDir()
	started := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	project := simpleProject("cancel-one", "repo.repo.changed", "sleep 31 & sleep 31 & wait")
	var out bytes.Buffer

	go func() {
		<-started
		time.Sleep(300 * time.Millisecond) // let both sleeps actually start
		cancel()
	}()

	grace := config.Current().RunnerCancelGrace
	_, runErr := RunOne(ctx, project, "t", Options{
		WorkDir: dir, Stdout: &out, Stderr: &out,
		OnTaskStart: func(string) { close(started) },
	})
	if runErr == nil {
		t.Fatal("RunOne: expected an error after cancellation, got nil")
	}

	deadline := time.Now().Add(grace + 2*time.Second)
	for {
		output, lookErr := exec.Command("pgrep", "-f", "sleep 31").Output()
		if lookErr != nil {
			return // pgrep exits non-zero when no process matches
		}
		if strings.TrimSpace(string(output)) == "" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("sleep processes still running after grace(%v)+2s: %s", grace, output)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func TestRunLocal_Fail(t *testing.T) {
	skipOnWindows(t)
	if !bashAvailable() {
		t.Skip("bash not available")
	}
	dir := t.TempDir()
	var out bytes.Buffer
	results, err := RunLocal(
		context.Background(),
		simpleProject("p1", "repo.repo.changed",
			`exit 7`),
		map[string]bool{"repo": true},
		Options{WorkDir: dir, Stdout: &out, Stderr: &out},
	)
	if err != nil {
		t.Fatalf("RunLocal returned err: %v", err)
	}
	if len(results) != 1 || results[0].Status != "failed" {
		t.Fatalf("results = %+v, want one failed task", results)
	}
	if !strings.Contains(results[0].Error, "exit") {
		t.Errorf("expected error to mention 'exit', got %q", results[0].Error)
	}
}

func TestRunLocal_ManualAndDepBlocked(t *testing.T) {
	skipOnWindows(t)
	if !bashAvailable() {
		t.Skip("bash not available")
	}
	dir := t.TempDir()
	project := model.Project{
		Name:  "p1",
		Repos: []model.Repo{{Name: "repo", Path: ".", Kind: "subpath"}},
		Tasks: []model.Task{
			{Name: "build", Trigger: "repo.repo.changed",
				Execute: model.ExecuteSpec{Repo: "repo", Script: `exit 1`}},
			{Name: "dependent", Trigger: "repo.repo.changed",
				DependsOn: []string{"build"}, Execute: model.ExecuteSpec{Repo: "repo", Script: `echo should_not_run`}},
			{Name: "manual-ok", Trigger: "repo.repo.changed", Manual: true,
				Execute: model.ExecuteSpec{Repo: "repo", Script: `echo manual`}},
		},
	}
	var out bytes.Buffer
	results, err := RunLocal(context.Background(), project,
		map[string]bool{"repo": true},
		Options{WorkDir: dir, Stdout: &out, Stderr: &out},
	)
	if err != nil {
		t.Fatalf("RunLocal: %v", err)
	}
	byName := map[string]TaskResult{}
	for _, r := range results {
		byName[r.Name] = r
	}
	if byName["build"].Status != "failed" {
		t.Errorf("build status = %q, want failed", byName["build"].Status)
	}
	if byName["dependent"].Status != "skipped" {
		t.Errorf("dependent status = %q, want skipped", byName["dependent"].Status)
	}
	if byName["manual-ok"].Status != "manual" {
		t.Errorf("manual-ok status = %q, want manual", byName["manual-ok"].Status)
	}
}

func TestRunLocal_IncludeManual(t *testing.T) {
	skipOnWindows(t)
	if !bashAvailable() {
		t.Skip("bash not available")
	}
	dir := t.TempDir()
	var completionFired bool
	var out bytes.Buffer
	// In RunLocal the bash subprocess runs with cwd=taskDir (the repo
	// subdir), so write the marker file relative to "." rather than
	// to /workspace, which only exists in docker mode.
	results, err := RunLocal(
		context.Background(),
		model.Project{
			Name:  "p1",
			Repos: []model.Repo{{Name: "repo", Path: ".", Kind: "subpath"}},
			Tasks: []model.Task{
				{Name: "manual", Trigger: "repo.repo.changed", Manual: true,
					Execute: model.ExecuteSpec{Repo: "repo", Script: `echo manual && date +%s > ./m.run`}},
			},
		},
		map[string]bool{"repo": true},
		Options{WorkDir: dir, Stdout: &out, Stderr: &out, IncludeManual: true,
			OnTaskStart:    func(string) {},
			OnTaskComplete: func(r TaskResult) { completionFired = true },
		},
	)
	if err != nil {
		t.Fatalf("RunLocal: %v", err)
	}
	if len(results) != 1 || results[0].Status != "passed" {
		t.Fatalf("results = %+v, want one passed task", results)
	}
	if !completionFired {
		t.Errorf("OnTaskComplete not fired")
	}
	if _, err := os.Stat(filepath.Join(dir, "m.run")); err != nil {
		t.Errorf("manual task did not produce the marker file: %v", err)
	}
}

func TestRunLocal_IgnoreOnTaskCallbacks(t *testing.T) {
	skipOnWindows(t)
	if !bashAvailable() {
		t.Skip("bash not available")
	}
	// OnTaskStart and OnTaskComplete nil → branches where they're not
	// called must still be safe.
	dir := t.TempDir()
	var out bytes.Buffer
	results, err := RunLocal(context.Background(),
		simpleProject("p", "repo.repo.changed", `echo ok`),
		map[string]bool{"repo": true},
		Options{WorkDir: dir, Stdout: &out, Stderr: &out},
	)
	if err != nil || len(results) != 1 || results[0].Status != "passed" {
		t.Fatalf("results=%+v err=%v", results, err)
	}
}

func TestRunLocal_WorkdirDefault(t *testing.T) {
	skipOnWindows(t)
	if !bashAvailable() {
		t.Skip("bash not available")
	}
	// options.WorkDir == "" → defaults to "." which is the cwd of the test
	// process. As long as the task produces no output and the call returns
	// cleanly, the default branch was exercised.
	var out bytes.Buffer
	results, err := RunLocal(context.Background(),
		simpleProject("p", "repo.repo.changed", `:`),
		map[string]bool{"repo": true},
		Options{Stdout: &out, Stderr: &out},
	)
	if err != nil || len(results) != 1 || results[0].Status != "passed" {
		t.Fatalf("results=%+v err=%v", results, err)
	}
}

func TestRunLocal_InvalidWorkdir(t *testing.T) {
	skipOnWindows(t)
	if !bashAvailable() {
		t.Skip("bash not available")
	}
	// nonexistent workdir → error path
	_, err := RunLocal(context.Background(),
		simpleProject("p", "repo.repo.changed", `:`),
		map[string]bool{"repo": true},
		Options{WorkDir: "/nonexistent-please-12345"},
	)
	if err == nil {
		t.Fatal("expected error for missing workdir")
	}
}

func TestRunLocal_ResolveFails(t *testing.T) {
	skipOnWindows(t)
	if !bashAvailable() {
		t.Skip("bash not available")
	}
	// Trigger referencing an undeclared repo fails Validate() before
	// Resolve() is reached, but the runner still surfaces a wrapped
	// pipeline error. We only care that RunLocal surfaces whatever
	// pipeline-validation error occurs — not which one specifically.
	dir := t.TempDir()
	bad := model.Project{
		Name:  "p",
		Repos: []model.Repo{{Name: "declared", Path: ".", Kind: "subpath"}},
		Tasks: []model.Task{
			{Name: "t", Trigger: "repo.undeclared.changed",
				Execute: model.ExecuteSpec{Repo: "declared", Script: `:`}},
		},
	}
	_, err := RunLocal(context.Background(), bad,
		map[string]bool{"declared": true},
		Options{WorkDir: dir},
	)
	if err == nil {
		t.Fatal("expected error from pipeline validation")
	}
}

func TestRunOne_HappyPath(t *testing.T) {
	skipOnWindows(t)
	if !bashAvailable() {
		t.Skip("bash not available")
	}
	dir := t.TempDir()
	var completionFired bool
	var out bytes.Buffer
	project := model.Project{
		Name:  "p1",
		Repos: []model.Repo{{Name: "repo", Path: ".", Kind: "subpath"}},
		Tasks: []model.Task{
			{Name: "first", Trigger: "repo.repo.changed",
				Execute: model.ExecuteSpec{Repo: "repo", Script: `echo first`}},
		},
	}
	res, err := RunOne(context.Background(), project, "first",
		Options{WorkDir: dir, Stdout: &out, Stderr: &out,
			OnTaskStart:    func(string) {},
			OnTaskComplete: func(r TaskResult) { completionFired = true }},
	)
	if err != nil {
		t.Fatalf("RunOne: %v", err)
	}
	if res.Status != "passed" {
		t.Errorf("status = %q, want passed", res.Status)
	}
	if !completionFired {
		t.Errorf("OnTaskComplete not fired")
	}
}

func TestRunOne_TaskNotFound(t *testing.T) {
	skipOnWindows(t)
	if !bashAvailable() {
		t.Skip("bash not available")
	}
	project := simpleProject("p", "repo.repo.changed", `:`)
	_, err := RunOne(context.Background(), project, "ghost",
		Options{WorkDir: t.TempDir()},
	)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected not-found error, got %v", err)
	}
}

// When the task's script exits non-zero, RunOne must surface the error
// (with the task name in the message), mark result.Status="failed",
// and still fire OnTaskComplete.
func TestRunOne_FailedScript(t *testing.T) {
	skipOnWindows(t)
	if !bashAvailable() {
		t.Skip("bash not available")
	}
	dir := t.TempDir()
	var completion TaskResult
	var out bytes.Buffer
	project := model.Project{
		Name:  "p1",
		Repos: []model.Repo{{Name: "repo", Path: ".", Kind: "subpath"}},
		Tasks: []model.Task{
			{Name: "boom", Trigger: "repo.repo.changed",
				Execute: model.ExecuteSpec{Repo: "repo", Script: `exit 7`}},
		},
	}
	res, err := RunOne(context.Background(), project, "boom",
		Options{WorkDir: dir, Stdout: &out, Stderr: &out,
			OnTaskStart:    func(string) {},
			OnTaskComplete: func(r TaskResult) { completion = r }},
	)
	if err == nil {
		t.Fatal("expected error from non-zero exit")
	}
	if !strings.Contains(err.Error(), `"boom"`) {
		t.Errorf("error should include task name, got %v", err)
	}
	if res.Status != "failed" {
		t.Errorf("status = %q, want failed", res.Status)
	}
	if completion.Status != "failed" {
		t.Errorf("OnTaskComplete status = %q, want failed", completion.Status)
	}
}

// RunOne resolves the task's Execute.Repo against project.Repos. Missing
// repo entries fall through to a zero-value model.Repo, which forces
// repoDir's "not a directory" branch via Path="." pointing at a file.
func TestRunOne_MissingRepo(t *testing.T) {
	skipOnWindows(t)
	if !bashAvailable() {
		t.Skip("bash not available")
	}
	root := t.TempDir()
	// Put a regular file in the root so . is "not a directory" — repoDir
	// joins the (default) Path="." onto WorkDir and expects a directory.
	if err := os.WriteFile(filepath.Join(root, "block"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	project := model.Project{
		Name:  "p1",
		Repos: []model.Repo{{Name: "other", Path: ".", Kind: "subpath"}}, // declares "other", not "repo"
		Tasks: []model.Task{
			{Name: "ghost", Trigger: "repo.repo.changed",
				Execute: model.ExecuteSpec{Repo: "repo", Script: `:`}},
		},
	}
	_, err := RunOne(context.Background(), project, "ghost",
		Options{WorkDir: filepath.Join(root, "block")},
	)
	if err == nil {
		t.Fatal("expected error for undeclared repo")
	}
	if !strings.Contains(err.Error(), "not a directory") {
		t.Errorf("expected 'not a directory' in error, got %v", err)
	}
}

// A git-typed repo cannot be run locally; RunOne must surface that
// guard from repoDir instead of shelling out.
func TestRunOne_GitRepoRejected(t *testing.T) {
	skipOnWindows(t)
	if !bashAvailable() {
		t.Skip("bash not available")
	}
	project := model.Project{
		Name:  "p1",
		Repos: []model.Repo{{Name: "remote", Path: ".", Kind: "git"}},
		Tasks: []model.Task{
			{Name: "remote_task", Trigger: "remote.head.changed",
				Execute: model.ExecuteSpec{Repo: "remote", Script: `:`}},
		},
	}
	_, err := RunOne(context.Background(), project, "remote_task",
		Options{WorkDir: t.TempDir()},
	)
	if err == nil {
		t.Fatal("expected git-repo error")
	}
	if !strings.Contains(err.Error(), "cannot be run locally") {
		t.Errorf("expected local-only error, got %v", err)
	}
}

// TestRunOne_Defaults covers the small defensive default branches:
// nil Stderr gets substituted with io.Discard, nil Stdout ditto. The
// `if relDir == ""` guard at line 481 looks like it could be reached by
// feeding WorkDir equal to the repo's resolved taskDir, but in practice
// `filepath.Rel"between two equal paths always returns "." — never ""` —
// Go's stdlib guarantees that, so the branch is unreachable on supported
// platforms. Left uncovered intentionally: documented, not faked.
func TestRunOne_Defaults(t *testing.T) {
	skipOnWindows(t)
	if !bashAvailable() {
		t.Skip("bash not available")
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	project := model.Project{
		Name:  "p1",
		Repos: []model.Repo{{Name: "repo", Path: ".", Kind: "subpath"}},
		Tasks: []model.Task{
			{Name: "t", Trigger: "repo.repo.changed",
				Execute: model.ExecuteSpec{Repo: "repo", Script: `:`}},
		},
	}
	res, err := RunOne(context.Background(), project, "t",
		Options{WorkDir: cwd}, // Stderr and Stdout intentionally nil
	)
	if err != nil {
		t.Fatalf("RunOne: %v", err)
	}
	if res.Status != "passed" {
		t.Errorf("status = %q, want passed", res.Status)
	}
}

func TestRunLocal_VariablesInjected(t *testing.T) {
	skipOnWindows(t)
	if !bashAvailable() {
		t.Skip("bash not available")
	}
	dir := t.TempDir()
	// Project's repo name is "repo"; buildVars must bind variables to
	// that same repo, otherwise Get() returns nothing.
	store := buildVars(internalCfgForVars(), "repo")
	var out bytes.Buffer
	results, err := RunLocal(context.Background(),
		simpleProject("p", "repo.repo.changed",
			`echo "$MY_VAR" > ./v.out`),
		map[string]bool{"repo": true},
		Options{WorkDir: dir, Stdout: &out, Stderr: &out, Variables: store},
	)
	if err != nil {
		t.Fatalf("RunLocal: %v", err)
	}
	if len(results) != 1 || results[0].Status != "passed" {
		t.Fatalf("results = %+v, want one passed", results)
	}
	got, err := os.ReadFile(filepath.Join(dir, "v.out"))
	if err != nil {
		t.Fatalf("expected v.out: %v", err)
	}
	if !strings.Contains(string(got), "value-good") {
		t.Errorf("variable not injected: got %q", got)
	}
}

func TestRunLocal_WorkdirIsFile(t *testing.T) {
	skipOnWindows(t)
	if !bashAvailable() {
		t.Skip("bash not available")
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "afile"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "afile")
	// point WorkDir at the file; must fail the IsDir check.
	_, err := RunLocal(context.Background(),
		simpleProject("p", "repo.repo.changed", `:`),
		map[string]bool{"repo": true},
		Options{WorkDir: dir},
	)
	if err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("expected not-a-directory error, got %v", err)
	}
}

func TestRunLocal_FailPropagatesCompletionCallback(t *testing.T) {
	skipOnWindows(t)
	if !bashAvailable() {
		t.Skip("bash not available")
	}
	dir := t.TempDir()
	var completionFired bool
	var out bytes.Buffer
	results, err := RunLocal(context.Background(),
		model.Project{
			Name:  "p1",
			Repos: []model.Repo{{Name: "repo", Path: ".", Kind: "subpath"}},
			Tasks: []model.Task{
				{Name: "failing", Trigger: "repo.repo.changed",
					Execute: model.ExecuteSpec{Repo: "repo", Script: `exit 1`}},
			},
		},
		map[string]bool{"repo": true},
		Options{WorkDir: dir, Stdout: &out, Stderr: &out,
			OnTaskComplete: func(r TaskResult) { completionFired = true }},
	)
	if err != nil {
		t.Fatalf("RunLocal err = %v", err)
	}
	if len(results) != 1 || results[0].Status != "failed" {
		t.Fatalf("results = %+v", results)
	}
	if !completionFired {
		t.Errorf("OnTaskComplete should fire on failure")
	}
}

// TestRunLocal_ManualSkipFiresCallback covers the [MANUAL] skip path
// (line 243-245 in runner.go): when a task is manual and IncludeManual
// is false, RunLocal emits TaskResult{Status:"manual"} and fires
// OnTaskComplete on that result. Existing TestRunLocal_ManualAndDepBlocked
// passes nil OnTaskComplete and so never exercises the callback branch.
func TestRunLocal_ManualSkipFiresCallback(t *testing.T) {
	skipOnWindows(t)
	if !bashAvailable() {
		t.Skip("bash not available")
	}
	dir := t.TempDir()
	var fired TaskResult
	project := model.Project{
		Name:  "p1",
		Repos: []model.Repo{{Name: "repo", Path: ".", Kind: "subpath"}},
		Tasks: []model.Task{
			{Name: "manual", Trigger: "repo.repo.changed", Manual: true,
				Execute: model.ExecuteSpec{Repo: "repo", Script: `echo manual`}},
		},
	}
	results, err := RunLocal(context.Background(), project,
		map[string]bool{"repo": true},
		Options{WorkDir: dir,
			OnTaskComplete: func(r TaskResult) { fired = r }},
	)
	if err != nil {
		t.Fatalf("RunLocal: %v", err)
	}
	if len(results) != 1 || results[0].Status != "manual" {
		t.Fatalf("results = %+v, want one manual", results)
	}
	if fired.Status != "manual" || fired.Name != "manual" {
		t.Errorf("OnTaskComplete fired with %+v, want {manual manual}", fired)
	}
}

// TestRunLocal_DependencySkipFiresCallback covers the [SKIP] branch
// when a dep failed (line 260-262 in runner.go): the dependent task's
// OnTaskComplete is fired with Status="skipped".
func TestRunLocal_DependencySkipFiresCallback(t *testing.T) {
	skipOnWindows(t)
	if !bashAvailable() {
		t.Skip("bash not available")
	}
	dir := t.TempDir()
	var fired []TaskResult
	project := model.Project{
		Name:  "p1",
		Repos: []model.Repo{{Name: "repo", Path: ".", Kind: "subpath"}},
		Tasks: []model.Task{
			{Name: "build", Trigger: "repo.repo.changed",
				Execute: model.ExecuteSpec{Repo: "repo", Script: `exit 1`}},
			{Name: "dependent", Trigger: "repo.repo.changed", DependsOn: []string{"build"},
				Execute: model.ExecuteSpec{Repo: "repo", Script: `echo should_not_run`}},
		},
	}
	_, err := RunLocal(context.Background(), project,
		map[string]bool{"repo": true},
		Options{WorkDir: dir,
			OnTaskComplete: func(r TaskResult) { fired = append(fired, r) }},
	)
	if err != nil {
		t.Fatalf("RunLocal: %v", err)
	}
	byName := map[string]TaskResult{}
	for _, r := range fired {
		byName[r.Name] = r
	}
	if byName["dependent"].Status != "skipped" {
		t.Errorf("dependent callback = %+v, want skipped", byName["dependent"])
	}
	if !strings.Contains(byName["dependent"].Error, "dependency failed") {
		t.Errorf("dependent error = %q, want 'dependency failed'", byName["dependent"].Error)
	}
}

// TestRunLocal_OrderFailureAfterResolve covers the order() error branch
// in RunLocal (line 227-229). To reach it we need Validate and Resolve
// both to pass but `order(nodes)` to fail — which happens when a task
// is triggered with a DependsOn pointing at a task that Resolve dropped
// (non-triggered). DAG's detectCycles sees the dep target exists by
// name in project.Tasks and so does not flag it as unknown, but Resolve
// removes the non-triggered task from the `nodes` slice and order's
// inner lookup `byName[name]` misses.
func TestRunLocal_OrderFailureAfterResolve(t *testing.T) {
	skipOnWindows(t)
	if !bashAvailable() {
		t.Skip("bash not available")
	}
	dir := t.TempDir()
	project := model.Project{
		Name:  "p1",
		Repos: []model.Repo{{Name: "repo", Path: ".", Kind: "subpath"}},
		Tasks: []model.Task{
			// Non-triggered (negated expr is false because repo IS changed) —
			// Resolve omits it from nodes, but detectCycles sees it exists.
			{Name: "nontrigger", Trigger: "!repo.repo.changed",
				Execute: model.ExecuteSpec{Repo: "repo", Script: `:`}},
			{Name: "downstream", Trigger: "repo.repo.changed",
				DependsOn: []string{"nontrigger"},
				Execute:   model.ExecuteSpec{Repo: "repo", Script: `:`}},
		},
	}
	_, err := RunLocal(context.Background(), project,
		map[string]bool{"repo": true},
		Options{WorkDir: dir},
	)
	if err == nil {
		t.Fatal("expected order error from untriggered dependency")
	}
	if !strings.Contains(err.Error(), "not triggered") {
		t.Errorf("expected 'not triggered' error, got %v", err)
	}
}

// TestRunLocal_RepoDirFailsInLoop covers the in-loop repoDir error path
// (line 267-269 in runner.go): when the resolved task points at a git
// repo, repoDir returns "cannot be run locally" mid-iteration. Validate
// passes (repo exists in project.Repos), Resolve passes (trigger syntax
// is fine), order passes (no depends issues), and the error fires only
// at repoDir inside the task loop.
func TestRunLocal_RepoDirFailsInLoop(t *testing.T) {
	skipOnWindows(t)
	if !bashAvailable() {
		t.Skip("bash not available")
	}
	dir := t.TempDir()
	project := model.Project{
		Name: "p1",
		Repos: []model.Repo{
			{Name: "remote", Path: ".", Kind: "git"},
		},
		Tasks: []model.Task{
			{Name: "failme", Trigger: "repo.remote.changed",
				Execute: model.ExecuteSpec{Repo: "remote", Script: `:`}},
		},
	}
	results, err := RunLocal(context.Background(), project,
		map[string]bool{"remote": true},
		Options{WorkDir: dir},
	)
	if err == nil {
		t.Fatal("expected repoDir error inside RunLocal loop")
	}
	if !strings.Contains(err.Error(), "cannot be run locally") {
		t.Errorf("expected git-repo error, got %v", err)
	}
	// No tasks should be reported when repoDir fails on the only task.
	if len(results) != 0 {
		t.Errorf("expected 0 partial results, got %d", len(results))
	}
}

// Unreachable-branches documentation for RunLocal (lines 212-213, 223-224,
// and 272-273 in runner.go):
//   - filepath.Abs error branch (line 212-213): filepath.Abs only fails
//     when os.Getwd() fails, which requires the current working directory
//     to have been removed or the underlying filesystem unmounted — both
//     are outside the reach of portable unit tests. Left uncovered.
//   - Resolve error branch (line 223-224): Validate (RunLocal line 205)
//     runs the same EvaluateTrigger + detectCycles passes against the
//     same Engine. Any input that makes Resolve's EvaluateTrigger/Cycle
//     fail would have been caught by Validate moments earlier; the only
//     remaining difference is the `changed` set passed to EvaluateTrigger
//     (empty vs caller-provided), but EvaluateTrigger's knownRepos is
//     always derived from project.Repos, not from `changed`, so the error
//     outcome is identical. Reachable only by an Engine whose definition
//     mutates between the two calls — not a supported state of this
//     package. Defensive guard, left uncovered.
//   - relDir == "" fallback (line 272-273): filepath.Rel between two
//     equal paths always returns "." (never "" — stdlib guarantee),
//     so `if relDir == ""` is unreachable on supported platforms.
//
// These match the documented pattern in the go-coverage-to-100 skill
// (§4 unreachable-branch annotation): we annotate rather than fake.

// internalCfgForVars / buildVars are small helpers below.
func internalCfgForVars() map[string]string { return map[string]string{"MY_VAR": "value-good"} }
func buildVars(cfg map[string]string, repo string) *variables.Store {
	st := variables.New()
	for k, v := range cfg {
		st.SetRepo(repo, k, v)
	}
	return st
}

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
	"gitlab.com/lunaris-bot/metaci/internal/variables"
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

// --- RunLocal: bash-only execution path -----------------------------------
//
// RunLocal shells out via `bash -c <script>` instead of docker, so the
// full task pipeline is reachable without docker. Tests use plain
// bash expressions to exercise success, failure, manual-block, and
// dep-block branches.

func TestRunLocal_PassAndScript(t *testing.T) {
	skipOnWindows(t)
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

func TestRunLocal_Fail(t *testing.T) {
	skipOnWindows(t)
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
			OnTaskStart:   func(string) {},
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
	project := simpleProject("p", "repo.repo.changed", `:`)
	_, err := RunOne(context.Background(), project, "ghost",
		Options{WorkDir: t.TempDir()},
	)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected not-found error, got %v", err)
	}
}

func TestRunLocal_VariablesInjected(t *testing.T) {
	skipOnWindows(t)
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

// internalCfgForVars / buildVars are small helpers below.
func internalCfgForVars() map[string]string { return map[string]string{"MY_VAR": "value-good"} }
func buildVars(cfg map[string]string, repo string) *variables.Store {
	st := variables.New()
	for k, v := range cfg {
		st.SetRepo(repo, k, v)
	}
	return st
}

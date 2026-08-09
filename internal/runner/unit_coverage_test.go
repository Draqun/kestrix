package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"kestrix/internal/dag"
	"kestrix/internal/domain"
	"kestrix/internal/model"
	"kestrix/internal/variables"
)

func TestRunnerHelperProcess(t *testing.T) {
	isHelper, fail := false, false
	for _, arg := range os.Args {
		isHelper = isHelper || arg == "--kestrix-runner-helper"
		fail = fail || arg == "--kestrix-runner-helper-fail"
	}
	if !isHelper {
		return
	}
	_, _ = fmt.Fprint(os.Stdout, "fake stdout")
	_, _ = fmt.Fprint(os.Stderr, "fake stderr")
	if fail {
		os.Exit(7)
	}
	os.Exit(0)
}

func installFakeCommands(t *testing.T, fail bool) {
	t.Helper()
	old := commandContext
	commandContext = func(ctx context.Context, _ string, args ...string) *exec.Cmd {
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=TestRunnerHelperProcess", "--", "--kestrix-runner-helper")
		cmd.Args = append(cmd.Args, args...)
		if fail {
			cmd.Args = append(cmd.Args, "--kestrix-runner-helper-fail")
		}
		return cmd
	}
	t.Cleanup(func() { commandContext = old })
}

func runnerProject(script string) model.Project {
	return model.Project{
		Name:  "unit",
		Repos: []model.Repo{{Name: "repo", Path: ".", Kind: "subpath"}},
		Tasks: []model.Task{{
			Name: "task", Trigger: "repo.repo.changed",
			Execute: model.ExecuteSpec{Repo: "repo", Script: script},
		}},
	}
}

type stubExecutor struct {
	errors map[string]error
	calls  []string
}

func (e *stubExecutor) RunTask(_ context.Context, task model.Task, _ Options) (string, error) {
	e.calls = append(e.calls, task.Name)
	return "output-" + task.Name, e.errors[task.Name]
}

func TestUnitTaskLoopBranches(t *testing.T) {
	nodes := []dag.Node{
		{Task: model.Task{Name: "pass"}},
		{Task: model.Task{Name: "fail", DependsOn: []string{"pass"}}},
		{Task: model.Task{Name: "blocked", DependsOn: []string{"fail"}}},
		{Task: model.Task{Name: "manual", Manual: true}},
	}
	executor := &stubExecutor{errors: map[string]error{"fail": errors.New("failed")}}
	starts := []string{}
	completions := []TaskResult{}
	results, err := taskLoop(context.Background(), nodes, executor, Options{
		Stdout:         &bytes.Buffer{},
		OnTaskStart:    func(name string) { starts = append(starts, name) },
		OnTaskComplete: func(result TaskResult) { completions = append(completions, result) },
	})
	if err != nil || len(results) != 4 || len(starts) != 2 || len(completions) != 4 {
		t.Fatalf("results=%+v starts=%v completions=%+v err=%v", results, starts, completions, err)
	}
	if results[0].Status != "passed" || results[1].Status != "failed" || results[2].Status != "skipped" || results[3].Status != "manual" {
		t.Fatalf("unexpected statuses: %+v", results)
	}

	fatalExecutor := &stubExecutor{errors: map[string]error{"fatal": taskFatal{err: errors.New("fatal")}}}
	partial, err := taskLoop(context.Background(), []dag.Node{{Task: model.Task{Name: "fatal"}}}, fatalExecutor, Options{Stdout: &bytes.Buffer{}})
	if err == nil || len(partial) != 0 {
		t.Fatalf("fatal task results=%+v err=%v", partial, err)
	}
}

func TestTaskFatalAndOrderCycle(t *testing.T) {
	cause := errors.New("boom")
	fatal := taskFatal{err: cause}
	if fatal.Error() != "boom" || !errors.Is(fatal, cause) {
		t.Fatalf("taskFatal does not preserve cause: %v", fatal)
	}
	_, err := order([]dag.Node{
		{Task: model.Task{Name: "a", DependsOn: []string{"b"}}},
		{Task: model.Task{Name: "b", DependsOn: []string{"a"}}},
	})
	if err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("expected cycle error, got %v", err)
	}
}

func TestConcreteExecutorsWithFakeCommand(t *testing.T) {
	installFakeCommands(t, false)
	root := t.TempDir()
	store := variables.New()
	store.SetRepo("repo", "TOKEN", "unit")
	opts := Options{Variables: store, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}
	task := model.Task{Name: "task", Execute: model.ExecuteSpec{Repo: "repo", Script: "run"}}
	repos := map[string]model.Repo{"repo": {Name: "repo", Path: ".", Kind: "subpath"}}

	docker := &DockerExecutor{WorkDir: root, Repos: repos}
	out, err := docker.RunTask(context.Background(), task, opts)
	if err != nil || !strings.Contains(out, "fake stdout") {
		t.Fatalf("DockerExecutor output=%q err=%v", out, err)
	}

	local := &LocalExecutor{WorkDir: root, Repos: repos}
	out, err = local.RunTask(context.Background(), task, opts)
	if err != nil || !strings.Contains(out, "fake stderr") {
		t.Fatalf("LocalExecutor output=%q err=%v", out, err)
	}

	gitlab := &GitLabRunnerExecutor{WorkDir: root, StageMap: map[string]string{"task": "build"}}
	out, err = gitlab.RunTask(context.Background(), task, opts)
	if err != nil || !strings.Contains(out, "fake stdout") {
		t.Fatalf("GitLabRunnerExecutor output=%q err=%v", out, err)
	}
}

func TestRunnerRemotePreparationAndExternalDockerBranches(t *testing.T) {
	installFakeCommands(t, false)
	project := remoteProject()
	changed := map[string]bool{"app": true}
	failedSources := Options{SourceAcquirer: &recordingAcquirer{fail: true}}
	if _, err := Run(context.Background(), project, changed, failedSources); err == nil {
		t.Fatal("Run should surface source preparation error")
	}
	if _, err := RunGitLabRunner(context.Background(), project, nil, failedSources, func(model.Project, []dag.Node) string { return "yaml" }); err == nil {
		t.Fatal("RunGitLabRunner should surface source preparation error")
	}
	if _, err := RunOne(context.Background(), project, "build", failedSources); err == nil {
		t.Fatal("RunOne should surface source preparation error")
	}
	executor := &PipelineExecutor{Mode: "gitlab-runner", GenerateYAML: func(model.Project, []dag.Node) string { return "yaml" }}
	if _, err := executor.Run(context.Background(), project, changed, domain.RunnerOptions{SourceAcquirer: &recordingAcquirer{fail: true}}); err == nil {
		t.Fatal("PipelineExecutor should surface source preparation error")
	}

	root := t.TempDir()
	external := t.TempDir()
	docker := &DockerExecutor{WorkDir: root, Repos: map[string]model.Repo{"app": {Name: "app", Path: ".", Kind: "subpath"}}}
	_, err := docker.RunTask(context.Background(), model.Task{Name: "task", Execute: model.ExecuteSpec{Repo: "app", Script: "true"}}, Options{
		Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}, RepoPaths: map[string]string{"app": external},
	})
	if err != nil {
		t.Fatalf("external Docker source branch: %v", err)
	}
}

func TestConcreteExecutorsErrors(t *testing.T) {
	root := t.TempDir()
	task := model.Task{Name: "task", Execute: model.ExecuteSpec{Repo: "repo", Script: "run"}}
	opts := Options{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}
	badRepos := map[string]model.Repo{"repo": {Name: "repo", Kind: "git"}}
	for name, executor := range map[string]Executor{
		"docker": &DockerExecutor{WorkDir: root, Repos: badRepos},
		"local":  &LocalExecutor{WorkDir: root, Repos: badRepos},
	} {
		_, err := executor.RunTask(context.Background(), task, opts)
		var fatal taskFatal
		if !errors.As(err, &fatal) {
			t.Errorf("%s error = %v, want taskFatal", name, err)
		}
	}

	installFakeCommands(t, true)
	goodRepos := map[string]model.Repo{"repo": {Name: "repo", Path: ".", Kind: "subpath"}}
	for name, executor := range map[string]Executor{
		"docker": &DockerExecutor{WorkDir: root, Repos: goodRepos, DefaultImage: "alpine"},
		"local":  &LocalExecutor{WorkDir: root, Repos: goodRepos},
		"gitlab": &GitLabRunnerExecutor{WorkDir: root, StageMap: map[string]string{"task": "test"}},
	} {
		if _, err := executor.RunTask(context.Background(), task, opts); err == nil {
			t.Errorf("%s should return fake command error", name)
		}
	}
}

func TestRunDockerUnitBranches(t *testing.T) {
	installFakeCommands(t, false)
	changed := map[string]bool{"repo": true}
	results, err := Run(context.Background(), runnerProject("run"), changed, Options{WorkDir: t.TempDir(), SourcesPrepared: true})
	if err != nil || len(results) != 1 || results[0].Status != "passed" {
		t.Fatalf("Run results=%+v err=%v", results, err)
	}
	badProject := runnerProject("run")
	badProject.Tasks[0].Trigger = "repo.missing.changed"
	if _, err := Run(context.Background(), badProject, nil, Options{SourcesPrepared: true}); err == nil {
		t.Fatal("expected validation error")
	}
	if _, err := Run(context.Background(), runnerProject("run"), changed, Options{WorkDir: filepath.Join(t.TempDir(), "missing"), SourcesPrepared: true}); err == nil {
		t.Fatal("expected missing workdir error")
	}
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(context.Background(), runnerProject("run"), changed, Options{WorkDir: file, SourcesPrepared: true}); err == nil {
		t.Fatal("expected non-directory workdir error")
	}
	project := runnerProject("run")
	project.Tasks = []model.Task{
		{Name: "not-triggered", Trigger: "!repo.repo.changed", Execute: model.ExecuteSpec{Repo: "repo"}},
		{Name: "dependent", Trigger: "repo.repo.changed", DependsOn: []string{"not-triggered"}, Execute: model.ExecuteSpec{Repo: "repo"}},
	}
	if _, err := Run(context.Background(), project, changed, Options{WorkDir: t.TempDir(), SourcesPrepared: true}); err == nil {
		t.Fatal("expected dependency validation/order error")
	}
}

func TestRunLocalUnitBranches(t *testing.T) {
	installFakeCommands(t, false)
	changed := map[string]bool{"repo": true}
	project := runnerProject("run")
	results, err := RunLocal(context.Background(), project, changed, Options{SourcesPrepared: true})
	if err != nil || len(results) != 1 || results[0].Status != "passed" {
		t.Fatalf("default workdir results=%+v err=%v", results, err)
	}
	bad := runnerProject("run")
	bad.Tasks[0].Trigger = "repo.missing.changed"
	if _, err := RunLocal(context.Background(), bad, changed, Options{SourcesPrepared: true}); err == nil {
		t.Fatal("expected validation error")
	}
	if _, err := RunLocal(context.Background(), project, changed, Options{WorkDir: filepath.Join(t.TempDir(), "missing"), SourcesPrepared: true}); err == nil {
		t.Fatal("expected missing workdir error")
	}
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := RunLocal(context.Background(), project, changed, Options{WorkDir: file, SourcesPrepared: true}); err == nil {
		t.Fatal("expected non-directory workdir error")
	}
	untriggered := runnerProject("run")
	untriggered.Tasks = []model.Task{
		{Name: "not-triggered", Trigger: "!repo.repo.changed", Execute: model.ExecuteSpec{Repo: "repo"}},
		{Name: "dependent", Trigger: "repo.repo.changed", DependsOn: []string{"not-triggered"}, Execute: model.ExecuteSpec{Repo: "repo"}},
	}
	if _, err := RunLocal(context.Background(), untriggered, changed, Options{WorkDir: t.TempDir(), SourcesPrepared: true}); err == nil {
		t.Fatal("expected ordering error")
	}
	gitRepo := runnerProject("run")
	gitRepo.Repos[0].Kind = "git"
	if _, err := RunLocal(context.Background(), gitRepo, changed, Options{WorkDir: t.TempDir(), SourcesPrepared: true}); err == nil {
		t.Fatal("expected repository setup error")
	}
}

func TestRunOneUnitBranches(t *testing.T) {
	installFakeCommands(t, false)
	project := runnerProject("run")
	if _, err := RunOne(context.Background(), project, "missing", Options{SourcesPrepared: true}); err == nil {
		t.Fatal("expected task-not-found error")
	}
	gitRepo := runnerProject("run")
	gitRepo.Repos[0].Kind = "git"
	if _, err := RunOne(context.Background(), gitRepo, "task", Options{WorkDir: t.TempDir(), SourcesPrepared: true}); err == nil {
		t.Fatal("expected repository setup error")
	}
	started, completed := false, false
	result, err := RunOne(context.Background(), project, "task", Options{
		WorkDir: t.TempDir(), SourcesPrepared: true, OnTaskStart: func(string) { started = true }, OnTaskComplete: func(TaskResult) { completed = true },
	})
	if err != nil || result.Status != "passed" || !started || !completed {
		t.Fatalf("result=%+v started=%v completed=%v err=%v", result, started, completed, err)
	}

	old := commandContext
	installFakeCommands(t, true)
	result, err = RunOne(context.Background(), project, "task", Options{WorkDir: t.TempDir(), SourcesPrepared: true})
	if err == nil || result.Status != "failed" {
		t.Fatalf("failed result=%+v err=%v", result, err)
	}
	commandContext = old
}

func TestRepoDir_DefaultPath(t *testing.T) {
	root := t.TempDir()
	got, err := repoDir(root, model.Repo{Name: "repo", Kind: "subpath"})
	if err != nil || got != root {
		t.Fatalf("repoDir=%q err=%v", got, err)
	}
}

func TestFilesystemErrorSeams(t *testing.T) {
	oldAbs, oldTemp, oldWrite := absPath, makeTemp, writeFile
	t.Cleanup(func() { absPath, makeTemp, writeFile = oldAbs, oldTemp, oldWrite })
	absPath = func(string) (string, error) { return "", errors.New("abs") }
	if _, err := Run(context.Background(), runnerProject("run"), map[string]bool{"repo": true}, Options{}); err == nil {
		t.Fatal("Run should surface abs error")
	}
	if _, err := RunLocal(context.Background(), runnerProject("run"), map[string]bool{"repo": true}, Options{}); err == nil {
		t.Fatal("RunLocal should surface abs error")
	}
	if _, err := RunGitLabRunner(context.Background(), model.Project{}, nil, Options{}, func(model.Project, []dag.Node) string { return "" }); err == nil {
		t.Fatal("RunGitLabRunner should surface abs error")
	}
	if _, err := RunOne(context.Background(), runnerProject("run"), "task", Options{}); err == nil {
		t.Fatal("RunOne should surface abs error")
	}

	absPath = oldAbs
	makeTemp = func(string, string) (string, error) { return "", errors.New("temp") }
	if _, err := RunGitLabRunner(context.Background(), model.Project{}, nil, Options{}, func(model.Project, []dag.Node) string { return "" }); err == nil {
		t.Fatal("expected temp error")
	}
	makeTemp = oldTemp
	writeFile = func(string, []byte, os.FileMode) error { return errors.New("write") }
	if _, err := RunGitLabRunner(context.Background(), model.Project{}, nil, Options{}, func(model.Project, []dag.Node) string { return "yaml" }); err == nil {
		t.Fatal("expected write error")
	}
}

func TestResolveErrorSeam(t *testing.T) {
	old := resolveNodes
	resolveNodes = func(model.Project, map[string]bool) ([]dag.Node, error) {
		return nil, errors.New("resolve")
	}
	t.Cleanup(func() { resolveNodes = old })
	project := runnerProject("run")
	changed := map[string]bool{"repo": true}
	if _, err := Run(context.Background(), project, changed, Options{WorkDir: t.TempDir()}); err == nil {
		t.Fatal("Run should surface resolve error")
	}
	if _, err := RunLocal(context.Background(), project, changed, Options{WorkDir: t.TempDir()}); err == nil {
		t.Fatal("RunLocal should surface resolve error")
	}
	executor := &PipelineExecutor{Mode: "gitlab-runner", GenerateYAML: func(model.Project, []dag.Node) string { return "yaml" }}
	if _, err := executor.Run(context.Background(), project, changed, domain.RunnerOptions{}); err == nil {
		t.Fatal("PipelineExecutor should surface resolve error")
	}
}

func TestRunGitLabRunnerUnitBranches(t *testing.T) {
	installFakeCommands(t, false)
	project := runnerProject("run")
	nodes, err := dag.New(project).Resolve(map[string]bool{"repo": true})
	if err != nil {
		t.Fatal(err)
	}
	results, err := RunGitLabRunner(context.Background(), project, nodes, Options{SourcesPrepared: true}, func(model.Project, []dag.Node) string { return "yaml" })
	if err != nil || len(results) != 1 || results[0].Status != "passed" {
		t.Fatalf("results=%+v err=%v", results, err)
	}
	bad := append(nodes, dag.Node{Task: model.Task{Name: "bad", DependsOn: []string{"missing"}}})
	if _, err := RunGitLabRunner(context.Background(), project, bad, Options{WorkDir: t.TempDir(), SourcesPrepared: true}, func(model.Project, []dag.Node) string { return "yaml" }); err == nil {
		t.Fatal("expected ordering error")
	}
}

func TestPipelineExecutorDispatchSeams(t *testing.T) {
	project := domain.Project{Name: "unit"}
	want := []domain.TaskResult{{Name: "fake", Status: "passed"}}
	called := ""
	executor := &PipelineExecutor{
		runLocal: func(context.Context, model.Project, map[string]bool, Options) ([]TaskResult, error) {
			called = "local"
			return want, nil
		},
		runDocker: func(context.Context, model.Project, map[string]bool, Options) ([]TaskResult, error) {
			called = "docker"
			return want, nil
		},
		runGitLab: func(context.Context, model.Project, []dag.Node, Options, func(model.Project, []dag.Node) string) ([]TaskResult, error) {
			called = "gitlab"
			return want, nil
		},
		runOne: func(context.Context, model.Project, string, Options) (TaskResult, error) {
			called = "one"
			return want[0], nil
		},
		GenerateYAML: func(model.Project, []dag.Node) string { return "yaml" },
	}
	for _, mode := range []string{"", "local", "docker", "gitlab-runner"} {
		executor.Mode = mode
		got, err := executor.Run(context.Background(), project, nil, domain.RunnerOptions{})
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("mode %q got=%+v err=%v", mode, got, err)
		}
	}
	got, err := executor.RunTask(context.Background(), project, "fake", domain.RunnerOptions{})
	if err != nil || got.Name != "fake" || called != "one" {
		t.Fatalf("RunTask got=%+v called=%q err=%v", got, called, err)
	}
}

func TestPipelineExecutorDefaultDispatch(t *testing.T) {
	installFakeCommands(t, false)
	project := runnerProject("run")
	changed := map[string]bool{"repo": true}
	root := t.TempDir()

	for _, executor := range []*PipelineExecutor{
		{Mode: "docker"},
		{Mode: "gitlab-runner", GenerateYAML: func(model.Project, []dag.Node) string { return "yaml" }},
	} {
		results, err := executor.Run(context.Background(), project, changed, domain.RunnerOptions{WorkDir: root})
		if err != nil || len(results) != 1 || results[0].Status != "passed" {
			t.Fatalf("mode=%q results=%+v err=%v", executor.Mode, results, err)
		}
	}

	store := variables.New()
	store.SetRepo("repo", "TOKEN", "unit")
	result, err := (&PipelineExecutor{}).RunTask(context.Background(), project, "task", domain.RunnerOptions{
		WorkDir: root, Variables: store,
	})
	if err != nil || result.Status != "passed" {
		t.Fatalf("RunTask result=%+v err=%v", result, err)
	}
}

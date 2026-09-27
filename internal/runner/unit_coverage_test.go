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
	"sync"
	"syscall"
	"testing"
	"time"

	"kestrix/internal/config"
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

// cancelAfterExecutor runs each task normally but cancels the loop's
// context once the named task completes, simulating cancellation arriving
// between two tasks.
type cancelAfterExecutor struct {
	calls       []string
	cancel      context.CancelFunc
	cancelAfter string
}

func (e *cancelAfterExecutor) RunTask(_ context.Context, task model.Task, _ Options) (string, error) {
	e.calls = append(e.calls, task.Name)
	if task.Name == e.cancelAfter {
		e.cancel()
	}
	return "output-" + task.Name, nil
}

func TestTaskLoopStopsSchedulingAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	nodes := []dag.Node{
		{Task: model.Task{Name: "first"}},
		{Task: model.Task{Name: "second"}},
		{Task: model.Task{Name: "third"}},
	}
	executor := &cancelAfterExecutor{cancel: cancel, cancelAfter: "first"}
	var out bytes.Buffer
	var completions []TaskResult
	results, err := taskLoop(ctx, nodes, executor, Options{
		Stdout:         &out,
		OnTaskComplete: func(tr TaskResult) { completions = append(completions, tr) },
	})
	if err != nil {
		t.Fatalf("taskLoop: %v", err)
	}
	if !reflect.DeepEqual(executor.calls, []string{"first"}) {
		t.Fatalf("executor calls = %v, want only [first] - no task should run after cancellation", executor.calls)
	}
	if len(results) != 3 {
		t.Fatalf("results = %+v, want 3 entries", results)
	}
	if results[0].Status != "passed" {
		t.Errorf("first task status = %q, want passed", results[0].Status)
	}
	if results[1].Status != "canceled" || results[1].Error != "canceled" {
		t.Errorf("second task result = %+v, want canceled/canceled", results[1])
	}
	if results[2].Status != "canceled" || results[2].Error != "canceled" {
		t.Errorf("third task result = %+v, want canceled/canceled", results[2])
	}
	if len(completions) != 3 {
		t.Fatalf("OnTaskComplete called %d times, want 3", len(completions))
	}
}

// cancelDuringExecutor cancels the loop's context from inside RunTask itself
// before returning an error, simulating a process killed by our own
// SIGTERM/SIGKILL after cancellation.
type cancelDuringExecutor struct {
	cancel context.CancelFunc
}

func (e *cancelDuringExecutor) RunTask(_ context.Context, _ model.Task, _ Options) (string, error) {
	e.cancel()
	return "", errors.New("signal: terminated")
}

func TestTaskLoopReclassifiesKilledTaskAsCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	nodes := []dag.Node{{Task: model.Task{Name: "victim"}}}
	executor := &cancelDuringExecutor{cancel: cancel}
	var out bytes.Buffer
	results, err := taskLoop(ctx, nodes, executor, Options{Stdout: &out})
	if err != nil {
		t.Fatalf("taskLoop: %v", err)
	}
	if len(results) != 1 || results[0].Status != "canceled" || results[0].Error != "canceled" {
		t.Fatalf("results = %+v, want one canceled task (not failed)", results)
	}
}

func TestTaskLoopPrintsAllowlistOnceBeforeFirstTask(t *testing.T) {
	nodes := []dag.Node{
		{Task: model.Task{Name: "one"}},
		{Task: model.Task{Name: "two"}},
	}
	executor := &stubExecutor{}
	var out bytes.Buffer
	if _, err := taskLoop(context.Background(), nodes, executor, Options{Stdout: &out}); err != nil {
		t.Fatalf("taskLoop: %v", err)
	}
	text := out.String()
	if strings.Count(text, "[ENV] task allowlist:") != 1 {
		t.Fatalf("allowlist line printed %d times, want 1: %q", strings.Count(text, "[ENV] task allowlist:"), text)
	}
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "[ENV] task allowlist:") && strings.Contains(line, "=") {
			t.Errorf("allowlist line exposes a value: %q", line)
		}
	}
}

func TestTaskLoopSkipsAllowlistWhenNoTaskRuns(t *testing.T) {
	nodes := []dag.Node{{Task: model.Task{Name: "manual", Manual: true}}}
	executor := &stubExecutor{}
	var out bytes.Buffer
	if _, err := taskLoop(context.Background(), nodes, executor, Options{Stdout: &out}); err != nil {
		t.Fatalf("taskLoop: %v", err)
	}
	if strings.Contains(out.String(), "[ENV] task allowlist:") {
		t.Errorf("allowlist printed with no task run: %q", out.String())
	}
}

func TestDockerExecutorHardeningFlags(t *testing.T) {
	var captured []string
	old := commandContext
	commandContext = func(ctx context.Context, _ string, args ...string) *exec.Cmd {
		captured = append([]string(nil), args...)
		return exec.CommandContext(ctx, "true")
	}
	t.Cleanup(func() { commandContext = old })

	root := t.TempDir()
	docker := &DockerExecutor{
		WorkDir: root,
		Repos:   map[string]model.Repo{"repo": {Name: "repo", Path: ".", Kind: "subpath"}},
		PIDs:    128,
		Memory:  "1g",
		CPUs:    "1.5",
		User:    "1000:1000",
		Network: "bridge",
	}
	task := model.Task{Name: "task", Execute: model.ExecuteSpec{Repo: "repo", Script: "run"}}
	if _, err := docker.RunTask(context.Background(), task, Options{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}); err != nil {
		t.Fatalf("RunTask: %v", err)
	}

	assertFlagValue(t, captured, "--pids-limit", "128")
	assertFlagValue(t, captured, "--memory", "1g")
	assertFlagValue(t, captured, "--cpus", "1.5")
	assertFlagValue(t, captured, "--network", "bridge")
	assertFlagValue(t, captured, "--user", "1000:1000")
	if !containsArg(captured, "--cap-drop") || !containsArg(captured, "ALL") {
		t.Errorf("missing --cap-drop ALL: %v", captured)
	}
	if !containsArg(captured, "--init") {
		t.Errorf("missing --init (cancel would wait out the full stop grace): %v", captured)
	}
	if !containsArg(captured, "--security-opt") || !containsArg(captured, "no-new-privileges") {
		t.Errorf("missing --security-opt no-new-privileges: %v", captured)
	}
	for _, arg := range captured {
		if strings.Contains(arg, "docker.sock") {
			t.Errorf("docker.sock mounted: %v", captured)
		}
	}
	if containsArg(captured, "--privileged") {
		t.Errorf("--privileged present: %v", captured)
	}
}

func TestDockerExecutorNetworkNoneAndOmittedUser(t *testing.T) {
	var captured []string
	old := commandContext
	commandContext = func(ctx context.Context, _ string, args ...string) *exec.Cmd {
		captured = append([]string(nil), args...)
		return exec.CommandContext(ctx, "true")
	}
	t.Cleanup(func() { commandContext = old })

	root := t.TempDir()
	docker := &DockerExecutor{
		WorkDir: root,
		Repos:   map[string]model.Repo{"repo": {Name: "repo", Path: ".", Kind: "subpath"}},
		Network: "none",
	}
	task := model.Task{Name: "task", Execute: model.ExecuteSpec{Repo: "repo", Script: "run"}}
	if _, err := docker.RunTask(context.Background(), task, Options{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}); err != nil {
		t.Fatalf("RunTask: %v", err)
	}

	assertFlagValue(t, captured, "--network", "none")
	if containsArg(captured, "--user") {
		t.Errorf("--user should be omitted when unset: %v", captured)
	}
	if containsArg(captured, "--pids-limit") || containsArg(captured, "--memory") || containsArg(captured, "--cpus") {
		t.Errorf("zero-valued limits should be omitted: %v", captured)
	}
}

func containsArg(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

// flagValue returns the value following flag's first occurrence in args, and
// whether it was found at all - unlike assertFlagValue, which checks a
// known value, this extracts an unknown one (e.g. a generated container
// name) for the caller to use.
func flagValue(args []string, flag string) (string, bool) {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1], true
		}
	}
	return "", false
}

func assertFlagValue(t *testing.T, args []string, flag, want string) {
	t.Helper()
	got, ok := flagValue(args, flag)
	if !ok {
		t.Errorf("%s not found in %v", flag, args)
		return
	}
	if got != want {
		t.Errorf("%s value = %q, want %q", flag, got, want)
	}
}

// TestKillGroupDefaultImplementation exercises the production killGroup body
// directly - every other test replaces the package variable, so the default
// syscall.Kill call would otherwise never execute. Signal 0 performs no
// delivery, only an existence/permission check, so this is safe to call
// unconditionally regardless of the test process's own process group.
func TestKillGroupDefaultImplementation(t *testing.T) {
	_ = killGroup(os.Getpid(), syscall.Signal(0))
}

// TestSetCancelViaProcessGroupCleanupIsNoOpBeforeCancel exercises the
// cleanup func's nil-timer branch: cmd.Cancel was never invoked (the
// process exited on its own), so there is no pending timer to stop.
func TestSetCancelViaProcessGroupCleanupIsNoOpBeforeCancel(t *testing.T) {
	old := killGroup
	killGroup = func(int, syscall.Signal) error { t.Fatal("killGroup should not be called"); return nil }
	t.Cleanup(func() { killGroup = old })

	cmd := exec.Command("true")
	cmd.Process = &os.Process{Pid: 4243}
	cleanup := setCancelViaProcessGroup(cmd, time.Second)
	cleanup() // Cancel was never called; must not panic or signal anything.
}

// TestSetCancelViaProcessGroupCleanupStopsPendingKill is a regression test
// for the grace-period SIGKILL racing PID reuse after a clean exit: calling
// cleanup right after Cancel (as RunTask does once cmd.Run returns) must
// stop the scheduled SIGKILL before it fires.
func TestSetCancelViaProcessGroupCleanupStopsPendingKill(t *testing.T) {
	type call struct {
		pgid int
		sig  syscall.Signal
	}
	var mu sync.Mutex
	var calls []call
	old := killGroup
	killGroup = func(pgid int, sig syscall.Signal) error {
		mu.Lock()
		calls = append(calls, call{pgid, sig})
		mu.Unlock()
		return nil
	}
	t.Cleanup(func() { killGroup = old })

	cmd := exec.Command("true")
	cmd.Process = &os.Process{Pid: 4244}
	cleanup := setCancelViaProcessGroup(cmd, 30*time.Millisecond)

	if err := cmd.Cancel(); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	cleanup() // Simulates cmd.Run() returning right after SIGTERM: stop the SIGKILL timer.

	time.Sleep(100 * time.Millisecond) // well past the grace period
	mu.Lock()
	defer mu.Unlock()
	if len(calls) != 1 || calls[0] != (call{4244, syscall.SIGTERM}) {
		t.Fatalf("calls = %+v, want exactly one SIGTERM and no SIGKILL after cleanup", calls)
	}
}

func TestSetCancelViaProcessGroupSendsTermThenKill(t *testing.T) {
	type call struct {
		pgid int
		sig  syscall.Signal
	}
	var mu sync.Mutex
	var calls []call
	done := make(chan struct{})
	old := killGroup
	killGroup = func(pgid int, sig syscall.Signal) error {
		mu.Lock()
		calls = append(calls, call{pgid, sig})
		mu.Unlock()
		if sig == syscall.SIGKILL {
			close(done)
		}
		return nil
	}
	t.Cleanup(func() { killGroup = old })

	cmd := exec.Command("true")
	cmd.Process = &os.Process{Pid: 4242}
	setCancelViaProcessGroup(cmd, 10*time.Millisecond)

	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.Setpgid {
		t.Fatal("SysProcAttr.Setpgid not set")
	}
	if cmd.WaitDelay != 10*time.Millisecond {
		t.Fatalf("WaitDelay = %v, want 10ms", cmd.WaitDelay)
	}

	if err := cmd.Cancel(); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the grace-period SIGKILL")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(calls) != 2 {
		t.Fatalf("calls = %+v, want exactly 2", calls)
	}
	if calls[0] != (call{4242, syscall.SIGTERM}) {
		t.Errorf("first call = %+v, want (4242, SIGTERM)", calls[0])
	}
	if calls[1] != (call{4242, syscall.SIGKILL}) {
		t.Errorf("second call = %+v, want (4242, SIGKILL)", calls[1])
	}
}

func TestLocalExecutorsWireProcessGroupCancel(t *testing.T) {
	root := t.TempDir()
	repos := map[string]model.Repo{"repo": {Name: "repo", Path: ".", Kind: "subpath"}}
	task := model.Task{Name: "task", Execute: model.ExecuteSpec{Repo: "repo", Script: "true"}}

	var captured *exec.Cmd
	old := commandContext
	commandContext = func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		captured = exec.CommandContext(ctx, "true")
		return captured
	}
	t.Cleanup(func() { commandContext = old })

	local := &LocalExecutor{WorkDir: root, Repos: repos}
	if _, err := local.RunTask(context.Background(), task, Options{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}); err != nil {
		t.Fatalf("LocalExecutor.RunTask: %v", err)
	}
	if captured.SysProcAttr == nil || !captured.SysProcAttr.Setpgid {
		t.Error("LocalExecutor: SysProcAttr.Setpgid not set")
	}
	if captured.Cancel == nil {
		t.Error("LocalExecutor: Cancel not set")
	}
	if captured.WaitDelay <= 0 {
		t.Error("LocalExecutor: WaitDelay not set")
	}

	captured = nil
	project := runnerProject("true")
	if _, err := RunOne(context.Background(), project, "task", Options{WorkDir: root, SourcesPrepared: true, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}); err != nil {
		t.Fatalf("RunOne: %v", err)
	}
	if captured.SysProcAttr == nil || !captured.SysProcAttr.Setpgid {
		t.Error("RunOne: SysProcAttr.Setpgid not set")
	}
	if captured.Cancel == nil {
		t.Error("RunOne: Cancel not set")
	}
	if captured.WaitDelay <= 0 {
		t.Error("RunOne: WaitDelay not set")
	}
}

func TestDockerExecutorCancelStopsContainerByName(t *testing.T) {
	type invocation struct {
		args []string
		cmd  *exec.Cmd
	}
	var invocations []invocation
	old := commandContext
	commandContext = func(ctx context.Context, _ string, args ...string) *exec.Cmd {
		cmd := exec.CommandContext(ctx, "true")
		invocations = append(invocations, invocation{args: append([]string(nil), args...), cmd: cmd})
		return cmd
	}
	t.Cleanup(func() { commandContext = old })

	root := t.TempDir()
	docker := &DockerExecutor{
		WorkDir: root,
		Repos:   map[string]model.Repo{"repo": {Name: "repo", Path: ".", Kind: "subpath"}},
		Grace:   5 * time.Second,
	}
	task := model.Task{Name: "task", Execute: model.ExecuteSpec{Repo: "repo", Script: "run"}}
	if _, err := docker.RunTask(context.Background(), task, Options{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}); err != nil {
		t.Fatalf("RunTask: %v", err)
	}

	// The task exited normally: no stop should have been issued.
	if len(invocations) != 1 {
		t.Fatalf("normal exit issued %d docker commands, want 1 (run only): %v", len(invocations), invocations)
	}
	runArgs := invocations[0].args
	containerName, ok := flagValue(runArgs, "--name")
	if !ok {
		t.Fatalf("could not find --name in run args: %v", runArgs)
	}

	// Invoking Cancel (as the stdlib would on context cancellation) must
	// stop the same container by name, with the configured grace period.
	if err := invocations[0].cmd.Cancel(); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if len(invocations) != 2 {
		t.Fatalf("Cancel issued %d docker commands, want 2 (run + stop): %v", len(invocations), invocations)
	}
	stopArgs := invocations[1].args
	want := []string{"stop", "-t", "5", containerName}
	if !reflect.DeepEqual(stopArgs, want) {
		t.Fatalf("stop args = %v, want %v", stopArgs, want)
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

}

func TestRunnerRemotePreparationAndExternalDockerBranches(t *testing.T) {
	installFakeCommands(t, false)
	project := remoteProject()
	changed := map[string]bool{"app": true}
	failedSources := Options{SourceAcquirer: &recordingAcquirer{fail: true}}
	if _, err := Run(context.Background(), project, changed, failedSources); err == nil {
		t.Fatal("Run should surface source preparation error")
	}
	if _, err := RunOne(context.Background(), project, "build", failedSources); err == nil {
		t.Fatal("RunOne should surface source preparation error")
	}
	executor := &PipelineExecutor{Mode: "docker"}
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

// TestRunOneLocalModeUsesBash is the negative counterpart to
// TestRunOneDockerMode (REVIEW.md finding R3): TestRunOneUnitBranches
// already exercises Mode=""/"local", but its installFakeCommands stub
// discards the invoked binary name, so it cannot prove bash (not docker)
// ran. A regression that made executorForMode always build a
// DockerExecutor, regardless of mode, would slip through every existing
// test until this one.
func TestRunOneLocalModeUsesBash(t *testing.T) {
	for _, mode := range []string{"", "local"} {
		t.Run("mode="+mode, func(t *testing.T) {
			var binary string
			old := commandContext
			commandContext = func(ctx context.Context, name string, a ...string) *exec.Cmd {
				binary = name
				return exec.CommandContext(ctx, "true")
			}
			t.Cleanup(func() { commandContext = old })

			project := runnerProject("run")
			result, err := RunOne(context.Background(), project, "task", Options{
				WorkDir: t.TempDir(), SourcesPrepared: true, Mode: mode,
				Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{},
			})
			if err != nil || result.Status != "passed" {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			if binary != "bash" {
				t.Fatalf("expected RunOne to shell out to bash, got binary=%q", binary)
			}
		})
	}
}

// TestRunOneDockerMode covers the known gap tracked as GitLab work item #1:
// manual task approval used to always shell out locally regardless of the
// configured runner mode. With Mode: "docker", RunOne must run the task
// through DockerExecutor (and its hardening flags), not bash -c.
func TestRunOneDockerMode(t *testing.T) {
	type invocation struct {
		binary string
		args   []string
		cmd    *exec.Cmd
	}
	var invocations []invocation
	old := commandContext
	commandContext = func(ctx context.Context, name string, a ...string) *exec.Cmd {
		cmd := exec.CommandContext(ctx, "true")
		invocations = append(invocations, invocation{binary: name, args: append([]string(nil), a...), cmd: cmd})
		return cmd
	}
	t.Cleanup(func() { commandContext = old })

	// A distinct value per field, set only here, so a swapped Memory/CPUs
	// (or any other executorForMode <-> config.Current() field mismatch)
	// fails this test instead of only DockerExecutor's own direct tests,
	// which never go through executorForMode. See REVIEW.md finding R2.
	env := *config.Current()
	env.RunnerDockerPIDs = 77
	env.RunnerDockerMemory = "512m"
	env.RunnerDockerCPUs = "0.75"
	env.RunnerDockerUser = "5000:5000"
	env.RunnerDockerNetwork = "none"
	env.RunnerCancelGrace = 3 * time.Second
	previous := config.ReplaceCurrent(&env)
	t.Cleanup(func() { config.ReplaceCurrent(previous) })

	project := runnerProject("run")
	result, err := RunOne(context.Background(), project, "task", Options{
		WorkDir: t.TempDir(), SourcesPrepared: true, Mode: "docker",
		Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{},
	})
	if err != nil || result.Status != "passed" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if len(invocations) != 1 {
		t.Fatalf("normal exit issued %d docker commands, want 1 (run only): %v", len(invocations), invocations)
	}
	run := invocations[0]
	if run.binary != "docker" {
		t.Fatalf("expected RunOne to shell out to docker, got binary=%q args=%v", run.binary, run.args)
	}
	if !containsArg(run.args, "--cap-drop") || !containsArg(run.args, "alpine:latest") {
		t.Errorf("expected hardened docker run with default image, got args=%v", run.args)
	}
	assertFlagValue(t, run.args, "--pids-limit", "77")
	assertFlagValue(t, run.args, "--memory", "512m")
	assertFlagValue(t, run.args, "--cpus", "0.75")
	assertFlagValue(t, run.args, "--user", "5000:5000")
	assertFlagValue(t, run.args, "--network", "none")

	containerName, ok := flagValue(run.args, "--name")
	if !ok {
		t.Fatalf("could not find --name in run args: %v", run.args)
	}

	// Grace isn't a `docker run` flag - it only surfaces through Cancel's
	// `docker stop -t <grace>`, mirroring
	// TestDockerExecutorCancelStopsContainerByName but reached through
	// RunOne + Mode="docker" instead of constructing DockerExecutor
	// directly, so executorForMode's Grace mapping is actually covered.
	if err := run.cmd.Cancel(); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if len(invocations) != 2 {
		t.Fatalf("Cancel issued %d docker commands, want 2 (run + stop): %v", len(invocations), invocations)
	}
	want := []string{"stop", "-t", "3", containerName}
	if !reflect.DeepEqual(invocations[1].args, want) {
		t.Fatalf("stop args = %v, want %v", invocations[1].args, want)
	}
}

func TestRunOneReportsCanceledWhenContextCanceled(t *testing.T) {
	installFakeCommands(t, true)
	project := runnerProject("run")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := RunOne(ctx, project, "task", Options{WorkDir: t.TempDir(), SourcesPrepared: true})
	if err == nil {
		t.Fatal("expected an error when the context is canceled")
	}
	if result.Status != "canceled" || result.Error != "canceled" {
		t.Fatalf("result=%+v, want canceled/canceled like the DAG taskLoop path", result)
	}
}

func TestRepoDir_DefaultPath(t *testing.T) {
	root := t.TempDir()
	got, err := repoDir(root, model.Repo{Name: "repo", Kind: "subpath"})
	if err != nil || got != root {
		t.Fatalf("repoDir=%q err=%v", got, err)
	}
}

func TestFilesystemErrorSeams(t *testing.T) {
	oldAbs := absPath
	t.Cleanup(func() { absPath = oldAbs })
	absPath = func(string) (string, error) { return "", errors.New("abs") }
	if _, err := Run(context.Background(), runnerProject("run"), map[string]bool{"repo": true}, Options{}); err == nil {
		t.Fatal("Run should surface abs error")
	}
	if _, err := RunLocal(context.Background(), runnerProject("run"), map[string]bool{"repo": true}, Options{}); err == nil {
		t.Fatal("RunLocal should surface abs error")
	}
	if _, err := RunOne(context.Background(), runnerProject("run"), "task", Options{}); err == nil {
		t.Fatal("RunOne should surface abs error")
	}

	absPath = oldAbs
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
		runOne: func(context.Context, model.Project, string, Options) (TaskResult, error) {
			called = "one"
			return want[0], nil
		},
	}
	for _, mode := range []string{"", "local", "docker"} {
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

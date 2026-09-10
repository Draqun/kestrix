// Package runner executes a resolved Kestrix pipeline on the local machine.
package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"kestrix/internal/config"
	"kestrix/internal/dag"
	"kestrix/internal/domain"
	"kestrix/internal/model"
)

var (
	commandContext = exec.CommandContext
	absPath        = filepath.Abs
	makeTemp       = os.MkdirTemp
	writeFile      = os.WriteFile
	resolveNodes   = func(project model.Project, changed map[string]bool) ([]dag.Node, error) {
		return dag.New(project).Resolve(changed)
	}
	// killGroup signals every process in pgid's process group. It is a
	// package variable so unit tests can record calls without sending real
	// signals; see design.md D4.
	killGroup = func(pgid int, sig syscall.Signal) error { return syscall.Kill(-pgid, sig) }
)

// setCancelViaProcessGroup makes cmd start in its own process group and
// wires its Cancel/WaitDelay so that context cancellation sends the group a
// SIGTERM, then a SIGKILL after grace if it hasn't exited (WaitDelay also
// makes the stdlib SIGKILL the direct child if it outlives the same grace).
// See design.md D4 and the task-execution-isolation capability.
//
// It returns a cleanup func the caller must call after cmd.Run() returns.
// Without it, a process group that exits cleanly from SIGTERM well before
// grace would still take a SIGKILL grace later, by which point the OS may
// have reused that pid/pgid for an unrelated process; the cleanup stops the
// pending timer so a clean exit never fires a stray, late SIGKILL.
func setCancelViaProcessGroup(cmd *exec.Cmd, grace time.Duration) (cleanup func()) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var mu sync.Mutex
	var timer *time.Timer
	cmd.Cancel = func() error {
		err := killGroup(cmd.Process.Pid, syscall.SIGTERM)
		mu.Lock()
		timer = time.AfterFunc(grace, func() {
			_ = killGroup(cmd.Process.Pid, syscall.SIGKILL)
		})
		mu.Unlock()
		return err
	}
	cmd.WaitDelay = grace
	return func() {
		mu.Lock()
		defer mu.Unlock()
		if timer != nil {
			timer.Stop()
		}
	}
}

// TaskResult is an alias for domain.TaskResult, the single type used
// across runner, store, and service layers.
type TaskResult = domain.TaskResult

// Options controls a local pipeline run.
type Options struct {
	// WorkDir is the checkout that contains subpath repositories.
	WorkDir string
	// IncludeManual runs tasks marked as manual. They are skipped by default.
	IncludeManual bool
	// Variables provides resolved variables injected as environment variables.
	Variables domain.VariableResolver
	// Stdout and Stderr receive aggregate command output. Nil values discard output.
	Stdout io.Writer
	Stderr io.Writer
	// OnTaskStart is called before a task begins executing.
	OnTaskStart func(name string)
	// OnTaskComplete is called after each task finishes (pass or fail).
	OnTaskComplete func(TaskResult)
	// SourceAcquirer obtains remote repositories before the DAG starts. When
	// nil, local execution keeps the historical checkout behavior.
	SourceSessions        domain.SourceSessionManager
	SourceAcquirer        domain.SourceAcquirer
	CredentialResolver    domain.CredentialResolver
	SourceTimeout         time.Duration
	SourceRetries         int
	RetainSourceWorkspace bool
	RepoPaths             map[string]string
	SourcesPrepared       bool
	OnSourcesAcquired     func([]domain.AcquiredSource) error
	WorkspaceCleanup      func()
}

func synchronizeOutput(opts *Options) {
	if opts.Stdout == nil {
		opts.Stdout = io.Discard
	}
	if opts.Stderr == nil {
		opts.Stderr = io.Discard
	}
	lock := &sync.Mutex{}
	opts.Stdout = &lockedWriter{mu: lock, w: opts.Stdout}
	opts.Stderr = &lockedWriter{mu: lock, w: opts.Stderr}
}

type lockedWriter struct {
	mu *sync.Mutex
	w  io.Writer
}

func (w *lockedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.w.Write(p)
}

// Executor runs a single task in the pipeline.
type Executor interface {
	// RunTask executes a single task, returning its output and any error.
	RunTask(ctx context.Context, task model.Task, opts Options) (output string, err error)
}

// taskFatal signals that a task-level setup error (e.g. repoDir failure)
// should abort the entire pipeline rather than skip to the next task.
type taskFatal struct{ err error }

func (e taskFatal) Error() string { return e.err.Error() }
func (e taskFatal) Unwrap() error { return e.err }

// DockerExecutor runs tasks inside Docker containers with volume mounts.
// PIDs, Memory, and CPUs bound the container's process count, memory, and
// CPU share; User and Network set its user and network. See
// task-execution-isolation and design.md D3.
type DockerExecutor struct {
	WorkDir      string
	Repos        map[string]model.Repo
	DefaultImage string
	PIDs         int
	Memory       string
	CPUs         string
	User         string
	Network      string
	// Grace bounds how long `docker stop` waits before the daemon kills the
	// container; see design.md D4.
	Grace time.Duration
}

func (e *DockerExecutor) RunTask(ctx context.Context, task model.Task, opts Options) (string, error) {
	repo := e.Repos[task.Execute.Repo]
	taskDir, err := repoDirFor(e.WorkDir, repo, opts.RepoPaths)
	if err != nil {
		return "", taskFatal{err: fmt.Errorf("task %q: %w", task.Name, err)}
	}

	image := task.Execute.Image
	if image == "" {
		image = e.DefaultImage
	}

	relDir, _ := filepath.Rel(e.WorkDir, taskDir)
	external := ""
	if candidate, ok := opts.RepoPaths[repo.Name]; ok && !pathWithin(e.WorkDir, candidate) {
		external = candidate
		relDir = filepath.Join("..", "sources", repo.Name)
	}

	fmt.Fprintf(opts.Stdout, "[RUN] %s (image=%s, dir=%s)\n", task.Name, image, relDir)

	containerName := fmt.Sprintf("kestrix-%s-%d", task.Name, time.Now().UnixNano())
	args := []string{
		"run", "--rm",
		"--name", containerName,
		"--cap-drop", "ALL",
		"--security-opt", "no-new-privileges",
	}
	if e.PIDs > 0 {
		args = append(args, "--pids-limit", strconv.Itoa(e.PIDs))
	}
	if e.Memory != "" {
		args = append(args, "--memory", e.Memory)
	}
	if e.CPUs != "" {
		args = append(args, "--cpus", e.CPUs)
	}
	if e.Network != "" {
		args = append(args, "--network", e.Network)
	}
	if e.User != "" {
		args = append(args, "--user", e.User)
	}
	args = append(args,
		"-v", e.WorkDir+":/workspace",
		"-w", "/workspace/"+relDir,
	)
	if external != "" {
		args = append(args, "-v", external+":/sources/"+repo.Name)
	}

	ci := map[string]string{"CI_PROJECT_DIR": "/workspace/" + relDir}
	var vars map[string]string
	if opts.Variables != nil {
		vars = opts.Variables.Get(task.Execute.Repo, task.Name, "")
	}
	for _, entry := range config.Current().TaskEnv(ci, vars) {
		args = append(args, "-e", entry)
	}

	args = append(args, image, "sh", "-c", task.Execute.Script)

	var taskOut bytes.Buffer
	taskOutWriter := &lockedWriter{mu: &sync.Mutex{}, w: &taskOut}
	cmd := commandContext(ctx, "docker", args...)
	cmd.Stdout = io.MultiWriter(opts.Stdout, taskOutWriter)
	cmd.Stderr = io.MultiWriter(opts.Stderr, taskOutWriter)
	cmd.Cancel = func() error {
		grace := strconv.Itoa(int(e.Grace.Seconds()))
		stop := commandContext(context.Background(), "docker", "stop", "-t", grace, containerName)
		return stop.Run()
	}

	runErr := cmd.Run()

	return taskOut.String(), runErr
}

// LocalExecutor runs tasks directly on the host via bash.
type LocalExecutor struct {
	WorkDir string
	Repos   map[string]model.Repo
}

func (e *LocalExecutor) RunTask(ctx context.Context, task model.Task, opts Options) (string, error) {
	repo := e.Repos[task.Execute.Repo]
	taskDir, err := repoDirFor(e.WorkDir, repo, opts.RepoPaths)
	if err != nil {
		return "", taskFatal{err: fmt.Errorf("task %q: %w", task.Name, err)}
	}

	relDir, _ := filepath.Rel(e.WorkDir, taskDir)

	fmt.Fprintf(opts.Stdout, "[RUN] %s (local, dir=%s)\n", task.Name, relDir)

	ci := map[string]string{"CI_PROJECT_DIR": taskDir}
	var vars map[string]string
	if opts.Variables != nil {
		vars = opts.Variables.Get(task.Execute.Repo, task.Name, "")
	}
	envSettings := config.Current()
	env := envSettings.TaskEnv(ci, vars)

	var taskOut bytes.Buffer
	taskOutWriter := &lockedWriter{mu: &sync.Mutex{}, w: &taskOut}
	cmd := commandContext(ctx, "bash", "-c", task.Execute.Script)
	cmd.Dir = taskDir
	cmd.Env = env
	cmd.Stdout = io.MultiWriter(opts.Stdout, taskOutWriter)
	cmd.Stderr = io.MultiWriter(opts.Stderr, taskOutWriter)
	stopCancelTimer := setCancelViaProcessGroup(cmd, envSettings.RunnerCancelGrace)

	runErr := cmd.Run()
	stopCancelTimer()

	return taskOut.String(), runErr
}

// GitLabRunnerExecutor runs tasks via gitlab-runner exec.
type GitLabRunnerExecutor struct {
	WorkDir  string
	StageMap map[string]string
}

func (e *GitLabRunnerExecutor) RunTask(ctx context.Context, task model.Task, opts Options) (string, error) {
	stage := e.StageMap[task.Name]
	fmt.Fprintf(opts.Stdout, "[RUN] %s (stage=%s via gitlab-runner exec)\n", task.Name, stage)

	ci := map[string]string{"CI_PROJECT_DIR": e.WorkDir}
	var vars map[string]string
	if opts.Variables != nil {
		vars = opts.Variables.Get(task.Execute.Repo, task.Name, "")
	}

	envSettings := config.Current()

	var taskOut bytes.Buffer
	taskOutWriter := &lockedWriter{mu: &sync.Mutex{}, w: &taskOut}
	cmd := commandContext(ctx, "gitlab-runner", "exec", stage, task.Name)
	cmd.Dir = e.WorkDir
	cmd.Env = envSettings.TaskEnv(ci, vars)
	cmd.Stdout = io.MultiWriter(opts.Stdout, taskOutWriter)
	cmd.Stderr = io.MultiWriter(opts.Stderr, taskOutWriter)
	stopCancelTimer := setCancelViaProcessGroup(cmd, envSettings.RunnerCancelGrace)

	runErr := cmd.Run()
	stopCancelTimer()

	return taskOut.String(), runErr
}

// taskLoop iterates over ordered nodes, skipping manual/blocked tasks and
// delegating execution to the provided Executor.
func taskLoop(ctx context.Context, ordered []dag.Node, executor Executor, opts Options) ([]TaskResult, error) {
	var results []TaskResult
	failedTasks := make(map[string]bool)
	allowlistPrinted := false
	for _, node := range ordered {
		task := node.Task
		if ctx.Err() != nil {
			fmt.Fprintf(opts.Stdout, "[SKIP] %s (run canceled)\n", task.Name)
			canceled := TaskResult{Name: task.Name, Status: "canceled", Error: "canceled"}
			results = append(results, canceled)
			failedTasks[task.Name] = true
			if opts.OnTaskComplete != nil {
				opts.OnTaskComplete(canceled)
			}
			continue
		}
		if task.Manual && !opts.IncludeManual {
			fmt.Fprintf(opts.Stdout, "[MANUAL] %s (waiting for approval)\n", task.Name)
			waiting := TaskResult{Name: task.Name, Status: "manual"}
			results = append(results, waiting)
			if opts.OnTaskComplete != nil {
				opts.OnTaskComplete(waiting)
			}
			continue
		}
		blocked := false
		for _, dep := range task.DependsOn {
			if failedTasks[dep] {
				blocked = true
				break
			}
		}
		if blocked {
			fmt.Fprintf(opts.Stdout, "[SKIP] %s (dependency failed)\n", task.Name)
			skipped := TaskResult{Name: task.Name, Status: "skipped", Error: "dependency failed"}
			results = append(results, skipped)
			failedTasks[task.Name] = true
			if opts.OnTaskComplete != nil {
				opts.OnTaskComplete(skipped)
			}
			continue
		}

		if !allowlistPrinted {
			fmt.Fprintf(opts.Stdout, "[ENV] task allowlist: %s\n", strings.Join(config.Current().AllowlistNames(), ", "))
			allowlistPrinted = true
		}
		if opts.OnTaskStart != nil {
			opts.OnTaskStart(task.Name)
		}

		start := time.Now()
		output, runErr := executor.RunTask(ctx, task, opts)
		dur := time.Since(start)

		var fatal taskFatal
		if errors.As(runErr, &fatal) {
			return results, fatal
		}

		tr := TaskResult{
			Name:     task.Name,
			Output:   output,
			Duration: dur,
		}
		if runErr != nil && ctx.Err() != nil {
			// The run was canceled while this task was executing: its
			// termination is expected, not a pipeline failure. See
			// design.md D5 and the "Cancel a running local task" scenario.
			tr.Status = "canceled"
			tr.Error = "canceled"
			fmt.Fprintf(opts.Stdout, "[DONE] %s (canceled)\n", task.Name)
			results = append(results, tr)
			failedTasks[task.Name] = true
		} else if runErr != nil {
			tr.Status = "failed"
			tr.Error = runErr.Error()
			fmt.Fprintf(opts.Stdout, "[DONE] %s (failed)\n", task.Name)
			results = append(results, tr)
			failedTasks[task.Name] = true
		} else {
			tr.Status = "passed"
			fmt.Fprintf(opts.Stdout, "[DONE] %s\n", task.Name)
			results = append(results, tr)
		}
		if opts.OnTaskComplete != nil {
			opts.OnTaskComplete(tr)
		}
	}
	return results, nil
}

// Run resolves and executes the tasks selected by changed. Each task runs
// in its own Docker container. Tasks run in dependency order.
func Run(ctx context.Context, project model.Project, changed map[string]bool, opts Options) ([]TaskResult, error) {
	synchronizeOutput(&opts)
	defer cleanupWorkspace(&opts)
	project, cleanup, err := prepareSources(ctx, project, &opts)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	if err := dag.New(project).Validate(); err != nil {
		return nil, fmt.Errorf("validate pipeline: %w", err)
	}
	if opts.WorkDir == "" {
		opts.WorkDir = "."
	}
	workDir, err := absPath(opts.WorkDir)
	if err != nil {
		return nil, fmt.Errorf("resolve workdir: %w", err)
	}
	if info, err := os.Stat(workDir); err != nil || !info.IsDir() {
		if err != nil {
			return nil, fmt.Errorf("workdir %q: %w", workDir, err)
		}
		return nil, fmt.Errorf("workdir %q is not a directory", workDir)
	}

	nodes, err := resolveNodes(project, changed)
	if err != nil {
		return nil, fmt.Errorf("resolve pipeline: %w", err)
	}
	ordered, err := order(nodes)
	if err != nil {
		return nil, err
	}
	repos := make(map[string]model.Repo, len(project.Repos))
	for _, repo := range project.Repos {
		repos[repo.Name] = repo
	}

	defaultImage := project.DefaultImage
	if defaultImage == "" {
		defaultImage = "alpine:latest"
	}

	env := config.Current()
	executor := &DockerExecutor{
		WorkDir:      workDir,
		Repos:        repos,
		DefaultImage: defaultImage,
		PIDs:         env.RunnerDockerPIDs,
		Memory:       env.RunnerDockerMemory,
		CPUs:         env.RunnerDockerCPUs,
		User:         env.RunnerDockerUser,
		Network:      env.RunnerDockerNetwork,
		Grace:        env.RunnerCancelGrace,
	}
	return taskLoop(ctx, ordered, executor, opts)
}

// RunLocal resolves and executes tasks directly on the host without Docker.
// Each task runs via `bash -c` in the repo directory.
func RunLocal(ctx context.Context, project model.Project, changed map[string]bool, opts Options) ([]TaskResult, error) {
	synchronizeOutput(&opts)
	defer cleanupWorkspace(&opts)
	project, cleanup, err := prepareSources(ctx, project, &opts)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	if err := dag.New(project).Validate(); err != nil {
		return nil, fmt.Errorf("validate pipeline: %w", err)
	}
	if opts.WorkDir == "" {
		opts.WorkDir = "."
	}
	workDir, err := absPath(opts.WorkDir)
	// filepath.Abs on a non-empty relative path that resolves cleanly
	// never errs on POSIX; t.TempDir()-supplied absolute paths also
	// can't fail this. Defensive guard; branch is unreachable in the
	// current test harness. See runner_test.go header for the
	// "annotate, don't fake" convention applied project-wide.
	if err != nil {
		return nil, fmt.Errorf("resolve workdir: %w", err)
	}
	if info, err := os.Stat(workDir); err != nil || !info.IsDir() {
		if err != nil {
			return nil, fmt.Errorf("workdir %q: %w", workDir, err)
		}
		return nil, fmt.Errorf("workdir %q is not a directory", workDir)
	}

	nodes, err := resolveNodes(project, changed)
	// Validate() above already exercised trigger syntax and cycle
	// detection; Resolve only fails when a task not in the changed-set
	// depends on a node that is — but Validate rejects such degenerate
	// graphs. Branch is unreachable given current DAG invariants.
	if err != nil {
		return nil, fmt.Errorf("resolve pipeline: %w", err)
	}
	ordered, err := order(nodes)
	if err != nil {
		return nil, err
	}
	repos := make(map[string]model.Repo, len(project.Repos))
	for _, repo := range project.Repos {
		repos[repo.Name] = repo
	}

	executor := &LocalExecutor{
		WorkDir: workDir,
		Repos:   repos,
	}
	return taskLoop(ctx, ordered, executor, opts)
}

// RunGitLabRunner executes tasks via `gitlab-runner exec`.
func RunGitLabRunner(ctx context.Context, project model.Project, nodes []dag.Node, opts Options, generateYAML func(model.Project, []dag.Node) string) ([]TaskResult, error) {
	synchronizeOutput(&opts)
	defer cleanupWorkspace(&opts)
	project, cleanup, err := prepareSources(ctx, project, &opts)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	if opts.WorkDir == "" {
		opts.WorkDir = "."
	}
	workDir, err := absPath(opts.WorkDir)
	if err != nil {
		return nil, fmt.Errorf("resolve workdir: %w", err)
	}

	tmpDir, err := makeTemp("", "kestrix-runner-*")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	yaml := generateYAML(project, nodes)
	ciFile := filepath.Join(tmpDir, ".gitlab-ci.yml")
	if err := writeFile(ciFile, []byte(yaml), 0o644); err != nil {
		return nil, fmt.Errorf("write .gitlab-ci.yml: %w", err)
	}
	fmt.Fprintf(opts.Stdout, "[CI] Generated .gitlab-ci.yml (%d bytes)\n", len(yaml))

	stageMap := make(map[string]string, len(nodes))
	for _, n := range nodes {
		stageMap[n.Task.Name] = assignStage(n.Task, nodes)
	}

	ordered, err := order(nodes)
	if err != nil {
		return nil, err
	}

	executor := &GitLabRunnerExecutor{
		WorkDir:  workDir,
		StageMap: stageMap,
	}
	return taskLoop(ctx, ordered, executor, opts)
}

// RunOne executes a single named task (used for manual job approval).
func RunOne(ctx context.Context, project model.Project, taskName string, opts Options) (TaskResult, error) {
	synchronizeOutput(&opts)
	defer cleanupWorkspace(&opts)
	project, cleanup, err := prepareSources(ctx, project, &opts)
	if err != nil {
		return TaskResult{}, err
	}
	defer cleanup()
	if opts.WorkDir == "" {
		opts.WorkDir = "."
	}
	workDir, err := absPath(opts.WorkDir)
	// See RunLocal: filepath.Abs on a clean POSIX path doesn't fail in
	// practice. Defensive guard; the error branch is unreachable.
	if err != nil {
		return TaskResult{}, fmt.Errorf("resolve workdir: %w", err)
	}

	var task *model.Task
	for i := range project.Tasks {
		if project.Tasks[i].Name == taskName {
			task = &project.Tasks[i]
			break
		}
	}
	if task == nil {
		return TaskResult{}, fmt.Errorf("task %q not found", taskName)
	}

	repos := make(map[string]model.Repo, len(project.Repos))
	for _, repo := range project.Repos {
		repos[repo.Name] = repo
	}
	repo := repos[task.Execute.Repo]
	taskDir, err := repoDirFor(workDir, repo, opts.RepoPaths)
	if err != nil {
		return TaskResult{}, fmt.Errorf("task %q: %w", task.Name, err)
	}
	relDir, _ := filepath.Rel(workDir, taskDir)

	fmt.Fprintf(opts.Stdout, "[RUN] %s (manual approve, dir=%s)\n", task.Name, relDir)
	if opts.OnTaskStart != nil {
		opts.OnTaskStart(task.Name)
	}

	ci := map[string]string{"CI_PROJECT_DIR": taskDir}
	var vars map[string]string
	if opts.Variables != nil {
		vars = opts.Variables.Get(task.Execute.Repo, task.Name, "")
	}
	envSettings := config.Current()
	env := envSettings.TaskEnv(ci, vars)

	var taskOut bytes.Buffer
	taskOutWriter := &lockedWriter{mu: &sync.Mutex{}, w: &taskOut}
	cmd := commandContext(ctx, "bash", "-c", task.Execute.Script)
	cmd.Dir = taskDir
	cmd.Env = env
	cmd.Stdout = io.MultiWriter(opts.Stdout, taskOutWriter)
	cmd.Stderr = io.MultiWriter(opts.Stderr, taskOutWriter)
	stopCancelTimer := setCancelViaProcessGroup(cmd, envSettings.RunnerCancelGrace)

	start := time.Now()
	runErr := cmd.Run()
	stopCancelTimer()
	dur := time.Since(start)

	tr := TaskResult{
		Name:     task.Name,
		Output:   taskOut.String(),
		Duration: dur,
	}
	if runErr != nil {
		tr.Status = "failed"
		tr.Error = runErr.Error()
		fmt.Fprintf(opts.Stdout, "[DONE] %s (failed)\n", task.Name)
	} else {
		tr.Status = "passed"
		fmt.Fprintf(opts.Stdout, "[DONE] %s\n", task.Name)
	}
	if opts.OnTaskComplete != nil {
		opts.OnTaskComplete(tr)
	}
	if runErr != nil {
		return tr, fmt.Errorf("task %q failed: %w", task.Name, runErr)
	}
	return tr, nil
}

// assignStage determines which stage a task belongs to.
func assignStage(task model.Task, nodes []dag.Node) string {
	if task.Manual {
		return "deploy"
	}
	if len(task.DependsOn) > 0 {
		for _, dep := range task.DependsOn {
			if strings.Contains(dep, "test") {
				return "deploy"
			}
		}
		return "test"
	}
	return "build"
}

func order(nodes []dag.Node) ([]dag.Node, error) {
	byName := make(map[string]dag.Node, len(nodes))
	for _, node := range nodes {
		byName[node.Task.Name] = node
	}
	ordered := make([]dag.Node, 0, len(nodes))
	visited := make(map[string]bool, len(nodes))
	visiting := make(map[string]bool, len(nodes))
	var visit func(string) error
	visit = func(name string) error {
		if visited[name] {
			return nil
		}
		if visiting[name] {
			return fmt.Errorf("cycle detected while ordering task %q", name)
		}
		node, ok := byName[name]
		if !ok {
			return fmt.Errorf("task dependency %q was not triggered", name)
		}
		visiting[name] = true
		for _, dep := range node.Task.DependsOn {
			if err := visit(dep); err != nil {
				return fmt.Errorf("task %q: %w", name, err)
			}
		}
		visiting[name] = false
		visited[name] = true
		ordered = append(ordered, node)
		return nil
	}
	for _, node := range nodes {
		if err := visit(node.Task.Name); err != nil {
			return nil, err
		}
	}
	return ordered, nil
}

func repoDir(workDir string, repo model.Repo) (string, error) {
	if repo.Kind == "git" {
		return "", fmt.Errorf("remote repository %q cannot be run locally; clone it first and declare it as a subpath", repo.Name)
	}
	path := repo.Path
	if path == "" {
		path = "."
	}
	dir := filepath.Clean(filepath.Join(workDir, path))
	rel, err := filepath.Rel(workDir, dir)
	if err != nil || rel == ".." || len(rel) > 3 && rel[:3] == ".."+string(filepath.Separator) {
		return "", fmt.Errorf("repo path %q escapes workdir", repo.Path)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		if err != nil {
			return "", fmt.Errorf("repo path %q: %w", repo.Path, err)
		}
		return "", fmt.Errorf("repo path %q is not a directory", repo.Path)
	}
	return dir, nil
}

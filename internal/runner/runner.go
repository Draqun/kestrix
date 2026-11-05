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
	commandContext  = exec.CommandContext
	absPath         = filepath.Abs
	resolvePipeline = func(project model.Project, changed map[string]bool) ([]dag.Node, []domain.TaskResult, error) {
		return dag.New(project).ResolveWithSkipped(changed)
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

// CalculateRunStatus computes the overall run status and error message from task results.
func CalculateRunStatus(taskResults []TaskResult, runErr error) (string, string) {
	return domain.CalculateRunStatus(taskResults, runErr)
}

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
	// Mode selects RunOne's execution backend: "docker" runs the task
	// through DockerExecutor (with the configured PID/memory/CPU/network
	// hardening); any other value, including "", falls back to
	// LocalExecutor's bare `bash -c`, matching prior behavior.
	Mode string
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
		// --init makes tini PID 1 so `docker stop` (cancel) delivers SIGTERM
		// to the task; without it a PID-1 `sleep` ignores SIGTERM and every
		// cancel waits out the full grace period before SIGKILL.
		"--init",
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

	runCtx := ctx
	var cancel context.CancelFunc
	if d := task.ParsedDuration(); d > 0 {
		runCtx, cancel = context.WithTimeout(ctx, d)
		defer cancel()
	}

	var taskOut bytes.Buffer
	taskOutWriter := &lockedWriter{mu: &sync.Mutex{}, w: &taskOut}
	cmd := commandContext(runCtx, "docker", args...)
	cmd.Stdout = io.MultiWriter(opts.Stdout, taskOutWriter)
	cmd.Stderr = io.MultiWriter(opts.Stderr, taskOutWriter)
	cmd.Cancel = func() error {
		grace := strconv.Itoa(int(e.Grace.Seconds()))
		stop := commandContext(context.Background(), "docker", "stop", "-t", grace, containerName)
		return stop.Run()
	}

	runErr := cmd.Run()

	if runErr != nil && runCtx.Err() == context.DeadlineExceeded && (ctx == nil || ctx.Err() == nil) {
		runErr = fmt.Errorf("task execution timed out after %s", task.Timeout)
	}

	if runErr == nil {
		if err := checkArtifacts(taskDir, task.Artifacts); err != nil {
			return taskOut.String(), err
		}
	}

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

	runCtx := ctx
	var cancel context.CancelFunc
	if d := task.ParsedDuration(); d > 0 {
		runCtx, cancel = context.WithTimeout(ctx, d)
		defer cancel()
	}

	var taskOut bytes.Buffer
	taskOutWriter := &lockedWriter{mu: &sync.Mutex{}, w: &taskOut}
	cmd := commandContext(runCtx, "bash", "-c", task.Execute.Script)
	cmd.Dir = taskDir
	cmd.Env = env
	cmd.Stdout = io.MultiWriter(opts.Stdout, taskOutWriter)
	cmd.Stderr = io.MultiWriter(opts.Stderr, taskOutWriter)
	stopCancelTimer := setCancelViaProcessGroup(cmd, envSettings.RunnerCancelGrace)

	runErr := cmd.Run()
	stopCancelTimer()

	if runErr != nil && runCtx.Err() == context.DeadlineExceeded && (ctx == nil || ctx.Err() == nil) {
		runErr = fmt.Errorf("task execution timed out after %s", task.Timeout)
	}

	if runErr == nil {
		if err := checkArtifacts(taskDir, task.Artifacts); err != nil {
			return taskOut.String(), err
		}
	}

	return taskOut.String(), runErr
}

// checkArtifacts verifies that every declared artifact path exists under taskDir.
func checkArtifacts(taskDir string, artifacts []string) error {
	for _, art := range artifacts {
		p := filepath.Join(taskDir, art)
		if _, err := os.Stat(p); err != nil {
			return fmt.Errorf("artifact %q not found", art)
		}
	}
	return nil
}

// taskLoop iterates over ordered nodes, skipping manual/blocked tasks and
// delegating execution to the provided Executor.
func taskLoop(ctx context.Context, ordered []dag.Node, executor Executor, opts Options) ([]TaskResult, error) {
	var results []TaskResult
	resByName := make(map[string]TaskResult, len(ordered))
	record := func(tr TaskResult) {
		results = append(results, tr)
		resByName[tr.Name] = tr
		if opts.OnTaskComplete != nil {
			opts.OnTaskComplete(tr)
		}
	}
	failedTasks := make(map[string]bool)
	allowlistPrinted := false
	for _, node := range ordered {
		task := node.Task
		if ctx.Err() != nil {
			fmt.Fprintf(opts.Stdout, "[SKIP] %s (run canceled)\n", task.Name)
			canceled := domain.NewRunCanceledSkipResult(task.Name)
			failedTasks[task.Name] = true
			record(canceled)
			continue
		}
		blocked, anyCanceled, failedDeps := domain.ScanBlockedDeps(task.DependsOn, func(dep string) (string, *domain.SkipReason, bool) {
			if !failedTasks[dep] {
				return "", nil, false
			}
			r := resByName[dep]
			return r.Status, r.SkipReason, true
		})
		if blocked {
			fmt.Fprintf(opts.Stdout, "[SKIP] %s (dependency failed)\n", task.Name)
			skipped := domain.NewDependencySkipResult(task.Name, failedDeps, anyCanceled)
			failedTasks[task.Name] = true
			record(skipped)
			continue
		}

		waitingForDep := false
		for _, dep := range task.DependsOn {
			if r, ok := resByName[dep]; ok {
				if r.Status == domain.TaskStatusManual || r.Status == domain.TaskStatusPending {
					waitingForDep = true
					break
				}
			}
		}
		if waitingForDep {
			fmt.Fprintf(opts.Stdout, "[PENDING] %s (waiting for dependency)\n", task.Name)
			pending := TaskResult{Name: task.Name, Status: domain.TaskStatusPending}
			record(pending)
			continue
		}

		if task.Manual && !opts.IncludeManual {
			fmt.Fprintf(opts.Stdout, "[MANUAL] %s (waiting for approval)\n", task.Name)
			waiting := TaskResult{Name: task.Name, Status: "manual"}
			record(waiting)
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
			tr.SkipReason = domain.NewRunCanceledSkipReason()
			fmt.Fprintf(opts.Stdout, "[DONE] %s (canceled)\n", task.Name)
			failedTasks[task.Name] = true
			record(tr)
		} else if runErr != nil {
			tr.Status = "failed"
			tr.Error = runErr.Error()
			fmt.Fprintf(opts.Stdout, "[DONE] %s (failed)\n", task.Name)
			failedTasks[task.Name] = true
			record(tr)
		} else {
			tr.Status = "passed"
			fmt.Fprintf(opts.Stdout, "[DONE] %s\n", task.Name)
			record(tr)
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

	nodes, skipped, err := resolvePipeline(project, changed)
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

	executor := newDockerExecutor(workDir, repos, project.DefaultImage)
	results, err := taskLoop(ctx, ordered, executor, opts)
	return append(skipped, results...), err
}

// newDockerExecutor builds a DockerExecutor from the process-wide hardening
// config, defaulting defaultImage to "alpine:latest" when the project sets
// none. Shared by Run() and executorForMode so the two never drift.
func newDockerExecutor(workDir string, repos map[string]model.Repo, defaultImage string) *DockerExecutor {
	if defaultImage == "" {
		defaultImage = "alpine:latest"
	}
	env := config.Current()
	return &DockerExecutor{
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

	nodes, skipped, err := resolvePipeline(project, changed)
	// Validate() above already exercised trigger syntax and cycle
	// detection; ResolveWithSkipped only fails when a task not in the changed-set
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
	results, err := taskLoop(ctx, ordered, executor, opts)
	return append(skipped, results...), err
}

// executorForMode returns the Executor RunOne should use for a manual task
// approval. Only "docker" changes behavior; every other Mode value keeps
// the historical bare `bash -c` execution.
func executorForMode(mode, workDir string, repos map[string]model.Repo, defaultImage string) Executor {
	if mode != "docker" {
		return &LocalExecutor{WorkDir: workDir, Repos: repos}
	}
	return newDockerExecutor(workDir, repos, defaultImage)
}

// RunOne executes a single named task (used for manual job approval). It
// honors opts.Mode so a manual approval on a Docker-mode project gets the
// same container hardening as the rest of the pipeline instead of always
// shelling out on the host.
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
	executor := executorForMode(opts.Mode, workDir, repos, project.DefaultImage)

	if opts.OnTaskStart != nil {
		opts.OnTaskStart(task.Name)
	}

	start := time.Now()
	output, runErr := executor.RunTask(ctx, *task, opts)
	dur := time.Since(start)

	var fatal taskFatal
	if errors.As(runErr, &fatal) {
		return TaskResult{}, fatal
	}

	tr := TaskResult{
		Name:     task.Name,
		Output:   output,
		Duration: dur,
	}
	if runErr != nil && ctx.Err() != nil {
		// The run was canceled while this task was executing: its
		// termination is expected, not a pipeline failure. Mirrors the
		// taskLoop reclassification for DAG tasks.
		tr.Status = "canceled"
		tr.Error = "canceled"
		tr.SkipReason = domain.NewRunCanceledSkipReason()
		fmt.Fprintf(opts.Stdout, "[DONE] %s (canceled)\n", task.Name)
	} else if runErr != nil {
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

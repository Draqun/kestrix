// Package runner executes a resolved MetaCI pipeline on the local machine.
package runner

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"gitlab.com/lunaris-bot/metaci/internal/dag"
	"gitlab.com/lunaris-bot/metaci/internal/model"
	"gitlab.com/lunaris-bot/metaci/internal/variables"
)

// TaskResult captures the outcome of a single task execution.
type TaskResult struct {
	Name     string        `json:"name"`
	Status   string        `json:"status"` // passed, failed, skipped
	Output   string        `json:"output"`
	Error    string        `json:"error,omitempty"`
	Duration time.Duration `json:"duration"`
}

// Options controls a local pipeline run.
type Options struct {
	// WorkDir is the checkout that contains subpath repositories.
	WorkDir string
	// IncludeManual runs tasks marked as manual. They are skipped by default.
	IncludeManual bool
	// Variables provides resolved variables injected as environment variables.
	Variables *variables.Store
	// Stdout and Stderr receive aggregate command output. Nil values discard output.
	Stdout io.Writer
	Stderr io.Writer
}

// Run resolves and executes the tasks selected by changed. Each task runs
// in its own Docker container. Tasks run in dependency order.
func Run(ctx context.Context, project model.Project, changed map[string]bool, opts Options) ([]TaskResult, error) {
	if opts.Stdout == nil {
		opts.Stdout = io.Discard
	}
	if opts.Stderr == nil {
		opts.Stderr = io.Discard
	}
	if err := dag.New(project).Validate(); err != nil {
		return nil, fmt.Errorf("validate pipeline: %w", err)
	}
	if opts.WorkDir == "" {
		opts.WorkDir = "."
	}
	workDir, err := filepath.Abs(opts.WorkDir)
	if err != nil {
		return nil, fmt.Errorf("resolve workdir: %w", err)
	}
	if info, err := os.Stat(workDir); err != nil || !info.IsDir() {
		if err != nil {
			return nil, fmt.Errorf("workdir %q: %w", workDir, err)
		}
		return nil, fmt.Errorf("workdir %q is not a directory", workDir)
	}

	nodes, err := dag.New(project).Resolve(changed)
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

	var results []TaskResult
	for _, node := range ordered {
		task := node.Task
		if task.Manual && !opts.IncludeManual {
			fmt.Fprintf(opts.Stdout, "[SKIP] %s (manual)\n", task.Name)
			results = append(results, TaskResult{Name: task.Name, Status: "skipped"})
			continue
		}
		repo := repos[task.Execute.Repo]
		taskDir, err := repoDir(workDir, repo)
		if err != nil {
			return results, fmt.Errorf("task %q: %w", task.Name, err)
		}

		image := task.Execute.Image
		if image == "" {
			image = defaultImage
		}

		relDir, _ := filepath.Rel(workDir, taskDir)
		if relDir == "" {
			relDir = "."
		}

		fmt.Fprintf(opts.Stdout, "[RUN] %s (image=%s, dir=%s)\n", task.Name, image, relDir)

		// Build docker run command.
		args := []string{
			"run", "--rm",
			"--name", fmt.Sprintf("metaci-%s-%d", task.Name, time.Now().UnixNano()),
			"-v", workDir + ":/workspace",
			"-w", "/workspace/" + relDir,
			"-e", "CI_PROJECT_DIR=/workspace/" + relDir,
		}

		// Inject resolved variables.
		if opts.Variables != nil {
			vars := opts.Variables.Get(task.Execute.Repo, task.Name, "")
			for k, v := range vars {
				args = append(args, "-e", k+"="+v)
			}
		}

		args = append(args, image, "sh", "-c", task.Execute.Script)

		var taskOut bytes.Buffer
		cmd := exec.CommandContext(ctx, "docker", args...)
		cmd.Stdout = io.MultiWriter(opts.Stdout, &taskOut)
		cmd.Stderr = io.MultiWriter(opts.Stderr, &taskOut)

		start := time.Now()
		runErr := cmd.Run()
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
			results = append(results, tr)
			return results, fmt.Errorf("task %q failed: %w", task.Name, runErr)
		}
		tr.Status = "passed"
		fmt.Fprintf(opts.Stdout, "[DONE] %s\n", task.Name)
		results = append(results, tr)
	}
	return results, nil
}

// RunGitLabRunner executes tasks via `gitlab-runner exec`.
func RunGitLabRunner(ctx context.Context, project model.Project, nodes []dag.Node, opts Options, generateYAML func(model.Project, []dag.Node) string) ([]TaskResult, error) {
	if opts.Stdout == nil {
		opts.Stdout = io.Discard
	}
	if opts.Stderr == nil {
		opts.Stderr = io.Discard
	}
	if opts.WorkDir == "" {
		opts.WorkDir = "."
	}
	workDir, err := filepath.Abs(opts.WorkDir)
	if err != nil {
		return nil, fmt.Errorf("resolve workdir: %w", err)
	}

	tmpDir, err := os.MkdirTemp("", "metaci-runner-*")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	yaml := generateYAML(project, nodes)
	ciFile := filepath.Join(tmpDir, ".gitlab-ci.yml")
	if err := os.WriteFile(ciFile, []byte(yaml), 0o644); err != nil {
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

	var results []TaskResult
	for _, node := range ordered {
		task := node.Task
		if task.Manual && !opts.IncludeManual {
			fmt.Fprintf(opts.Stdout, "[SKIP] %s (manual)\n", task.Name)
			results = append(results, TaskResult{Name: task.Name, Status: "skipped"})
			continue
		}

		stage := stageMap[task.Name]
		fmt.Fprintf(opts.Stdout, "[RUN] %s (stage=%s via gitlab-runner exec)\n", task.Name, stage)

		var taskOut bytes.Buffer
		cmd := exec.CommandContext(ctx, "gitlab-runner", "exec", stage, task.Name)
		cmd.Dir = workDir
		cmd.Env = append(os.Environ(), "CI_PROJECT_DIR="+workDir)
		cmd.Stdout = io.MultiWriter(opts.Stdout, &taskOut)
		cmd.Stderr = io.MultiWriter(opts.Stderr, &taskOut)

		if opts.Variables != nil {
			vars := opts.Variables.Get(task.Execute.Repo, task.Name, "")
			for k, v := range vars {
				cmd.Env = append(cmd.Env, k+"="+v)
			}
		}

		start := time.Now()
		runErr := cmd.Run()
		dur := time.Since(start)

		tr := TaskResult{
			Name:     task.Name,
			Output:   taskOut.String(),
			Duration: dur,
		}
		if runErr != nil {
			tr.Status = "failed"
			tr.Error = runErr.Error()
			results = append(results, tr)
			return results, fmt.Errorf("task %q failed (gitlab-runner exec): %w", task.Name, runErr)
		}
		tr.Status = "passed"
		fmt.Fprintf(opts.Stdout, "[DONE] %s\n", task.Name)
		results = append(results, tr)
	}
	return results, nil
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

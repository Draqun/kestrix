// Package runner executes a resolved MetaCI pipeline on the local machine.
package runner

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gitlab.com/lunaris-bot/metaci/internal/dag"
	"gitlab.com/lunaris-bot/metaci/internal/model"
	"gitlab.com/lunaris-bot/metaci/internal/variables"
)

// Options controls a local pipeline run.
type Options struct {
	// WorkDir is the checkout that contains subpath repositories.
	WorkDir string
	// IncludeManual runs tasks marked as manual. They are skipped by default.
	IncludeManual bool
	// Variables provides resolved variables injected as environment variables.
	Variables *variables.Store
	// Stdout and Stderr receive command output. Nil values discard output.
	Stdout io.Writer
	Stderr io.Writer
}

// Run resolves and executes the tasks selected by changed. Tasks run in
// dependency order. A task whose dependency was not selected is rejected:
// running it would not have the same meaning as a CI job with an unmet need.
func Run(ctx context.Context, project model.Project, changed map[string]bool, opts Options) error {
	if opts.Stdout == nil {
		opts.Stdout = io.Discard
	}
	if opts.Stderr == nil {
		opts.Stderr = io.Discard
	}
	if err := dag.New(project).Validate(); err != nil {
		return fmt.Errorf("validate pipeline: %w", err)
	}
	if opts.WorkDir == "" {
		opts.WorkDir = "."
	}
	workDir, err := filepath.Abs(opts.WorkDir)
	if err != nil {
		return fmt.Errorf("resolve workdir: %w", err)
	}
	if info, err := os.Stat(workDir); err != nil || !info.IsDir() {
		if err != nil {
			return fmt.Errorf("workdir %q: %w", workDir, err)
		}
		return fmt.Errorf("workdir %q is not a directory", workDir)
	}

	nodes, err := dag.New(project).Resolve(changed)
	if err != nil {
		return fmt.Errorf("resolve pipeline: %w", err)
	}
	ordered, err := order(nodes)
	if err != nil {
		return err
	}
	repos := make(map[string]model.Repo, len(project.Repos))
	for _, repo := range project.Repos {
		repos[repo.Name] = repo
	}

	for _, node := range ordered {
		task := node.Task
		if task.Manual && !opts.IncludeManual {
			fmt.Fprintf(opts.Stdout, "[SKIP] %s (manual — enable --include-manual to run)\n", task.Name)
			continue
		}
		repo := repos[task.Execute.Repo]
		taskDir, err := repoDir(workDir, repo)
		if err != nil {
			return fmt.Errorf("task %q: %w", task.Name, err)
		}
		fmt.Fprintf(opts.Stdout, "[RUN] %s (%s)\n", task.Name, taskDir)
		cmd := exec.CommandContext(ctx, "sh", "-c", task.Execute.Script)
		cmd.Dir = taskDir
		cmd.Stdout = opts.Stdout
		cmd.Stderr = opts.Stderr

		// Inject resolved variables as environment variables.
		cmd.Env = os.Environ()
		if opts.Variables != nil {
			vars := opts.Variables.Get(task.Execute.Repo, task.Name, "")
			for k, v := range vars {
				cmd.Env = append(cmd.Env, k+"="+v)
			}
		}
		// Also expand ${VAR} references in the script itself.
		if opts.Variables != nil && strings.Contains(task.Execute.Script, "${") {
			vars := opts.Variables.Get(task.Execute.Repo, task.Name, "")
			cmd.Args = []string{"sh", "-c", variables.Expand(task.Execute.Script, vars)}
		}

		if err := cmd.Run(); err != nil {
			return fmt.Errorf("task %q failed: %w", task.Name, err)
		}
		fmt.Fprintf(opts.Stdout, "[DONE] %s\n", task.Name)
	}
	return nil
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

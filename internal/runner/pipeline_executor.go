package runner

import (
	"context"
	"fmt"

	"kestrix/internal/dag"
	"kestrix/internal/domain"
	"kestrix/internal/model"
)

// PipelineExecutor adapts the runner package to the domain PipelineRunner port.
type PipelineExecutor struct {
	Mode         string
	GenerateYAML func(model.Project, []dag.Node) string
	runDocker    func(context.Context, model.Project, map[string]bool, Options) ([]TaskResult, error)
	runLocal     func(context.Context, model.Project, map[string]bool, Options) ([]TaskResult, error)
	runGitLab    func(context.Context, model.Project, []dag.Node, Options, func(model.Project, []dag.Node) string) ([]TaskResult, error)
	runOne       func(context.Context, model.Project, string, Options) (TaskResult, error)
}

var _ domain.PipelineRunner = (*PipelineExecutor)(nil)

// Run selects the configured execution backend and translates domain options.
func (e *PipelineExecutor) Run(ctx context.Context, project domain.Project, changed map[string]bool, opts domain.RunnerOptions) ([]domain.TaskResult, error) {
	runnerOpts := Options{
		WorkDir:        opts.WorkDir,
		IncludeManual:  opts.IncludeManual,
		Variables:      opts.Variables,
		Stdout:         opts.Stdout,
		Stderr:         opts.Stderr,
		OnTaskStart:    opts.OnTaskStart,
		OnTaskComplete: opts.OnTaskComplete,
	}
	switch e.Mode {
	case "local", "":
		if e.runLocal != nil {
			return e.runLocal(ctx, project, changed, runnerOpts)
		}
		return RunLocal(ctx, project, changed, runnerOpts)
	case "docker":
		if e.runDocker != nil {
			return e.runDocker(ctx, project, changed, runnerOpts)
		}
		return Run(ctx, project, changed, runnerOpts)
	case "gitlab-runner":
		if e.GenerateYAML == nil {
			return nil, fmt.Errorf("gitlab-runner mode requires a YAML generator")
		}
		nodes, err := resolveNodes(project, changed)
		if err != nil {
			return nil, err
		}
		if e.runGitLab != nil {
			return e.runGitLab(ctx, project, nodes, runnerOpts, e.GenerateYAML)
		}
		return RunGitLabRunner(ctx, project, nodes, runnerOpts, e.GenerateYAML)
	default:
		return nil, fmt.Errorf("unsupported runner mode %q", e.Mode)
	}
}

// RunTask executes a single named task using the configured backend.
func (e *PipelineExecutor) RunTask(ctx context.Context, project domain.Project, taskName string, opts domain.RunnerOptions) (domain.TaskResult, error) {
	runnerOpts := Options{
		WorkDir:        opts.WorkDir,
		IncludeManual:  true,
		Variables:      opts.Variables,
		Stdout:         opts.Stdout,
		Stderr:         opts.Stderr,
		OnTaskStart:    opts.OnTaskStart,
		OnTaskComplete: opts.OnTaskComplete,
	}
	if e.runOne != nil {
		return e.runOne(ctx, project, taskName, runnerOpts)
	}
	return RunOne(ctx, project, taskName, runnerOpts)
}

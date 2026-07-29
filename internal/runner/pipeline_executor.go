package runner

import (
	"context"
	"fmt"

	"gitlab.com/lunaris-bot/metaci/internal/dag"
	"gitlab.com/lunaris-bot/metaci/internal/domain"
	"gitlab.com/lunaris-bot/metaci/internal/model"
)

// PipelineExecutor adapts the runner package to the domain PipelineRunner port.
type PipelineExecutor struct {
	Mode         string
	GenerateYAML func(model.Project, []dag.Node) string
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
		return RunLocal(ctx, project, changed, runnerOpts)
	case "docker":
		return Run(ctx, project, changed, runnerOpts)
	case "gitlab-runner":
		if e.GenerateYAML == nil {
			return nil, fmt.Errorf("gitlab-runner mode requires a YAML generator")
		}
		nodes, err := dag.New(project).Resolve(changed)
		if err != nil {
			return nil, err
		}
		return RunGitLabRunner(ctx, project, nodes, runnerOpts, e.GenerateYAML)
	default:
		return nil, fmt.Errorf("unsupported runner mode %q", e.Mode)
	}
}

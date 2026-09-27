package runner

import (
	"context"
	"fmt"

	"kestrix/internal/domain"
	"kestrix/internal/model"
)

// PipelineExecutor adapts the runner package to the domain PipelineRunner port.
type PipelineExecutor struct {
	Mode      string
	runDocker func(context.Context, model.Project, map[string]bool, Options) ([]TaskResult, error)
	runLocal  func(context.Context, model.Project, map[string]bool, Options) ([]TaskResult, error)
	runOne    func(context.Context, model.Project, string, Options) (TaskResult, error)
}

var _ domain.PipelineRunner = (*PipelineExecutor)(nil)

// Run selects the configured execution backend and translates domain options.
func (e *PipelineExecutor) Run(ctx context.Context, project domain.Project, changed map[string]bool, opts domain.RunnerOptions) ([]domain.TaskResult, error) {
	runnerOpts := Options{
		WorkDir:               opts.WorkDir,
		IncludeManual:         opts.IncludeManual,
		Variables:             opts.Variables,
		Stdout:                opts.Stdout,
		Stderr:                opts.Stderr,
		OnTaskStart:           opts.OnTaskStart,
		OnTaskComplete:        opts.OnTaskComplete,
		SourceSessions:        opts.SourceSessions,
		SourceAcquirer:        opts.SourceAcquirer,
		CredentialResolver:    opts.CredentialResolver,
		SourceTimeout:         opts.SourceTimeout,
		SourceRetries:         opts.SourceRetries,
		RetainSourceWorkspace: opts.RetainSourceWorkspace,
		OnSourcesAcquired:     opts.OnSourcesAcquired,
		WorkspaceCleanup:      opts.WorkspaceCleanup,
		RepoPaths:             opts.RepoPaths,
		SourcesPrepared:       opts.SourcesPrepared,
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
	default:
		return nil, fmt.Errorf("unsupported runner mode %q", e.Mode)
	}
}

// RunTask executes a single named task using the configured backend.
func (e *PipelineExecutor) RunTask(ctx context.Context, project domain.Project, taskName string, opts domain.RunnerOptions) (domain.TaskResult, error) {
	runnerOpts := Options{
		WorkDir:               opts.WorkDir,
		Mode:                  e.Mode,
		IncludeManual:         true,
		Variables:             opts.Variables,
		Stdout:                opts.Stdout,
		Stderr:                opts.Stderr,
		OnTaskStart:           opts.OnTaskStart,
		OnTaskComplete:        opts.OnTaskComplete,
		SourceSessions:        opts.SourceSessions,
		SourceAcquirer:        opts.SourceAcquirer,
		CredentialResolver:    opts.CredentialResolver,
		SourceTimeout:         opts.SourceTimeout,
		SourceRetries:         opts.SourceRetries,
		RetainSourceWorkspace: opts.RetainSourceWorkspace,
		OnSourcesAcquired:     opts.OnSourcesAcquired,
		WorkspaceCleanup:      opts.WorkspaceCleanup,
		RepoPaths:             opts.RepoPaths,
		SourcesPrepared:       opts.SourcesPrepared,
	}
	if e.runOne != nil {
		return e.runOne(ctx, project, taskName, runnerOpts)
	}
	return RunOne(ctx, project, taskName, runnerOpts)
}

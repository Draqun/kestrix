package web

import (
	"context"

	"kestrix/internal/domain"
	"kestrix/internal/runner"
)

// modeSelectingExecutor replaces the single, instance-wide
// runner.PipelineExecutor Kestrix used to construct once at startup with
// one built fresh per call, keyed on the calling project's own Mode
// (opts.Mode) rather than a fixed cfg.Runner.Mode - see
// remote-runner-registration design.md D2. It is never used for
// opts.Mode == "remote": RunService branches on that before ever reaching
// a PipelineRunner/TaskRunner call (design.md D10), so a "remote" project
// arriving here at all is a bug upstream, not something to route silently -
// runner.PipelineExecutor's own "unsupported runner mode" error covers it.
// A runner.PipelineExecutor is cheap and stateless besides its Mode field,
// so building one per call needs no pooling or caching.
type modeSelectingExecutor struct {
	// defaultMode is used only when a caller leaves opts.Mode empty - every
	// production call site sets it from the project's stored Mode, but
	// direct RunService callers in tests may not.
	defaultMode string
}

var _ domain.PipelineRunner = modeSelectingExecutor{}
var _ domain.TaskRunner = modeSelectingExecutor{}

func (e modeSelectingExecutor) executorFor(opts domain.RunnerOptions) *runner.PipelineExecutor {
	mode := opts.Mode
	if mode == "" {
		mode = e.defaultMode
	}
	return &runner.PipelineExecutor{Mode: mode}
}

func (e modeSelectingExecutor) Run(ctx context.Context, project domain.Project, changed map[string]bool, opts domain.RunnerOptions) ([]domain.TaskResult, error) {
	return e.executorFor(opts).Run(ctx, project, changed, opts)
}

func (e modeSelectingExecutor) RunTask(ctx context.Context, project domain.Project, taskName string, opts domain.RunnerOptions) (domain.TaskResult, error) {
	return e.executorFor(opts).RunTask(ctx, project, taskName, opts)
}

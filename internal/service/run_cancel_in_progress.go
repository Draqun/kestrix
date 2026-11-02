package service

import (
	"context"
	"log/slog"
	"sync"

	"kestrix/internal/domain"
	"kestrix/internal/model"
)

// CancelActorSystemCancelInProgress is recorded as canceled_by when a run
// is automatically superseded on a push to the same branch.
const CancelActorSystemCancelInProgress = "system:cancel-in-progress"

func primarySourceRef(opts domain.RunnerOptions) string {
	for _, src := range opts.InitialSources {
		if src.Primary && src.RequestedRef != "" {
			return src.RequestedRef
		}
	}
	return ""
}

// cancelSupersededRuns finds active and queued runs on the same branch and
// cancels them before the new run starts.
func (s *RunService) cancelSupersededRuns(ctx context.Context, projectName string, proj *model.Project, opts domain.RunnerOptions) {
	if proj == nil || !proj.CancelInProgress || s.runStore == nil {
		return
	}
	ref := primarySourceRef(opts)
	if ref == "" {
		return
	}
	activeRuns, err := s.runStore.FindActiveRunsByRef(projectName, ref)
	if err != nil {
		slog.Warn("could not find active runs to cancel", "project", projectName, "ref", ref, "err", err)
		return
	}
	var wg sync.WaitGroup
	for _, oldRunID := range activeRuns {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			if cancelErr := s.Cancel(ctx, id, CancelActorSystemCancelInProgress); cancelErr != nil {
				slog.Warn("could not cancel superseded run", "run_id", id, "err", cancelErr)
			}
		}(oldRunID)
	}
	wg.Wait()
}

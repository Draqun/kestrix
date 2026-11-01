package web

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"kestrix/internal/domain"
	"kestrix/internal/mocks"
	"kestrix/internal/model"
	"kestrix/internal/service"
)

type webhookBlockingRunner struct {
	started chan int
	release chan struct{}
}

func newWebhookBlockingRunner() *webhookBlockingRunner {
	return &webhookBlockingRunner{
		started: make(chan int, 10),
		release: make(chan struct{}),
	}
}

func (r *webhookBlockingRunner) Run(ctx context.Context, _ domain.Project, _ map[string]bool, opts domain.RunnerOptions) ([]domain.TaskResult, error) {
	if opts.WorkspaceCleanup != nil {
		opts.WorkspaceCleanup()
	}
	r.started <- 1
	select {
	case <-r.release:
		return []domain.TaskResult{{Name: "build", Status: "passed"}}, nil
	case <-ctx.Done():
		return []domain.TaskResult{{Name: "build", Status: "canceled", Error: "canceled"}}, ctx.Err()
	}
}

func (r *webhookBlockingRunner) RunTask(context.Context, domain.Project, string, domain.RunnerOptions) (domain.TaskResult, error) {
	return domain.TaskResult{}, nil
}

type webhookCipStore struct {
	*mocks.MockRunStore
	mu     sync.Mutex
	nextID int
	runs   map[int]*domain.Run
}

func newWebhookCipStore() *webhookCipStore {
	return &webhookCipStore{
		MockRunStore: &mocks.MockRunStore{},
		runs:         make(map[int]*domain.Run),
	}
}

func (s *webhookCipStore) CreateRun(projectName string, changed []string, rerunTask string, includeManual bool, snap *model.Project, startedBy string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	id := s.nextID
	s.runs[id] = &domain.Run{
		ID:          id,
		ProjectName: projectName,
		Status:      "running",
		PipelineDef: snap,
		StartedBy:   startedBy,
		StartedAt:   time.Now(),
	}
	return id, nil
}

func (s *webhookCipStore) UpdateRunSources(runID int, sources []domain.AcquiredSource) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r, ok := s.runs[runID]; ok {
		r.Sources = sources
	}
	return nil
}

func (s *webhookCipStore) CancelRun(id int, canceledBy string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r, ok := s.runs[id]; ok {
		if r.CanceledBy == "" {
			r.CanceledBy = canceledBy
		}
		if r.Status == "running" || r.Status == "queued" {
			r.Status = "canceled"
			return true, nil
		}
	}
	return false, nil
}

func (s *webhookCipStore) SetCanceledBy(id int, canceledBy string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r, ok := s.runs[id]; ok && r.CanceledBy == "" {
		r.CanceledBy = canceledBy
	}
	return nil
}

func (s *webhookCipStore) FinishRun(id int, status, output, errMsg string, trs []domain.TaskResult) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r, ok := s.runs[id]; ok {
		if r.Status != "canceled" {
			r.Status = status
			r.Error = errMsg
			r.TaskResults = trs
		}
	}
	return nil
}

func (s *webhookCipStore) FindActiveRunsByRef(projectName, ref string) ([]int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var ids []int
	for id, r := range s.runs {
		if r.ProjectName == projectName && (r.Status == "running" || r.Status == "queued") {
			for _, src := range r.Sources {
				if (src.Primary || len(r.Sources) == 1) && src.RequestedRef == ref {
					ids = append(ids, id)
					break
				}
			}
		}
	}
	return ids, nil
}

func (s *webhookCipStore) GetRun(id int) (*domain.Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r, ok := s.runs[id]; ok {
		copy := *r
		return &copy, nil
	}
	return nil, nil
}

func TestWebhook_CancelInProgress_ConsecutivePushesCancelEarlierRun(t *testing.T) {
	const cipPipeline = `project "shop" {
  cancel_in_progress = true
  ci_repo "app" {
    path = "app"
  }
  ci_task "build" {
    trigger = "repo.app.changed"
    execute {
      repo = "app"
      script = ":"
    }
  }
}`
	proj := registeredProject(t, "shop", cipPipeline, 0)
	runStore := newWebhookCipStore()
	runner := newWebhookBlockingRunner()

	srv, store, log, sessions := sourcesServer(t, proj)
	_ = store
	_ = log
	_ = sessions

	srv.runStore = runStore
	srv.pipelineRunner = runner
	srv.taskRunner = runner
	srv.runService = service.NewRunService(runStore, runner, runner)
	srv.runService.CancelGrace = 50 * time.Millisecond
	srv.runService.SetLocalExecutionPolicy(true, "local")

	// 1. First push arrives
	body1 := sourcePush(0, []string{proj.RepoURL}, "commit-1", "app/main.go")
	rec1 := postWebhook(t, srv, body1)
	if rec1.Code != http.StatusOK {
		t.Fatalf("push 1: status = %d body = %s", rec1.Code, rec1.Body.String())
	}
	resp1 := decodeBody(t, rec1)
	runID1 := int(resp1["run_id"].(float64))

	// Wait for run 1 to start executing
	select {
	case <-runner.started:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for run 1 to start")
	}

	// 2. Second push arrives on the same branch
	body2 := sourcePush(0, []string{proj.RepoURL}, "commit-2", "app/main.go")
	rec2 := postWebhook(t, srv, body2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("push 2: status = %d body = %s", rec2.Code, rec2.Body.String())
	}
	resp2 := decodeBody(t, rec2)
	runID2 := int(resp2["run_id"].(float64))

	if runID1 == runID2 {
		t.Fatalf("expected different run IDs, got %d for both", runID1)
	}

	// Run 1 should have been canceled
	run1, err := runStore.GetRun(runID1)
	if err != nil || run1 == nil {
		t.Fatalf("GetRun run 1: %v", err)
	}
	if run1.Status != "canceled" {
		t.Errorf("run 1 status = %q, want 'canceled'", run1.Status)
	}
	if run1.CanceledBy != "system:cancel-in-progress" {
		t.Errorf("run 1 canceled_by = %q, want 'system:cancel-in-progress'", run1.CanceledBy)
	}

	// Wait for run 2 to start executing
	select {
	case <-runner.started:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for run 2 to start")
	}

	// Release runner so run 2 can complete
	close(runner.release)

	time.Sleep(50 * time.Millisecond)

	// Run 2 should complete as passed
	run2, err := runStore.GetRun(runID2)
	if err != nil || run2 == nil {
		t.Fatalf("GetRun run 2: %v", err)
	}
	if run2.Status != "passed" {
		t.Errorf("run 2 status = %q, want 'passed'", run2.Status)
	}
}

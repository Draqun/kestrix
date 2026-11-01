package service

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"kestrix/internal/domain"
	"kestrix/internal/mocks"
	"kestrix/internal/model"
)

type cipBlockRunner struct {
	started chan int
	release chan struct{}
}

func newCipBlockRunner() *cipBlockRunner {
	return &cipBlockRunner{
		started: make(chan int, 10),
		release: make(chan struct{}),
	}
}

func (r *cipBlockRunner) Run(ctx context.Context, _ model.Project, _ map[string]bool, opts domain.RunnerOptions) ([]domain.TaskResult, error) {
	r.started <- 1
	select {
	case <-r.release:
		return []domain.TaskResult{{Name: "test", Status: "passed"}}, nil
	case <-ctx.Done():
		return []domain.TaskResult{{Name: "test", Status: "canceled", Error: "canceled"}}, ctx.Err()
	}
}

func (r *cipBlockRunner) RunTask(context.Context, model.Project, string, domain.RunnerOptions) (domain.TaskResult, error) {
	return domain.TaskResult{}, nil
}

type cipStatefulRunStore struct {
	*mocks.MockRunStore
	mu        sync.Mutex
	nextID    int
	runs      map[int]*domain.Run
	cancelLog []struct {
		ID    int
		Actor string
	}
}

func newCipStatefulRunStore() *cipStatefulRunStore {
	return &cipStatefulRunStore{
		MockRunStore: &mocks.MockRunStore{},
		runs:         make(map[int]*domain.Run),
	}
}

func (s *cipStatefulRunStore) CreateRun(projectName string, changed []string, rerunTask string, includeManual bool, snap *model.Project, startedBy string) (int, error) {
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

func (s *cipStatefulRunStore) UpdateRunSources(runID int, sources []domain.AcquiredSource) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r, ok := s.runs[runID]; ok {
		r.Sources = sources
	}
	return nil
}

func (s *cipStatefulRunStore) SetRunQueued(id int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r, ok := s.runs[id]; ok && r.Status != "canceled" && r.Status != "passed" && r.Status != "failed" {
		r.Status = "queued"
	}
	return nil
}

func (s *cipStatefulRunStore) StartQueuedRun(id int) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r, ok := s.runs[id]; ok && r.Status == "queued" {
		r.Status = "running"
		return true, nil
	}
	return false, nil
}

func (s *cipStatefulRunStore) CancelRun(id int, canceledBy string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cancelLog = append(s.cancelLog, struct {
		ID    int
		Actor string
	}{id, canceledBy})
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

func (s *cipStatefulRunStore) SetCanceledBy(id int, canceledBy string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r, ok := s.runs[id]; ok && r.CanceledBy == "" {
		r.CanceledBy = canceledBy
	}
	return nil
}

func (s *cipStatefulRunStore) FinishRun(id int, status, output, errMsg string, trs []domain.TaskResult) error {
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

func (s *cipStatefulRunStore) FindActiveRunsByRef(projectName, ref string) ([]int, error) {
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

func (s *cipStatefulRunStore) GetRun(id int) (*domain.Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r, ok := s.runs[id]; ok {
		copy := *r
		return &copy, nil
	}
	return nil, nil
}

func TestRunService_Start_CancelInProgress_CancelsInFlightRunOnSameBranch(t *testing.T) {
	store := newCipStatefulRunStore()
	runner := newCipBlockRunner()
	svc := NewRunService(store, runner, runner)
	svc.CancelGrace = 50 * time.Millisecond

	proj := &model.Project{
		Name:             "demo",
		CancelInProgress: true,
		Tasks:            []model.Task{{Name: "test"}},
	}

	opts1 := domain.RunnerOptions{
		InitialSources: []domain.AcquiredSource{{
			RepoName:     "demo",
			RequestedRef: "feat/x",
			Primary:      true,
		}},
	}

	runID1, err := svc.Start(context.Background(), "demo", proj, nil, opts1)
	if err != nil {
		t.Fatalf("Start run 1: %v", err)
	}

	// Wait for run 1 to start executing
	select {
	case <-runner.started:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for run 1 to start")
	}

	// Now start run 2 on the same branch
	opts2 := domain.RunnerOptions{
		InitialSources: []domain.AcquiredSource{{
			RepoName:     "demo",
			RequestedRef: "feat/x",
			Primary:      true,
		}},
	}

	runID2, err := svc.Start(context.Background(), "demo", proj, nil, opts2)
	if err != nil {
		t.Fatalf("Start run 2: %v", err)
	}

	// Run 1 should have been canceled by system:cancel-in-progress
	run1, _ := store.GetRun(runID1)
	if run1.Status != "canceled" {
		t.Errorf("run 1 status = %q, want 'canceled'", run1.Status)
	}
	if run1.CanceledBy != "system:cancel-in-progress" {
		t.Errorf("run 1 canceled_by = %q, want 'system:cancel-in-progress'", run1.CanceledBy)
	}

	// Run 2 should be active/running
	if runID2 == runID1 {
		t.Errorf("run 2 ID equals run 1 ID (%d)", runID1)
	}

	// Unblock runner so run 2 can finish
	close(runner.release)
}

func TestRunService_Start_CancelInProgress_CancelsQueuedRunOnSameBranch(t *testing.T) {
	store := newCipStatefulRunStore()
	runner := newCipBlockRunner()
	svc := NewRunService(store, runner, runner)
	svc.SetMaxConcurrentRuns(1)
	svc.CancelGrace = 50 * time.Millisecond

	proj := &model.Project{
		Name:             "demo",
		CancelInProgress: true,
		Tasks:            []model.Task{{Name: "test"}},
	}

	// Run 1 on "main" occupies the only concurrency slot
	opts1 := domain.RunnerOptions{
		InitialSources: []domain.AcquiredSource{{
			RepoName:     "demo",
			RequestedRef: "main",
			Primary:      true,
		}},
	}
	runID1, err := svc.Start(context.Background(), "demo", proj, nil, opts1)
	if err != nil {
		t.Fatalf("Start run 1: %v", err)
	}
	<-runner.started

	// Run 2 on "feat/queue" arrives and is queued because slot is busy
	opts2 := domain.RunnerOptions{
		InitialSources: []domain.AcquiredSource{{
			RepoName:     "demo",
			RequestedRef: "feat/queue",
			Primary:      true,
		}},
	}
	runID2, err := svc.Start(context.Background(), "demo", proj, nil, opts2)
	if err != nil {
		t.Fatalf("Start run 2: %v", err)
	}

	run2, _ := store.GetRun(runID2)
	if run2.Status != "queued" {
		t.Fatalf("run 2 status = %q, want 'queued'", run2.Status)
	}

	// Run 3 on "feat/queue" arrives with CancelInProgress = true
	opts3 := domain.RunnerOptions{
		InitialSources: []domain.AcquiredSource{{
			RepoName:     "demo",
			RequestedRef: "feat/queue",
			Primary:      true,
		}},
	}
	runID3, err := svc.Start(context.Background(), "demo", proj, nil, opts3)
	if err != nil {
		t.Fatalf("Start run 3: %v", err)
	}

	// Run 2 should now be canceled with system:cancel-in-progress without having executed
	run2After, _ := store.GetRun(runID2)
	if run2After.Status != "canceled" {
		t.Errorf("run 2 status = %q, want 'canceled'", run2After.Status)
	}
	if run2After.CanceledBy != "system:cancel-in-progress" {
		t.Errorf("run 2 canceled_by = %q, want 'system:cancel-in-progress'", run2After.CanceledBy)
	}

	// Run 1 should still be running
	run1, _ := store.GetRun(runID1)
	if run1.Status != "running" {
		t.Errorf("run 1 status = %q, want 'running'", run1.Status)
	}

	// Unblock slot, Run 3 should start
	close(runner.release)
	_ = runID3
}

func TestRunService_Start_CancelInProgress_LeavesDifferentBranchesUntouched(t *testing.T) {
	store := newCipStatefulRunStore()
	runner := newCipBlockRunner()
	svc := NewRunService(store, runner, runner)
	svc.CancelGrace = 50 * time.Millisecond

	proj := &model.Project{
		Name:             "demo",
		CancelInProgress: true,
		Tasks:            []model.Task{{Name: "test"}},
	}

	opts1 := domain.RunnerOptions{
		InitialSources: []domain.AcquiredSource{{
			RepoName:     "demo",
			RequestedRef: "feat/branch-a",
			Primary:      true,
		}},
	}
	runID1, err := svc.Start(context.Background(), "demo", proj, nil, opts1)
	if err != nil {
		t.Fatalf("Start run 1: %v", err)
	}
	<-runner.started

	// Run 2 on different branch "feat/branch-b"
	opts2 := domain.RunnerOptions{
		InitialSources: []domain.AcquiredSource{{
			RepoName:     "demo",
			RequestedRef: "feat/branch-b",
			Primary:      true,
		}},
	}
	_, err = svc.Start(context.Background(), "demo", proj, nil, opts2)
	if err != nil {
		t.Fatalf("Start run 2: %v", err)
	}

	// Run 1 should remain running
	run1, _ := store.GetRun(runID1)
	if run1.Status != "running" {
		t.Errorf("run 1 on branch-a was canceled: status = %q", run1.Status)
	}

	close(runner.release)
}

func TestRunService_Start_CancelInProgress_DisabledLeavesEarlierRunsRunning(t *testing.T) {
	store := newCipStatefulRunStore()
	runner := newCipBlockRunner()
	svc := NewRunService(store, runner, runner)
	svc.CancelGrace = 50 * time.Millisecond

	proj := &model.Project{
		Name:             "demo",
		CancelInProgress: false, // disabled
		Tasks:            []model.Task{{Name: "test"}},
	}

	opts1 := domain.RunnerOptions{
		InitialSources: []domain.AcquiredSource{{
			RepoName:     "demo",
			RequestedRef: "feat/same",
			Primary:      true,
		}},
	}
	runID1, err := svc.Start(context.Background(), "demo", proj, nil, opts1)
	if err != nil {
		t.Fatalf("Start run 1: %v", err)
	}
	<-runner.started

	// Run 2 on same branch, but CancelInProgress is false
	opts2 := domain.RunnerOptions{
		InitialSources: []domain.AcquiredSource{{
			RepoName:     "demo",
			RequestedRef: "feat/same",
			Primary:      true,
		}},
	}
	_, err = svc.Start(context.Background(), "demo", proj, nil, opts2)
	if err != nil {
		t.Fatalf("Start run 2: %v", err)
	}

	// Run 1 should remain running
	run1, _ := store.GetRun(runID1)
	if run1.Status != "running" {
		t.Errorf("run 1 was canceled despite cancel_in_progress=false: status = %q", run1.Status)
	}

	close(runner.release)
}

func TestRunService_Start_CancelInProgress_RemoteMode(t *testing.T) {
	mockRun := &mocks.MockRunStore{CreateRunID: 50}

	// Simulate an active run 40 for project on "feat/remote"
	mockRun.FindActiveRunsByRefResult = []int{40}

	svc := NewRunService(mockRun, nil, nil)
	svc.remoteJobStore = &mockRun.MockRemoteJobStore

	proj := &model.Project{
		Name:             "remote-proj",
		CancelInProgress: true,
		Tasks:            []model.Task{{Name: "build", Trigger: "true"}},
	}

	opts := domain.RunnerOptions{
		Mode:    "remote",
		WorkDir: t.TempDir(),
		InitialSources: []domain.AcquiredSource{{
			RepoName:     "remote-proj",
			RequestedRef: "feat/remote",
			Primary:      true,
		}},
	}

	newRunID, err := svc.Start(context.Background(), "remote-proj", proj, nil, opts)
	if err != nil {
		t.Fatalf("Start remote: %v", err)
	}
	if newRunID != 50 {
		t.Errorf("new run ID = %d, want 50", newRunID)
	}

	// Verify FindActiveRunsByRef was called
	if len(mockRun.FindActiveRunsByRefCalls) != 1 {
		t.Fatalf("expected 1 FindActiveRunsByRef call, got %d", len(mockRun.FindActiveRunsByRefCalls))
	}
	call := mockRun.FindActiveRunsByRefCalls[0]
	if call.ProjectName != "remote-proj" || call.Ref != "feat/remote" {
		t.Errorf("unexpected FindActiveRunsByRef call: %+v", call)
	}

	// Verify CancelRun was called for run 40 with system:cancel-in-progress
	if len(mockRun.CancelRunCalls) != 1 {
		t.Fatalf("expected 1 CancelRun call, got %d", len(mockRun.CancelRunCalls))
	}
	if mockRun.CancelRunCalls[0].ID != 40 || mockRun.CancelRunCalls[0].CanceledBy != "system:cancel-in-progress" {
		t.Errorf("unexpected CancelRun call: %+v", mockRun.CancelRunCalls[0])
	}

	// Verify CancelJobsForRun was called for run 40
	if len(mockRun.CancelJobsForRunCalls) != 1 || mockRun.CancelJobsForRunCalls[0] != 40 {
		t.Errorf("unexpected CancelJobsForRun calls: %+v", mockRun.CancelJobsForRunCalls)
	}
}

func TestRunService_Start_CancelInProgress_FindActiveRunsErrorLogged(t *testing.T) {
	mockRun := &mocks.MockRunStore{
		CreateRunID:            55,
		FindActiveRunsByRefErr: fmt.Errorf("db connection down"),
	}
	runner := &mocks.MockPipelineRunner{}
	svc := NewRunService(mockRun, runner, runner)

	proj := &model.Project{
		Name:             "demo",
		CancelInProgress: true,
		Tasks:            []model.Task{{Name: "test"}},
	}
	opts := domain.RunnerOptions{
		InitialSources: []domain.AcquiredSource{{
			RepoName:     "demo",
			RequestedRef: "feat/x",
			Primary:      true,
		}},
	}
	runID, err := svc.Start(context.Background(), "demo", proj, nil, opts)
	if err != nil {
		t.Fatalf("Start error: %v", err)
	}
	if runID != 55 {
		t.Errorf("runID = %d, want 55", runID)
	}
}

func TestRunService_Start_CancelInProgress_CancelErrorLogged(t *testing.T) {
	mockRun := &mocks.MockRunStore{
		CreateRunID:               56,
		FindActiveRunsByRefResult: []int{99},
		CancelRunErr:              fmt.Errorf("cannot cancel"),
	}
	runner := &mocks.MockPipelineRunner{}
	svc := NewRunService(mockRun, runner, runner)

	proj := &model.Project{
		Name:             "demo",
		CancelInProgress: true,
		Tasks:            []model.Task{{Name: "test"}},
	}
	opts := domain.RunnerOptions{
		InitialSources: []domain.AcquiredSource{{
			RepoName:     "demo",
			RequestedRef: "feat/x",
			Primary:      true,
		}},
	}
	runID, err := svc.Start(context.Background(), "demo", proj, nil, opts)
	if err != nil {
		t.Fatalf("Start error: %v", err)
	}
	if runID != 56 {
		t.Errorf("runID = %d, want 56", runID)
	}
}

func TestRunService_Start_CancelInProgress_UpdateRunSourcesErrorLogged(t *testing.T) {
	mockRun := &mocks.MockRunStore{
		CreateRunID:      57,
		UpdateSourcesErr: fmt.Errorf("disk full"),
	}
	runner := &mocks.MockPipelineRunner{}
	svc := NewRunService(mockRun, runner, runner)

	proj := &model.Project{
		Name:             "demo",
		CancelInProgress: true,
		Tasks:            []model.Task{{Name: "test"}},
	}
	opts := domain.RunnerOptions{
		InitialSources: []domain.AcquiredSource{{
			RepoName:     "demo",
			RequestedRef: "feat/x",
			Primary:      true,
		}},
	}
	runID, err := svc.Start(context.Background(), "demo", proj, nil, opts)
	if err != nil {
		t.Fatalf("Start error: %v", err)
	}
	if runID != 57 {
		t.Errorf("runID = %d, want 57", runID)
	}
}

func TestRunService_Start_CancelInProgress_NoPrimarySourceRefIsNoop(t *testing.T) {
	mockRun := &mocks.MockRunStore{CreateRunID: 58}
	runner := &mocks.MockPipelineRunner{}
	svc := NewRunService(mockRun, runner, runner)

	proj := &model.Project{
		Name:             "demo",
		CancelInProgress: true,
		Tasks:            []model.Task{{Name: "test"}},
	}
	// InitialSources without Primary: true or with empty RequestedRef
	opts := domain.RunnerOptions{
		InitialSources: []domain.AcquiredSource{
			{RepoName: "demo", RequestedRef: "", Primary: true},
			{RepoName: "lib", RequestedRef: "main", Primary: false},
		},
	}
	runID, err := svc.Start(context.Background(), "demo", proj, nil, opts)
	if err != nil {
		t.Fatalf("Start error: %v", err)
	}
	if runID != 58 {
		t.Errorf("runID = %d, want 58", runID)
	}
	if len(mockRun.FindActiveRunsByRefCalls) != 0 {
		t.Errorf("expected 0 FindActiveRunsByRef calls, got %d", len(mockRun.FindActiveRunsByRefCalls))
	}
}

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
	"kestrix/internal/variables"
)

// fakeStatefulRunStore is a minimal, real-enough-to-test-against RunStore:
// unlike mocks.MockRunStore's fixed GetRunResult, it actually tracks
// per-run task_results across CreateRun/AppendTaskResult/ApplyTaskResult
// calls, mirroring PGStore's computeRunStatus closely enough for the
// dependency-resumption tests below (tasks.md 3.4/3.6), which exercise a
// real multi-step read-modify-write sequence a fixed-result mock cannot.
type fakeStatefulRunStore struct {
	*mocks.MockRunStore
	mu     sync.Mutex
	nextID int
	runs   map[int]*domain.Run
}

func newFakeStatefulRunStore() *fakeStatefulRunStore {
	return &fakeStatefulRunStore{MockRunStore: &mocks.MockRunStore{}, nextID: 1, runs: make(map[int]*domain.Run)}
}

func (f *fakeStatefulRunStore) CreateRun(projectName string, changed []string, rerunTask string, includeManual bool, snap *model.Project, startedBy string) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := f.nextID
	f.nextID++
	f.runs[id] = &domain.Run{ID: id, ProjectName: projectName, Changed: changed, RerunTask: rerunTask, PipelineDef: snap, Status: "running", StartedBy: startedBy}
	return id, nil
}

func (f *fakeStatefulRunStore) GetRun(id int) (*domain.Run, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.MockRunStore.GetRunErr != nil {
		return nil, f.MockRunStore.GetRunErr
	}
	r, ok := f.runs[id]
	if !ok {
		return nil, nil
	}
	cp := *r
	cp.TaskResults = append([]domain.TaskResult(nil), r.TaskResults...)
	return &cp, nil
}

func (f *fakeStatefulRunStore) AppendTaskResult(runID int, tr domain.TaskResult) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.runs[runID]
	if !ok {
		return fmt.Errorf("no such run %d", runID)
	}
	r.TaskResults = append(r.TaskResults, tr)
	return nil
}

func (f *fakeStatefulRunStore) ApplyTaskResult(runID int, tr domain.TaskResult, outputExtra string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.MockRunStore.ApplyTaskErr != nil {
		return "", f.MockRunStore.ApplyTaskErr
	}
	r, ok := f.runs[runID]
	if !ok {
		return "", fmt.Errorf("no such run %d", runID)
	}
	r.TaskResults = append(r.TaskResults, tr)
	status, _ := computeRunStatusForTest(r.TaskResults)
	r.Status = status
	return status, nil
}

// computeRunStatusForTest mirrors store.computeRunStatus (unexported,
// different package) closely enough for these tests: latest result per
// task name decides the run's overall status.
func computeRunStatusForTest(results []domain.TaskResult) (string, string) {
	latest := make(map[string]domain.TaskResult)
	for _, r := range results {
		latest[r.Name] = r
	}
	status := "passed"
	canExecute := false
	failed := false
	canceled := false
	for _, r := range latest {
		switch r.Status {
		case "manual", "pending", "running":
			canExecute = true
		case "failed":
			failed = true
		case "canceled":
			canceled = true
		}
	}
	if canExecute {
		status = "running"
	} else if canceled {
		status = "canceled"
	} else if failed {
		status = "failed"
	}
	return status, ""
}

func (f *fakeStatefulRunStore) status(runID int) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.runs[runID].Status
}

// fakeRemoteJobStore wraps mocks.MockRemoteJobStore so CompleteJob also
// records the task result on the linked runStore, the way PGStore's real
// CompleteJob does transactionally against runs.task_results - a fixed-mock
// CompleteJobOK alone would leave the completed task's status at "pending"
// forever, so resumeRemoteRun would never see it as resolved.
type fakeRemoteJobStore struct {
	*mocks.MockRemoteJobStore
	runStore *fakeStatefulRunStore
}

func (f *fakeRemoteJobStore) CompleteJob(jobID, epoch int, status, output, taskError string, callerRunnerID *int, maxOutputBytes int) (bool, error) {
	ok, err := f.MockRemoteJobStore.CompleteJob(jobID, epoch, status, output, taskError, callerRunnerID, maxOutputBytes)
	if err != nil || !ok {
		return ok, err
	}
	job := f.MockRemoteJobStore.Jobs[jobID]
	if job != nil {
		_, _ = f.runStore.ApplyTaskResult(job.RunID, domain.TaskResult{Name: job.TaskName, Status: status, Output: output}, "")
	}
	return ok, nil
}

// ── RunService.CompleteRemoteJob / resumeRemoteRun (tasks.md 3.4/3.6) ──

func TestRunService_CompleteRemoteJob_DependentBecomesEligibleAfterCompletion(t *testing.T) {
	proj := &model.Project{Name: "proj", Repos: []domain.Repo{{Name: "app"}}, Tasks: []model.Task{
		{Name: "build", Trigger: "repo.app.changed", Execute: domain.ExecuteSpec{Repo: "app", Script: "echo build"}},
		{Name: "deploy", Trigger: "repo.app.changed", DependsOn: []string{"build"}, Execute: domain.ExecuteSpec{Repo: "app", Script: "echo deploy"}},
	}}
	runStore := newFakeStatefulRunStore()
	remoteJobs := &mocks.MockRemoteJobStore{}
	svc := NewRunService(runStore, &mocks.MockPipelineRunner{}, &mocks.MockPipelineRunner{})
	svc.SetRemoteJobStore(&fakeRemoteJobStore{MockRemoteJobStore: remoteJobs, runStore: runStore})

	runID, err := svc.Start(context.Background(), "proj", proj, []string{"app"}, domain.RunnerOptions{
		Mode: "remote", WorkDir: t.TempDir(), Variables: variables.New(),
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if len(remoteJobs.CreateRemoteJobCalls) != 1 || remoteJobs.CreateRemoteJobCalls[0].TaskName != "build" {
		t.Fatalf("expected only build dispatched initially, got %+v", remoteJobs.CreateRemoteJobCalls)
	}
	buildJobID := 1
	remoteJobs.Jobs = map[int]*domain.RemoteJob{buildJobID: {ID: buildJobID, RunID: runID, TaskName: "build"}}
	remoteJobs.CompleteJobOK = true

	ok, err := svc.CompleteRemoteJob(buildJobID, 1, "passed", "build output", "", nil)
	if err != nil {
		t.Fatalf("CompleteRemoteJob: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true")
	}
	if len(remoteJobs.CreateRemoteJobCalls) != 2 || remoteJobs.CreateRemoteJobCalls[1].TaskName != "deploy" {
		t.Fatalf("expected deploy dispatched after build completed, got %+v", remoteJobs.CreateRemoteJobCalls)
	}
}

func TestRunService_CompleteRemoteJob_NotOKIsNoOpNoResumption(t *testing.T) {
	remoteJobs := &mocks.MockRemoteJobStore{
		Jobs:          map[int]*domain.RemoteJob{1: {ID: 1, RunID: 9, TaskName: "build"}},
		CompleteJobOK: false,
	}
	runStore := newFakeStatefulRunStore()
	svc := NewRunService(runStore, &mocks.MockPipelineRunner{}, &mocks.MockPipelineRunner{})
	svc.SetRemoteJobStore(remoteJobs)

	ok, err := svc.CompleteRemoteJob(1, 1, "passed", "x", "", nil)
	if err != nil {
		t.Fatalf("CompleteRemoteJob: %v", err)
	}
	if ok {
		t.Fatal("expected ok=false to be a no-op")
	}
}

func TestRunService_CompleteRemoteJob_UnknownJobIsNoOp(t *testing.T) {
	remoteJobs := &mocks.MockRemoteJobStore{}
	svc := NewRunService(newFakeStatefulRunStore(), &mocks.MockPipelineRunner{}, &mocks.MockPipelineRunner{})
	svc.SetRemoteJobStore(remoteJobs)
	ok, err := svc.CompleteRemoteJob(999, 1, "passed", "x", "", nil)
	if err != nil {
		t.Fatalf("CompleteRemoteJob: %v", err)
	}
	if ok {
		t.Fatal("expected ok=false for an unknown job")
	}
}

func TestRunService_CompleteRemoteJob_GetRemoteJobErrorPropagates(t *testing.T) {
	remoteJobs := &mocks.MockRemoteJobStore{GetRemoteJobErr: context.DeadlineExceeded}
	svc := NewRunService(newFakeStatefulRunStore(), &mocks.MockPipelineRunner{}, &mocks.MockPipelineRunner{})
	svc.SetRemoteJobStore(remoteJobs)
	if _, err := svc.CompleteRemoteJob(1, 1, "passed", "x", "", nil); err == nil {
		t.Fatal("expected GetRemoteJob's error to propagate")
	}
}

func TestRunService_Start_RemoteMode_SetRemoteRunSnapshotErrorPropagates(t *testing.T) {
	proj := &model.Project{Name: "proj", Repos: []domain.Repo{{Name: "app"}}, Tasks: []model.Task{
		{Name: "a", Trigger: "repo.app.changed", Execute: domain.ExecuteSpec{Repo: "app", Script: "x"}},
	}}
	remoteJobs := &mocks.MockRemoteJobStore{SetRemoteRunSnapshotErr: fmt.Errorf("disk full")}
	svc := NewRunService(newFakeStatefulRunStore(), &mocks.MockPipelineRunner{}, &mocks.MockPipelineRunner{})
	svc.SetRemoteJobStore(remoteJobs)
	if _, err := svc.Start(context.Background(), "proj", proj, []string{"app"}, domain.RunnerOptions{
		Mode: "remote", WorkDir: t.TempDir(), Variables: variables.New(),
	}); err == nil {
		t.Fatal("expected SetRemoteRunSnapshot's error to propagate")
	}
}

func TestRunService_CompleteRemoteJob_ResumptionErrorPropagatesButOKStaysTrue(t *testing.T) {
	remoteJobs := &mocks.MockRemoteJobStore{
		Jobs:          map[int]*domain.RemoteJob{1: {ID: 1, RunID: 9, TaskName: "build"}},
		CompleteJobOK: true,
	}
	// No run 9 registered in the stateful store, but GetRun returning
	// (nil, nil) for a missing run is not itself an error for
	// resumeRemoteRun (it just returns early) - force a real GetRun error
	// via the plain mock's GetRunErr instead, layered under the stateful
	// wrapper's CompleteJob call.
	runStore := newFakeStatefulRunStore()
	runStore.MockRunStore.GetRunErr = fmt.Errorf("connection reset")
	svc := NewRunService(runStore, &mocks.MockPipelineRunner{}, &mocks.MockPipelineRunner{})
	svc.SetRemoteJobStore(remoteJobs)

	ok, err := svc.CompleteRemoteJob(1, 1, "passed", "x", "", nil)
	if err == nil {
		t.Fatal("expected resumeRemoteRun's error to propagate")
	}
	if !ok {
		t.Fatal("expected ok=true: the completion itself succeeded, only resumption failed")
	}
}

func TestRunService_CompleteRemoteJob_CompleteJobErrorPropagates(t *testing.T) {
	remoteJobs := &mocks.MockRemoteJobStore{
		Jobs:           map[int]*domain.RemoteJob{1: {ID: 1, RunID: 1, TaskName: "t"}},
		CompleteJobErr: context.DeadlineExceeded,
	}
	svc := NewRunService(newFakeStatefulRunStore(), &mocks.MockPipelineRunner{}, &mocks.MockPipelineRunner{})
	svc.SetRemoteJobStore(remoteJobs)
	if _, err := svc.CompleteRemoteJob(1, 1, "passed", "x", "", nil); err == nil {
		t.Fatal("expected CompleteJob's error to propagate")
	}
}

// ── ReclaimStaleRemoteJobs (tasks.md 4.1/4.2) ───────────────────────

func TestRunService_ReclaimStaleRemoteJobs_NoRemoteJobStoreIsNoOp(t *testing.T) {
	svc := NewRunService(newFakeStatefulRunStore(), &mocks.MockPipelineRunner{}, &mocks.MockPipelineRunner{})
	reclaimed, failed, err := svc.ReclaimStaleRemoteJobs(time.Minute, 3, "silent")
	if err != nil || reclaimed != 0 || failed != 0 {
		t.Fatalf("ReclaimStaleRemoteJobs = (%d, %d, %v), want (0, 0, nil)", reclaimed, failed, err)
	}
}

func TestRunService_DeleteTerminalRemoteSecrets(t *testing.T) {
	svc := NewRunService(newFakeStatefulRunStore(), &mocks.MockPipelineRunner{}, &mocks.MockPipelineRunner{})
	if err := svc.DeleteTerminalRemoteSecrets(); err != nil {
		t.Fatalf("no store: err = %v, want nil", err)
	}
	remoteJobs := &mocks.MockRemoteJobStore{}
	svc.SetRemoteJobStore(remoteJobs)
	if err := svc.DeleteTerminalRemoteSecrets(); err != nil || remoteJobs.DeleteSecretsCallCount() != 1 {
		t.Fatalf("err = %v, calls = %d; want one delegated call", err, remoteJobs.DeleteSecretsCallCount())
	}
}

func TestRunService_ReclaimStaleRemoteJobs_DelegatesToStore(t *testing.T) {
	remoteJobs := &mocks.MockRemoteJobStore{ReclaimedCount: 2, ReclaimedFailed: 1}
	svc := NewRunService(newFakeStatefulRunStore(), &mocks.MockPipelineRunner{}, &mocks.MockPipelineRunner{})
	svc.SetRemoteJobStore(remoteJobs)
	reclaimed, failed, err := svc.ReclaimStaleRemoteJobs(time.Minute, 3, "silent")
	if err != nil || reclaimed != 2 || failed != 1 {
		t.Fatalf("ReclaimStaleRemoteJobs = (%d, %d, %v), want (2, 1, nil)", reclaimed, failed, err)
	}
}

func TestRunService_ReclaimStaleRemoteJobs_ErrorPropagates(t *testing.T) {
	remoteJobs := &mocks.MockRemoteJobStore{ReclaimErr: context.DeadlineExceeded}
	svc := NewRunService(newFakeStatefulRunStore(), &mocks.MockPipelineRunner{}, &mocks.MockPipelineRunner{})
	svc.SetRemoteJobStore(remoteJobs)
	if _, _, err := svc.ReclaimStaleRemoteJobs(time.Minute, 3, "silent"); err == nil {
		t.Fatal("expected the store's error to propagate")
	}
}

// ── Cancel on a remote-mode run (tasks.md 4.3) ──────────────────────

func TestRunService_Cancel_RemoteModeRun_ReturnsPromptlyAndCancelsJobs(t *testing.T) {
	runStore := &mocks.MockRunStore{}
	remoteJobs := &mocks.MockRemoteJobStore{}
	svc := NewRunService(runStore, &mocks.MockPipelineRunner{}, &mocks.MockPipelineRunner{})
	svc.SetRemoteJobStore(remoteJobs)
	// No trackActive call was ever made for this runID (a remote-mode run
	// never registers one - design.md D10), so Cancel takes the
	// no-active-entry branch, which must return immediately: no
	// CancelGrace+5s wait, no ErrRunCancelTimedOut, even though this test
	// never advances any clock.
	done := make(chan error, 1)
	go func() { done <- svc.Cancel(context.Background(), 42, "user:erin") }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Cancel: %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Cancel did not return promptly for a remote-mode run")
	}
	if len(remoteJobs.CancelJobsForRunCalls) != 1 || remoteJobs.CancelJobsForRunCalls[0] != 42 {
		t.Fatalf("CancelJobsForRunCalls = %v, want [42]", remoteJobs.CancelJobsForRunCalls)
	}
}

func TestRunService_Cancel_RemoteModeRun_CancelJobsErrorPropagates(t *testing.T) {
	runStore := &mocks.MockRunStore{}
	remoteJobs := &mocks.MockRemoteJobStore{CancelJobsForRunErr: context.DeadlineExceeded}
	svc := NewRunService(runStore, &mocks.MockPipelineRunner{}, &mocks.MockPipelineRunner{})
	svc.SetRemoteJobStore(remoteJobs)
	if err := svc.Cancel(context.Background(), 1, "user:erin"); err == nil {
		t.Fatal("expected CancelJobsForRun's error to propagate")
	}
}

func TestRunService_Cancel_NonRemoteRun_NoRemoteJobStoreCallsUnaffected(t *testing.T) {
	// A run with no remoteJobStore configured at all (most existing
	// deployments/tests) must behave exactly as before this field existed.
	runStore := &mocks.MockRunStore{}
	svc := NewRunService(runStore, &mocks.MockPipelineRunner{}, &mocks.MockPipelineRunner{})
	if err := svc.Cancel(context.Background(), 1, "user:erin"); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
}

// ── resumeRemoteRun (tasks.md 3.6) ──────────────────────────────────

func TestResumeRemoteRun_DiamondBothBranchesMustFinishBeforeJoin(t *testing.T) {
	proj := &model.Project{Name: "proj", Repos: []domain.Repo{{Name: "app"}}, Tasks: []model.Task{
		{Name: "root", Trigger: "repo.app.changed", Execute: domain.ExecuteSpec{Repo: "app", Script: "x"}},
		{Name: "left", Trigger: "repo.app.changed", DependsOn: []string{"root"}, Execute: domain.ExecuteSpec{Repo: "app", Script: "x"}},
		{Name: "right", Trigger: "repo.app.changed", DependsOn: []string{"root"}, Execute: domain.ExecuteSpec{Repo: "app", Script: "x"}},
		{Name: "join", Trigger: "repo.app.changed", DependsOn: []string{"left", "right"}, Execute: domain.ExecuteSpec{Repo: "app", Script: "x"}},
	}}
	runStore := newFakeStatefulRunStore()
	remoteJobs := &mocks.MockRemoteJobStore{Jobs: map[int]*domain.RemoteJob{}}
	svc := NewRunService(runStore, &mocks.MockPipelineRunner{}, &mocks.MockPipelineRunner{})
	svc.SetRemoteJobStore(&fakeRemoteJobStore{MockRemoteJobStore: remoteJobs, runStore: runStore})

	runID, err := svc.Start(context.Background(), "proj", proj, []string{"app"}, domain.RunnerOptions{
		Mode: "remote", WorkDir: t.TempDir(), Variables: variables.New(),
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if len(remoteJobs.CreateRemoteJobCalls) != 1 || remoteJobs.CreateRemoteJobCalls[0].TaskName != "root" {
		t.Fatalf("expected only root dispatched, got %+v", remoteJobs.CreateRemoteJobCalls)
	}
	remoteJobs.Jobs[1] = &domain.RemoteJob{ID: 1, RunID: runID, TaskName: "root"}
	remoteJobs.CompleteJobOK = true

	if _, err := svc.CompleteRemoteJob(1, 1, "passed", "", "", nil); err != nil {
		t.Fatalf("complete root: %v", err)
	}
	if len(remoteJobs.CreateRemoteJobCalls) != 3 {
		t.Fatalf("expected left and right dispatched after root, got %+v", remoteJobs.CreateRemoteJobCalls)
	}
	leftDispatched, rightDispatched := false, false
	for _, c := range remoteJobs.CreateRemoteJobCalls[1:] {
		if c.TaskName == "left" {
			leftDispatched = true
		}
		if c.TaskName == "right" {
			rightDispatched = true
		}
	}
	if !leftDispatched || !rightDispatched {
		t.Fatalf("expected both left and right dispatched, got %+v", remoteJobs.CreateRemoteJobCalls)
	}

	// join must not dispatch until BOTH left and right finish.
	remoteJobs.Jobs[2] = &domain.RemoteJob{ID: 2, RunID: runID, TaskName: "left"}
	if _, err := svc.CompleteRemoteJob(2, 1, "passed", "", "", nil); err != nil {
		t.Fatalf("complete left: %v", err)
	}
	if len(remoteJobs.CreateRemoteJobCalls) != 3 {
		t.Fatalf("join must not dispatch with only left done, got %+v", remoteJobs.CreateRemoteJobCalls)
	}

	remoteJobs.Jobs[3] = &domain.RemoteJob{ID: 3, RunID: runID, TaskName: "right"}
	if _, err := svc.CompleteRemoteJob(3, 1, "passed", "", "", nil); err != nil {
		t.Fatalf("complete right: %v", err)
	}
	if len(remoteJobs.CreateRemoteJobCalls) != 4 || remoteJobs.CreateRemoteJobCalls[3].TaskName != "join" {
		t.Fatalf("expected join dispatched once both branches finished, got %+v", remoteJobs.CreateRemoteJobCalls)
	}
}

// TestResumeRemoteRun_ConcurrentCallsNeverDoubleDispatchSharedDependent is
// REVIEW.md R4: two jobs of one diamond DAG (left, right) both required by
// one downstream dependent (join) completing at nearly the same time must
// still dispatch join exactly once, never twice, however the two
// resumeRemoteRun calls interleave.
func TestResumeRemoteRun_ConcurrentCallsNeverDoubleDispatchSharedDependent(t *testing.T) {
	proj := &model.Project{Name: "proj", Repos: []domain.Repo{{Name: "app"}}, Tasks: []model.Task{
		{Name: "root", Trigger: "repo.app.changed", Execute: domain.ExecuteSpec{Repo: "app", Script: "x"}},
		{Name: "left", Trigger: "repo.app.changed", DependsOn: []string{"root"}, Execute: domain.ExecuteSpec{Repo: "app", Script: "x"}},
		{Name: "right", Trigger: "repo.app.changed", DependsOn: []string{"root"}, Execute: domain.ExecuteSpec{Repo: "app", Script: "x"}},
		{Name: "join", Trigger: "repo.app.changed", DependsOn: []string{"left", "right"}, Execute: domain.ExecuteSpec{Repo: "app", Script: "x"}},
	}}
	runStore := newFakeStatefulRunStore()
	remoteJobs := &mocks.MockRemoteJobStore{Jobs: map[int]*domain.RemoteJob{}}
	svc := NewRunService(runStore, &mocks.MockPipelineRunner{}, &mocks.MockPipelineRunner{})
	svc.SetRemoteJobStore(&fakeRemoteJobStore{MockRemoteJobStore: remoteJobs, runStore: runStore})

	runID, err := svc.Start(context.Background(), "proj", proj, []string{"app"}, domain.RunnerOptions{
		Mode: "remote", WorkDir: t.TempDir(), Variables: variables.New(),
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Simulate both branches already reported "passed" - the state two
	// racing CompleteRemoteJob calls would each observe just before calling
	// resumeRemoteRun - without going through the CompleteJob/epoch path,
	// which is not what this test is about.
	for _, name := range []string{"root", "left", "right"} {
		if _, err := runStore.ApplyTaskResult(runID, domain.TaskResult{Name: name, Status: "passed"}, ""); err != nil {
			t.Fatalf("seed %q: %v", name, err)
		}
	}

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- svc.resumeRemoteRun(runID)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("resumeRemoteRun: %v", err)
		}
	}

	joinDispatches := 0
	for _, c := range remoteJobs.CreateRemoteJobCalls {
		if c.TaskName == "join" {
			joinDispatches++
		}
	}
	if joinDispatches != 1 {
		t.Fatalf("join dispatched %d times, want exactly 1 (calls: %+v)", joinDispatches, remoteJobs.CreateRemoteJobCalls)
	}
}

func TestResumeRemoteRun_FailedDependencyLeavesDependentsUndispatched(t *testing.T) {
	proj := &model.Project{Name: "proj", Repos: []domain.Repo{{Name: "app"}}, Tasks: []model.Task{
		{Name: "build", Trigger: "repo.app.changed", Execute: domain.ExecuteSpec{Repo: "app", Script: "x"}},
		{Name: "deploy", Trigger: "repo.app.changed", DependsOn: []string{"build"}, Execute: domain.ExecuteSpec{Repo: "app", Script: "x"}},
	}}
	runStore := newFakeStatefulRunStore()
	remoteJobs := &mocks.MockRemoteJobStore{Jobs: map[int]*domain.RemoteJob{}}
	svc := NewRunService(runStore, &mocks.MockPipelineRunner{}, &mocks.MockPipelineRunner{})
	svc.SetRemoteJobStore(&fakeRemoteJobStore{MockRemoteJobStore: remoteJobs, runStore: runStore})

	runID, err := svc.Start(context.Background(), "proj", proj, []string{"app"}, domain.RunnerOptions{
		Mode: "remote", WorkDir: t.TempDir(), Variables: variables.New(),
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	remoteJobs.Jobs[1] = &domain.RemoteJob{ID: 1, RunID: runID, TaskName: "build"}
	remoteJobs.CompleteJobOK = true

	if _, err := svc.CompleteRemoteJob(1, 1, "failed", "build broke", "", nil); err != nil {
		t.Fatalf("complete build: %v", err)
	}
	if len(remoteJobs.CreateRemoteJobCalls) != 1 {
		t.Fatalf("deploy must never be dispatched once its dependency failed, got %+v", remoteJobs.CreateRemoteJobCalls)
	}
	if got := runStore.status(runID); got != "failed" {
		t.Fatalf("run status = %q, want failed", got)
	}
	run, _ := runStore.GetRun(runID)
	found := false
	for _, tr := range run.TaskResults {
		if tr.Name == "deploy" {
			found = true
			if tr.Status != "skipped" || tr.Error != "dependency failed" {
				t.Fatalf("deploy result = %+v, want skipped/dependency failed", tr)
			}
		}
	}
	if !found {
		t.Fatal("expected a skipped result recorded for deploy")
	}
}

func TestResumeRemoteRun_NoRunOrNoPipelineDefIsNoOp(t *testing.T) {
	// run == nil (never created)
	remoteJobs := &mocks.MockRemoteJobStore{Jobs: map[int]*domain.RemoteJob{1: {ID: 1, RunID: 999, TaskName: "t"}}, CompleteJobOK: true}
	svc := NewRunService(newFakeStatefulRunStore(), &mocks.MockPipelineRunner{}, &mocks.MockPipelineRunner{})
	svc.SetRemoteJobStore(remoteJobs)
	if _, err := svc.CompleteRemoteJob(1, 1, "passed", "", "", nil); err != nil {
		t.Fatalf("expected no error for a run that does not exist: %v", err)
	}
}

func TestResumeRemoteRun_DagResolveErrorPropagates(t *testing.T) {
	runStore := newFakeStatefulRunStore()
	runID, err := runStore.CreateRun("proj", []string{"app"}, "", false, &model.Project{
		Name:  "proj",
		Tasks: []model.Task{{Name: "t", Trigger: "not a valid ( expression"}},
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	remoteJobs := &mocks.MockRemoteJobStore{Jobs: map[int]*domain.RemoteJob{1: {ID: 1, RunID: runID, TaskName: "t"}}, CompleteJobOK: true}
	svc := NewRunService(runStore, &mocks.MockPipelineRunner{}, &mocks.MockPipelineRunner{})
	svc.SetRemoteJobStore(remoteJobs)
	if _, err := svc.CompleteRemoteJob(1, 1, "passed", "", "", nil); err == nil {
		t.Fatal("expected the pipeline resolve error to propagate")
	}
}

func TestResumeRemoteRun_ApplyTaskResultErrorForSkipPropagates(t *testing.T) {
	proj := &model.Project{Name: "proj", Repos: []domain.Repo{{Name: "app"}}, Tasks: []model.Task{
		{Name: "build", Trigger: "repo.app.changed", Execute: domain.ExecuteSpec{Repo: "app", Script: "x"}},
		{Name: "deploy", Trigger: "repo.app.changed", DependsOn: []string{"build"}, Execute: domain.ExecuteSpec{Repo: "app", Script: "x"}},
		{Name: "unrelated", Trigger: "repo.app.changed", Execute: domain.ExecuteSpec{Repo: "app", Script: "x"}},
	}}
	runStore := newFakeStatefulRunStore()
	runID, err := runStore.CreateRun("proj", []string{"app"}, "", false, proj, "")
	if err != nil {
		t.Fatal(err)
	}
	// Seed build's failure directly (bypassing ApplyTaskResult, so the
	// error injected below fires only for the skip this triggers, not for
	// recording build's own result).
	runStore.runs[runID].TaskResults = append(runStore.runs[runID].TaskResults, domain.TaskResult{Name: "build", Status: "failed"})

	remoteJobs := &mocks.MockRemoteJobStore{Jobs: map[int]*domain.RemoteJob{1: {ID: 1, RunID: runID, TaskName: "unrelated"}}, CompleteJobOK: true}
	seedSnapshot(t, remoteJobs, runID)
	svc := NewRunService(runStore, &mocks.MockPipelineRunner{}, &mocks.MockPipelineRunner{})
	svc.SetRemoteJobStore(remoteJobs)
	runStore.MockRunStore.ApplyTaskErr = fmt.Errorf("write failed")

	if _, err := svc.CompleteRemoteJob(1, 1, "passed", "", "", nil); err == nil {
		t.Fatal("expected ApplyTaskResult's error (recording the skipped dependent) to propagate")
	}
}

func TestResumeRemoteRun_GetRemoteRunSnapshotErrorPropagates(t *testing.T) {
	proj := &model.Project{Name: "proj", Repos: []domain.Repo{{Name: "app"}}, Tasks: []model.Task{
		{Name: "build", Trigger: "repo.app.changed", Execute: domain.ExecuteSpec{Repo: "app", Script: "x"}},
		{Name: "deploy", Trigger: "repo.app.changed", DependsOn: []string{"build"}, Execute: domain.ExecuteSpec{Repo: "app", Script: "x"}},
	}}
	runStore := newFakeStatefulRunStore()
	runID, err := runStore.CreateRun("proj", []string{"app"}, "", false, proj, "")
	if err != nil {
		t.Fatal(err)
	}
	remoteJobs := &mocks.MockRemoteJobStore{
		Jobs:                    map[int]*domain.RemoteJob{1: {ID: 1, RunID: runID, TaskName: "build"}},
		CompleteJobOK:           true,
		GetRemoteRunSnapshotErr: fmt.Errorf("read failed"),
	}
	svc := NewRunService(runStore, &mocks.MockPipelineRunner{}, &mocks.MockPipelineRunner{})
	svc.SetRemoteJobStore(remoteJobs)
	if _, err := svc.CompleteRemoteJob(1, 1, "passed", "", "", nil); err == nil {
		t.Fatal("expected GetRemoteRunSnapshot's error to propagate")
	}
}

// The snapshot holds the run's whole source archive, so resumeRemoteRun reads
// it only once a task is due to be dispatched or skipped: completing a job
// that unblocks nothing (a parallel sibling is still running) must not pay
// for it.
func TestResumeRemoteRun_NothingToDispatchDoesNotLoadSnapshot(t *testing.T) {
	spec := domain.ExecuteSpec{Repo: "app", Script: "x"}
	proj := &model.Project{Name: "proj", Repos: []domain.Repo{{Name: "app"}}, Tasks: []model.Task{
		{Name: "lint", Trigger: "repo.app.changed", Execute: spec},
		{Name: "build", Trigger: "repo.app.changed", Execute: spec},
	}}
	runStore := newFakeStatefulRunStore()
	remoteJobs := &mocks.MockRemoteJobStore{Jobs: map[int]*domain.RemoteJob{}, CompleteJobOK: true}
	svc := NewRunService(runStore, &mocks.MockPipelineRunner{}, &mocks.MockPipelineRunner{})
	svc.SetRemoteJobStore(&fakeRemoteJobStore{MockRemoteJobStore: remoteJobs, runStore: runStore})

	runID, err := svc.Start(context.Background(), "proj", proj, []string{"app"}, domain.RunnerOptions{
		Mode: "remote", WorkDir: t.TempDir(), Variables: variables.New(),
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	remoteJobs.Jobs[1] = &domain.RemoteJob{ID: 1, RunID: runID, TaskName: "lint"}

	if _, err := svc.CompleteRemoteJob(1, 1, "passed", "", "", nil); err != nil {
		t.Fatalf("CompleteRemoteJob: %v", err)
	}
	if got := remoteJobs.GetRemoteRunSnapshotCalls; got != 0 {
		t.Fatalf("GetRemoteRunSnapshot called %d times for a completion that dispatches nothing, want 0", got)
	}
	if got := len(remoteJobs.CreateRemoteJobCalls); got != 2 {
		t.Fatalf("jobs created = %d, want only the 2 from Start: %+v", got, remoteJobs.CreateRemoteJobCalls)
	}
}

func TestResumeRemoteRun_CreateRemoteJobErrorPropagates(t *testing.T) {
	proj := &model.Project{Name: "proj", Repos: []domain.Repo{{Name: "app"}}, Tasks: []model.Task{
		{Name: "build", Trigger: "repo.app.changed", Execute: domain.ExecuteSpec{Repo: "app", Script: "x"}},
		{Name: "deploy", Trigger: "repo.app.changed", DependsOn: []string{"build"}, Execute: domain.ExecuteSpec{Repo: "app", Script: "x"}},
	}}
	runStore := newFakeStatefulRunStore()
	runID, err := runStore.CreateRun("proj", []string{"app"}, "", false, proj, "")
	if err != nil {
		t.Fatal(err)
	}
	remoteJobs := &mocks.MockRemoteJobStore{
		Jobs:          map[int]*domain.RemoteJob{1: {ID: 1, RunID: runID, TaskName: "build"}},
		CompleteJobOK: true,
	}
	svc := NewRunService(runStore, &mocks.MockPipelineRunner{}, &mocks.MockPipelineRunner{})
	svc.SetRemoteJobStore(remoteJobs)
	remoteJobs.CreateRemoteJobErr = context.DeadlineExceeded
	seedSnapshot(t, remoteJobs, runID)

	if _, err := svc.CompleteRemoteJob(1, 1, "passed", "", "", nil); err == nil {
		t.Fatal("expected CreateRemoteJob's error (dispatching deploy) to propagate")
	}
}

func TestResumeRemoteRun_ManualTaskNotDispatchedUntilApproved(t *testing.T) {
	proj := &model.Project{Name: "proj", Repos: []domain.Repo{{Name: "app"}}, Tasks: []model.Task{
		{Name: "build", Trigger: "repo.app.changed", Execute: domain.ExecuteSpec{Repo: "app", Script: "x"}},
		{Name: "deploy", Trigger: "repo.app.changed", Manual: true, DependsOn: []string{"build"}, Execute: domain.ExecuteSpec{Repo: "app", Script: "x"}},
	}}
	runStore := newFakeStatefulRunStore()
	remoteJobs := &mocks.MockRemoteJobStore{Jobs: map[int]*domain.RemoteJob{}}
	svc := NewRunService(runStore, &mocks.MockPipelineRunner{}, &mocks.MockPipelineRunner{})
	svc.SetRemoteJobStore(&fakeRemoteJobStore{MockRemoteJobStore: remoteJobs, runStore: runStore})

	runID, err := svc.Start(context.Background(), "proj", proj, []string{"app"}, domain.RunnerOptions{
		Mode: "remote", WorkDir: t.TempDir(), Variables: variables.New(),
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	remoteJobs.Jobs[1] = &domain.RemoteJob{ID: 1, RunID: runID, TaskName: "build"}
	remoteJobs.CompleteJobOK = true

	if _, err := svc.CompleteRemoteJob(1, 1, "passed", "", "", nil); err != nil {
		t.Fatalf("complete build: %v", err)
	}
	for _, c := range remoteJobs.CreateRemoteJobCalls {
		if c.TaskName == "deploy" {
			t.Fatalf("manual task deploy must not be auto-dispatched, got %+v", remoteJobs.CreateRemoteJobCalls)
		}
	}
}

// ── single-task re-runs never resume (remote-run-ux design.md D2) ───────

// seedSnapshot gives runID the snapshot startRemote stores for a whole run;
// resumeRemoteRun resumes only runs that have one (D2).
func seedSnapshot(t *testing.T, remoteJobs *mocks.MockRemoteJobStore, runID int) {
	t.Helper()
	if err := remoteJobs.SetRemoteRunSnapshot(runID, []byte("archive"), nil, nil); err != nil {
		t.Fatal(err)
	}
}

// rerunProject is the pipeline from remote-run-ux's proposal: lint's negated
// trigger holds for any changed set, so resolving the DAG against a re-run's
// task name makes lint (and, once build passes, deploy) eligible.
func rerunProject() *model.Project {
	spec := domain.ExecuteSpec{Repo: "app", Script: "x"}
	return &model.Project{Name: "proj", Repos: []domain.Repo{{Name: "app"}, {Name: "docs"}}, Tasks: []model.Task{
		{Name: "lint", Trigger: "!repo.docs.changed", Execute: spec},
		{Name: "build", Trigger: "!repo.docs.changed", Execute: spec},
		{Name: "deploy", Trigger: "!repo.docs.changed", DependsOn: []string{"build"}, Execute: spec},
	}}
}

// A single-task re-run dispatches one job from its own workspace and stores
// no remote snapshot. Completing that job must not dispatch, skip, or record
// any other task, whatever the re-run's own outcome.
func TestResumeRemoteRun_RunWithoutSnapshotDispatchesNothing(t *testing.T) {
	for _, status := range []string{"passed", "failed"} {
		t.Run(status, func(t *testing.T) {
			runStore := newFakeStatefulRunStore()
			runID, err := runStore.CreateRun("proj", nil, "build", false, rerunProject(), "")
			if err != nil {
				t.Fatal(err)
			}
			remoteJobs := &mocks.MockRemoteJobStore{
				Jobs:          map[int]*domain.RemoteJob{1: {ID: 1, RunID: runID, TaskName: "build"}},
				CompleteJobOK: true,
			}
			svc := NewRunService(runStore, &mocks.MockPipelineRunner{}, &mocks.MockPipelineRunner{})
			svc.SetRemoteJobStore(&fakeRemoteJobStore{MockRemoteJobStore: remoteJobs, runStore: runStore})

			if _, err := svc.CompleteRemoteJob(1, 1, status, "", "", nil); err != nil {
				t.Fatalf("CompleteRemoteJob: %v", err)
			}
			if len(remoteJobs.CreateRemoteJobCalls) != 0 {
				t.Fatalf("no job may be dispatched for a run without a snapshot, got %+v", remoteJobs.CreateRemoteJobCalls)
			}
			run, _ := runStore.GetRun(runID)
			if len(run.TaskResults) != 1 || run.TaskResults[0].Name != "build" {
				t.Fatalf("task results must name only build, got %+v", run.TaskResults)
			}
		})
	}
}

// The guard is about the snapshot, not about who approved what: a whole run
// has one, so approving a manual task still resumes that run's dependents.
func TestResumeRemoteRun_ApprovedManualTaskInWholeRunStillDispatchesDependents(t *testing.T) {
	spec := domain.ExecuteSpec{Repo: "app", Script: "x"}
	proj := &model.Project{Name: "proj", Repos: []domain.Repo{{Name: "app"}}, Tasks: []model.Task{
		{Name: "build", Trigger: "repo.app.changed", Execute: spec},
		{Name: "deploy", Trigger: "repo.app.changed", Manual: true, DependsOn: []string{"build"}, Execute: spec},
		{Name: "notify", Trigger: "repo.app.changed", DependsOn: []string{"deploy"}, Execute: spec},
	}}
	runStore := newFakeStatefulRunStore()
	remoteJobs := &mocks.MockRemoteJobStore{Jobs: map[int]*domain.RemoteJob{}, CompleteJobOK: true}
	svc := NewRunService(runStore, &mocks.MockPipelineRunner{}, &mocks.MockPipelineRunner{})
	svc.SetRemoteJobStore(&fakeRemoteJobStore{MockRemoteJobStore: remoteJobs, runStore: runStore})
	opts := domain.RunnerOptions{Mode: "remote", WorkDir: t.TempDir(), Variables: variables.New()}

	runID, err := svc.Start(context.Background(), "proj", proj, []string{"app"}, opts)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	remoteJobs.Jobs[1] = &domain.RemoteJob{ID: 1, RunID: runID, TaskName: "build"}
	if _, err := svc.CompleteRemoteJob(1, 1, "passed", "", "", nil); err != nil {
		t.Fatalf("complete build: %v", err)
	}

	// Approving deploy dispatches it (job 2); its completion must resume notify.
	if _, dispatched, err := svc.RunTask(context.Background(), runID, "proj", proj, "deploy", opts); err != nil || !dispatched {
		t.Fatalf("RunTask deploy = dispatched %v, err %v", dispatched, err)
	}
	remoteJobs.Jobs[2] = &domain.RemoteJob{ID: 2, RunID: runID, TaskName: "deploy"}
	if _, err := svc.CompleteRemoteJob(2, 1, "passed", "", "", nil); err != nil {
		t.Fatalf("complete deploy: %v", err)
	}
	calls := remoteJobs.CreateRemoteJobCalls
	if len(calls) != 3 || calls[2].TaskName != "notify" {
		t.Fatalf("expected notify dispatched after deploy was approved and passed, got %+v", calls)
	}
}

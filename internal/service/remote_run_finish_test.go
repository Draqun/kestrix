package service

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"kestrix/internal/domain"
	"kestrix/internal/mocks"
	"kestrix/internal/model"
)

// buildThenDeploy is the pipeline the proposal's bug is about: deploy runs
// only after build passed.
func buildThenDeploy() *model.Project {
	spec := domain.ExecuteSpec{Repo: "app", Script: "x"}
	return &model.Project{Name: "proj", Repos: []domain.Repo{{Name: "app"}}, Tasks: []model.Task{
		{Name: "build", Trigger: "repo.app.changed", Execute: spec},
		{Name: "deploy", Trigger: "repo.app.changed", DependsOn: []string{"build"}, Execute: spec},
	}}
}

// A run does not finish when a task does: it finishes when nothing is left
// to dispatch or run. The run stays running while deploy waits for its
// runner, and only becomes passed once deploy itself passed.
func TestRunService_CompleteRemoteJob_RunFinishesOnlyAfterItsDependentsAreDone(t *testing.T) {
	runStore := newFakeStatefulRunStore()
	remoteJobs := &mocks.MockRemoteJobStore{Jobs: map[int]*domain.RemoteJob{}, CompleteJobOK: true}
	svc := NewRunService(runStore, &mocks.MockPipelineRunner{}, &mocks.MockPipelineRunner{})
	svc.SetRemoteJobStore(&fakeRemoteJobStore{MockRemoteJobStore: remoteJobs, runStore: runStore})

	runID, err := svc.Start(context.Background(), "proj", buildThenDeploy(), []string{"app"}, domain.RunnerOptions{
		Mode: "remote", WorkDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	remoteJobs.Jobs[1] = &domain.RemoteJob{ID: 1, RunID: runID, TaskName: "build"}
	if _, err := svc.CompleteRemoteJob(1, 1, "passed", "", "", nil); err != nil {
		t.Fatalf("complete build: %v", err)
	}

	// deploy was dispatched, but it has not run: the run is not finished.
	if len(remoteJobs.CreateRemoteJobCalls) != 2 || remoteJobs.CreateRemoteJobCalls[1].TaskName != "deploy" {
		t.Fatalf("expected deploy dispatched after build passed, got %+v", remoteJobs.CreateRemoteJobCalls)
	}
	if got := runStore.status(runID); got != "running" {
		t.Fatalf("run status = %q, want running while deploy is pending", got)
	}

	remoteJobs.Jobs[2] = &domain.RemoteJob{ID: 2, RunID: runID, TaskName: "deploy"}
	if _, err := svc.CompleteRemoteJob(2, 1, "passed", "", "", nil); err != nil {
		t.Fatalf("complete deploy: %v", err)
	}
	if got := runStore.status(runID); got != "passed" {
		t.Fatalf("run status = %q, want passed once every task passed", got)
	}
}

// A dependent that is blocked and a dependent that became due are decided in
// the same pass: the blocked one is recorded skipped, the other one is
// dispatched. The run must not be finished by the skip - it still has a task
// in flight, so it stays running until that task reports.
func TestRunService_CompleteRemoteJob_SkipDoesNotFinishTheRunBeforeTheDueTaskIsDispatched(t *testing.T) {
	// package waits for lint and build; deploy waits for lint only. When lint
	// passes, package is blocked by the failed build and deploy becomes due -
	// both in the same pass.
	proj := &model.Project{Name: "proj", Repos: []domain.Repo{{Name: "app"}}, Tasks: []model.Task{
		{Name: "build", Trigger: "repo.app.changed", Execute: domain.ExecuteSpec{Repo: "app", Script: "x"}},
		{Name: "lint", Trigger: "repo.app.changed", Execute: domain.ExecuteSpec{Repo: "app", Script: "x"}},
		{Name: "package", Trigger: "repo.app.changed", DependsOn: []string{"lint", "build"}, Execute: domain.ExecuteSpec{Repo: "app", Script: "x"}},
		{Name: "deploy", Trigger: "repo.app.changed", DependsOn: []string{"lint"}, Execute: domain.ExecuteSpec{Repo: "app", Script: "x"}},
	}}
	runStore := newFakeStatefulRunStore()
	remoteJobs := &mocks.MockRemoteJobStore{Jobs: map[int]*domain.RemoteJob{}, CompleteJobOK: true}
	svc := NewRunService(runStore, &mocks.MockPipelineRunner{}, &mocks.MockPipelineRunner{})
	svc.SetRemoteJobStore(&fakeRemoteJobStore{MockRemoteJobStore: remoteJobs, runStore: runStore})

	runID, err := svc.Start(context.Background(), "proj", proj, []string{"app"}, domain.RunnerOptions{
		Mode: "remote", WorkDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	remoteJobs.Jobs[1] = &domain.RemoteJob{ID: 1, RunID: runID, TaskName: "build"}
	remoteJobs.Jobs[2] = &domain.RemoteJob{ID: 2, RunID: runID, TaskName: "lint"}

	// build fails: package still waits for lint, so nothing is decided yet.
	if _, err := svc.CompleteRemoteJob(1, 1, "failed", "build broke", "", nil); err != nil {
		t.Fatalf("complete build: %v", err)
	}
	if got := runStore.status(runID); got != "running" {
		t.Fatalf("run status = %q, want running while lint is pending", got)
	}

	// lint passes: package is now blocked by build (skipped) and deploy
	// became due (dispatched) - in the same pass.
	if _, err := svc.CompleteRemoteJob(2, 1, "passed", "", "", nil); err != nil {
		t.Fatalf("complete lint: %v", err)
	}
	dispatched := false
	for _, c := range remoteJobs.CreateRemoteJobCalls {
		if c.TaskName == "deploy" {
			dispatched = true
		}
	}
	if !dispatched {
		t.Fatalf("expected deploy dispatched after lint passed, got %+v", remoteJobs.CreateRemoteJobCalls)
	}
	if got := runStore.status(runID); got != "running" {
		t.Fatalf("run status = %q, want running: deploy is dispatched and pending", got)
	}
	run, err := runStore.GetRun(runID)
	if err != nil || run == nil {
		t.Fatalf("GetRun = %+v, %v", run, err)
	}
	skipped := false
	for _, tr := range run.TaskResults {
		if tr.Name == "package" && tr.Status == "skipped" {
			skipped = true
		}
	}
	if !skipped {
		t.Fatalf("package was not recorded skipped: %+v", run.TaskResults)
	}

	// deploy reports: nothing is left to do, and the run ends failed -
	// because build failed, not because a skip finalized it.
	remoteJobs.Jobs[3] = &domain.RemoteJob{ID: 3, RunID: runID, TaskName: "deploy"}
	if _, err := svc.CompleteRemoteJob(3, 1, "passed", "", "", nil); err != nil {
		t.Fatalf("complete deploy: %v", err)
	}
	if got := runStore.status(runID); got != "failed" {
		t.Fatalf("run status = %q, want failed", got)
	}
}

// A run canceled between the completion and the walker stays canceled: the
// walker dispatches nothing for a finished run -
// remote-run-waits-for-dependents design.md D4.
func TestResumeRemoteRun_CanceledRunDispatchesNothing(t *testing.T) {
	runStore := newFakeStatefulRunStore()
	remoteJobs := &mocks.MockRemoteJobStore{Jobs: map[int]*domain.RemoteJob{}, CompleteJobOK: true}
	fake := &fakeRemoteJobStore{MockRemoteJobStore: remoteJobs, runStore: runStore}
	svc := NewRunService(runStore, &mocks.MockPipelineRunner{}, &mocks.MockPipelineRunner{})
	svc.SetRemoteJobStore(fake)

	runID, err := svc.Start(context.Background(), "proj", buildThenDeploy(), []string{"app"}, domain.RunnerOptions{
		Mode: "remote", WorkDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	remoteJobs.Jobs[1] = &domain.RemoteJob{ID: 1, RunID: runID, TaskName: "build"}
	// The cancel lands after build's completion was recorded, before the
	// walker runs.
	fake.afterRecord = func() {
		runStore.runs[runID].Status = "canceled"
	}

	if _, err := svc.CompleteRemoteJob(1, 1, "passed", "", "", nil); err != nil {
		t.Fatalf("complete build: %v", err)
	}
	if len(remoteJobs.CreateRemoteJobCalls) != 1 {
		t.Fatalf("no job may be dispatched for a canceled run, got %+v", remoteJobs.CreateRemoteJobCalls)
	}
	if got := runStore.status(runID); got != "canceled" {
		t.Fatalf("run status = %q, want canceled", got)
	}
}

// A dependent that cannot be dispatched is recorded failed, naming the
// failure, and the run ends failed -
// remote-run-waits-for-dependents design.md D5.
func TestResumeRemoteRun_DispatchFailureFailsTheTaskAndTheRun(t *testing.T) {
	runStore := newFakeStatefulRunStore()
	runID, err := runStore.CreateRun("proj", []string{"app"}, "", false, buildThenDeploy(), "")
	if err != nil {
		t.Fatal(err)
	}
	remoteJobs := &mocks.MockRemoteJobStore{
		Jobs:          map[int]*domain.RemoteJob{1: {ID: 1, RunID: runID, TaskName: "build"}},
		CompleteJobOK: true,
	}
	svc := NewRunService(runStore, &mocks.MockPipelineRunner{}, &mocks.MockPipelineRunner{})
	svc.SetRemoteJobStore(&fakeRemoteJobStore{MockRemoteJobStore: remoteJobs, runStore: runStore})
	seedSnapshot(t, remoteJobs, runID)
	remoteJobs.CreateRemoteJobErr = fmt.Errorf("disk full")

	ok, err := svc.CompleteRemoteJob(1, 1, "passed", "", "", nil)
	if err == nil {
		t.Fatal("expected the dispatch failure to propagate")
	}
	if !ok {
		t.Fatal("expected ok=true: the completion itself succeeded, only the dispatch failed")
	}
	if !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("error = %v, want it to name the dispatch failure", err)
	}

	run, err := runStore.GetRun(runID)
	if err != nil || run == nil {
		t.Fatalf("GetRun = %+v, %v", run, err)
	}
	failed := false
	for _, tr := range run.TaskResults {
		if tr.Name == "deploy" && tr.Status == "failed" {
			failed = true
			if !strings.Contains(tr.Error, "disk full") {
				t.Fatalf("deploy's error = %q, want it to name the dispatch failure", tr.Error)
			}
		}
	}
	if !failed {
		t.Fatalf("deploy was not recorded failed: %+v", run.TaskResults)
	}
	if got := runStore.status(runID); got != "failed" {
		t.Fatalf("run status = %q, want failed", got)
	}
}

// A dispatch failure does not abandon the batch: the other task that became
// due is still dispatched, the failed one is recorded failed, and the failed
// one's own dependent is recorded skipped -
// remote-run-waits-for-dependents design.md D5.
func TestResumeRemoteRun_DispatchFailureKeepsTheRestOfTheBatch(t *testing.T) {
	spec := domain.ExecuteSpec{Repo: "app", Script: "x"}
	proj := &model.Project{Name: "proj", Repos: []domain.Repo{{Name: "app"}}, Tasks: []model.Task{
		{Name: "build", Trigger: "repo.app.changed", Execute: spec},
		{Name: "deploy", Trigger: "repo.app.changed", DependsOn: []string{"build"}, Execute: spec},
		{Name: "package", Trigger: "repo.app.changed", DependsOn: []string{"deploy"}, Execute: spec},
		{Name: "notify", Trigger: "repo.app.changed", DependsOn: []string{"build"}, Execute: spec},
	}}
	runStore := newFakeStatefulRunStore()
	runID, err := runStore.CreateRun("proj", []string{"app"}, "", false, proj, "")
	if err != nil {
		t.Fatal(err)
	}
	remoteJobs := &mocks.MockRemoteJobStore{
		Jobs:                  map[int]*domain.RemoteJob{1: {ID: 1, RunID: runID, TaskName: "build"}},
		CompleteJobOK:         true,
		CreateRemoteJobErrFor: map[string]error{"deploy": fmt.Errorf("disk full")},
	}
	svc := NewRunService(runStore, &mocks.MockPipelineRunner{}, &mocks.MockPipelineRunner{})
	svc.SetRemoteJobStore(&fakeRemoteJobStore{MockRemoteJobStore: remoteJobs, runStore: runStore})
	seedSnapshot(t, remoteJobs, runID)

	_, err = svc.CompleteRemoteJob(1, 1, "passed", "", "", nil)
	if err == nil {
		t.Fatal("expected the dispatch failure to propagate")
	}
	if !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("error = %v, want it to name the dispatch failure", err)
	}

	// The batch was not abandoned: notify became due too and was dispatched.
	dispatched := false
	for _, c := range remoteJobs.CreateRemoteJobCalls {
		if c.TaskName == "notify" {
			dispatched = true
		}
	}
	if !dispatched {
		t.Fatalf("expected notify dispatched after build passed, got %+v", remoteJobs.CreateRemoteJobCalls)
	}

	run, err := runStore.GetRun(runID)
	if err != nil || run == nil {
		t.Fatalf("GetRun = %+v, %v", run, err)
	}
	states := map[string]string{}
	for _, tr := range run.TaskResults {
		states[tr.Name] = tr.Status
	}
	if states["deploy"] != "failed" {
		t.Fatalf("deploy = %q, want failed: %+v", states["deploy"], run.TaskResults)
	}
	if states["package"] != "skipped" {
		t.Fatalf("package = %q, want skipped: %+v", states["package"], run.TaskResults)
	}
	// notify is still in flight, so the run is not finished yet.
	if got := runStore.status(runID); got != "running" {
		t.Fatalf("run status = %q, want running while notify is pending", got)
	}
}

// A task whose "pending" record cannot be written is recorded failed
// instead of being dispatched: the record is what tells the next walker
// pass that the task is decided -
// remote-run-waits-for-dependents design.md D5.
func TestResumeRemoteRun_PendingRecordFailureFailsTheTask(t *testing.T) {
	runStore := newFakeStatefulRunStore()
	runID, err := runStore.CreateRun("proj", []string{"app"}, "", false, buildThenDeploy(), "")
	if err != nil {
		t.Fatal(err)
	}
	remoteJobs := &mocks.MockRemoteJobStore{
		Jobs:          map[int]*domain.RemoteJob{1: {ID: 1, RunID: runID, TaskName: "build"}},
		CompleteJobOK: true,
	}
	svc := NewRunService(runStore, &mocks.MockPipelineRunner{}, &mocks.MockPipelineRunner{})
	svc.SetRemoteJobStore(&fakeRemoteJobStore{MockRemoteJobStore: remoteJobs, runStore: runStore})
	seedSnapshot(t, remoteJobs, runID)
	runStore.appendErrForStatus = "pending"

	_, err = svc.CompleteRemoteJob(1, 1, "passed", "", "", nil)
	if err == nil {
		t.Fatal("expected the pending record's error to propagate")
	}
	if !strings.Contains(err.Error(), "record pending task") {
		t.Fatalf("error = %v, want it to name the pending record", err)
	}
	run, err := runStore.GetRun(runID)
	if err != nil || run == nil {
		t.Fatalf("GetRun = %+v, %v", run, err)
	}
	failed := false
	for _, tr := range run.TaskResults {
		if tr.Name == "deploy" && tr.Status == "failed" {
			failed = true
		}
	}
	if !failed {
		t.Fatalf("deploy was not recorded failed: %+v", run.TaskResults)
	}
	if got := runStore.status(runID); got != "failed" {
		t.Fatalf("run status = %q, want failed", got)
	}
}

// When neither the pending record nor the failed record can be written, the
// error names both failures.
func TestResumeRemoteRun_PendingAndFailedRecordErrorsReportBoth(t *testing.T) {
	runStore := newFakeStatefulRunStore()
	runID, err := runStore.CreateRun("proj", []string{"app"}, "", false, buildThenDeploy(), "")
	if err != nil {
		t.Fatal(err)
	}
	remoteJobs := &mocks.MockRemoteJobStore{
		Jobs:          map[int]*domain.RemoteJob{1: {ID: 1, RunID: runID, TaskName: "build"}},
		CompleteJobOK: true,
	}
	svc := NewRunService(runStore, &mocks.MockPipelineRunner{}, &mocks.MockPipelineRunner{})
	svc.SetRemoteJobStore(&fakeRemoteJobStore{MockRemoteJobStore: remoteJobs, runStore: runStore})
	seedSnapshot(t, remoteJobs, runID)
	runStore.MockRunStore.AppendTaskErr = fmt.Errorf("write failed")

	_, err = svc.CompleteRemoteJob(1, 1, "passed", "", "", nil)
	if err == nil {
		t.Fatal("expected the record's error to propagate")
	}
	if !strings.Contains(err.Error(), "record pending task") || !strings.Contains(err.Error(), "record failed task") {
		t.Fatalf("error = %v, want it to name both records", err)
	}
}

// A blocked dependent that cannot be recorded is reported, and the run
// keeps the failure it already has.
func TestResumeRemoteRun_BlockedDependentRecordFailureIsReported(t *testing.T) {
	spec := domain.ExecuteSpec{Repo: "app", Script: "x"}
	proj := &model.Project{Name: "proj", Repos: []domain.Repo{{Name: "app"}}, Tasks: []model.Task{
		{Name: "build", Trigger: "repo.app.changed", Execute: spec},
		{Name: "deploy", Trigger: "repo.app.changed", DependsOn: []string{"build"}, Execute: spec},
		{Name: "package", Trigger: "repo.app.changed", DependsOn: []string{"deploy"}, Execute: spec},
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
	svc.SetRemoteJobStore(&fakeRemoteJobStore{MockRemoteJobStore: remoteJobs, runStore: runStore})
	seedSnapshot(t, remoteJobs, runID)
	remoteJobs.CreateRemoteJobErr = fmt.Errorf("disk full")
	runStore.appendErrForStatus = "skipped"

	_, err = svc.CompleteRemoteJob(1, 1, "passed", "", "", nil)
	if err == nil {
		t.Fatal("expected the dispatch failure to propagate")
	}
	if !strings.Contains(err.Error(), "record blocked task") {
		t.Fatalf("error = %v, want it to name the blocked dependent's record", err)
	}
}

// The run's finishing fails after a dispatch failure: both failures travel.
func TestResumeRemoteRun_DispatchFailureAndFinishFailureReportBoth(t *testing.T) {
	runStore := newFakeStatefulRunStore()
	runID, err := runStore.CreateRun("proj", []string{"app"}, "", false, buildThenDeploy(), "")
	if err != nil {
		t.Fatal(err)
	}
	remoteJobs := &mocks.MockRemoteJobStore{
		Jobs:          map[int]*domain.RemoteJob{1: {ID: 1, RunID: runID, TaskName: "build"}},
		CompleteJobOK: true,
	}
	svc := NewRunService(runStore, &mocks.MockPipelineRunner{}, &mocks.MockPipelineRunner{})
	svc.SetRemoteJobStore(&fakeRemoteJobStore{MockRemoteJobStore: remoteJobs, runStore: runStore})
	seedSnapshot(t, remoteJobs, runID)
	remoteJobs.CreateRemoteJobErr = fmt.Errorf("disk full")
	runStore.MockRunStore.FinishRunIfDoneErr = fmt.Errorf("connection reset")

	_, err = svc.CompleteRemoteJob(1, 1, "passed", "", "", nil)
	if err == nil {
		t.Fatal("expected the dispatch failure to propagate")
	}
	if !strings.Contains(err.Error(), "disk full") || !strings.Contains(err.Error(), "connection reset") {
		t.Fatalf("error = %v, want it to name both failures", err)
	}
}

// When the failed task cannot even be recorded, the error names both
// failures - the dispatch's and the record's.
func TestResumeRemoteRun_DispatchFailureAndFailedRecordReportBoth(t *testing.T) {
	runStore := newFakeStatefulRunStore()
	runID, err := runStore.CreateRun("proj", []string{"app"}, "", false, buildThenDeploy(), "")
	if err != nil {
		t.Fatal(err)
	}
	remoteJobs := &mocks.MockRemoteJobStore{
		Jobs:          map[int]*domain.RemoteJob{1: {ID: 1, RunID: runID, TaskName: "build"}},
		CompleteJobOK: true,
	}
	svc := NewRunService(runStore, &mocks.MockPipelineRunner{}, &mocks.MockPipelineRunner{})
	svc.SetRemoteJobStore(&fakeRemoteJobStore{MockRemoteJobStore: remoteJobs, runStore: runStore})
	seedSnapshot(t, remoteJobs, runID)
	remoteJobs.CreateRemoteJobErr = fmt.Errorf("disk full")
	// The failed record cannot be written; the pending record of the same
	// task can, so the dispatch is really attempted.
	runStore.appendErrForStatus = "failed"

	_, err = svc.CompleteRemoteJob(1, 1, "passed", "", "", nil)
	if err == nil {
		t.Fatal("expected the dispatch failure to propagate")
	}
	if !strings.Contains(err.Error(), "disk full") || !strings.Contains(err.Error(), "write failed") {
		t.Fatalf("error = %v, want it to name both failures", err)
	}
}

// An error that stops the walker before it knows what is due leaves the run
// running: finishing it "passed" would hide the tasks that were never
// dispatched -
// remote-run-waits-for-dependents design.md D5.
func TestResumeRemoteRun_SnapshotErrorLeavesTheRunRunning(t *testing.T) {
	runStore := newFakeStatefulRunStore()
	runID, err := runStore.CreateRun("proj", []string{"app"}, "", false, buildThenDeploy(), "")
	if err != nil {
		t.Fatal(err)
	}
	remoteJobs := &mocks.MockRemoteJobStore{
		Jobs:                    map[int]*domain.RemoteJob{1: {ID: 1, RunID: runID, TaskName: "build"}},
		CompleteJobOK:           true,
		GetRemoteRunSnapshotErr: fmt.Errorf("read failed"),
	}
	svc := NewRunService(runStore, &mocks.MockPipelineRunner{}, &mocks.MockPipelineRunner{})
	svc.SetRemoteJobStore(&fakeRemoteJobStore{MockRemoteJobStore: remoteJobs, runStore: runStore})

	if _, err := svc.CompleteRemoteJob(1, 1, "passed", "", "", nil); err == nil {
		t.Fatal("expected the snapshot error to propagate")
	}
	if got := runStore.status(runID); got != "running" {
		t.Fatalf("run status = %q, want running: a walker error must not finish the run", got)
	}
}

// The run's finishing is the service's last step: its failure is reported,
// but the completion itself already happened.
func TestRunService_CompleteRemoteJob_FinishRunIfDoneErrorPropagates(t *testing.T) {
	runStore := newFakeStatefulRunStore()
	remoteJobs := &mocks.MockRemoteJobStore{
		Jobs:          map[int]*domain.RemoteJob{1: {ID: 1, RunID: 9, TaskName: "build"}},
		CompleteJobOK: true,
	}
	svc := NewRunService(runStore, &mocks.MockPipelineRunner{}, &mocks.MockPipelineRunner{})
	svc.SetRemoteJobStore(remoteJobs)
	runStore.MockRunStore.FinishRunIfDoneErr = fmt.Errorf("connection reset")

	ok, err := svc.CompleteRemoteJob(1, 1, "passed", "x", "", nil)
	if err == nil {
		t.Fatal("expected FinishRunIfDone's error to propagate")
	}
	if !ok {
		t.Fatal("expected ok=true: the completion itself succeeded")
	}
}

// A finishing write that fails once is retried: the completion is already
// recorded, so a lost finishing write would leave the run "running" with
// every task terminal and nothing left to do -
// remote-run-waits-for-dependents design.md D5.
func TestRunService_CompleteRemoteJob_FinishingIsRetried(t *testing.T) {
	runStore := newFakeStatefulRunStore()
	runID, err := runStore.CreateRun("proj", []string{"app"}, "", false, buildThenDeploy(), "")
	if err != nil {
		t.Fatal(err)
	}
	remoteJobs := &mocks.MockRemoteJobStore{
		Jobs:          map[int]*domain.RemoteJob{1: {ID: 1, RunID: runID, TaskName: "build"}},
		CompleteJobOK: true,
	}
	svc := NewRunService(runStore, &mocks.MockPipelineRunner{}, &mocks.MockPipelineRunner{})
	svc.SetRemoteJobStore(&fakeRemoteJobStore{MockRemoteJobStore: remoteJobs, runStore: runStore})
	runStore.MockRunStore.FinishRunIfDoneErr = fmt.Errorf("connection reset")
	runStore.finishTransient = true
	runStore.finishErrsLeft = 1

	ok, err := svc.CompleteRemoteJob(1, 1, "passed", "", "", nil)
	if err != nil {
		t.Fatalf("CompleteRemoteJob: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true")
	}
	if got := runStore.status(runID); got != "passed" {
		t.Fatalf("run status = %q, want passed: the finishing write is retried", got)
	}
}

// A task that already has an active job is dispatched already: the store
// says so with domain.ErrRemoteJobExists, and that is not a failure - the
// task is not recorded failed and the run keeps running.
func TestResumeRemoteRun_AnAlreadyDispatchedTaskIsNotFailed(t *testing.T) {
	runStore := newFakeStatefulRunStore()
	runID, err := runStore.CreateRun("proj", []string{"app"}, "", false, buildThenDeploy(), "")
	if err != nil {
		t.Fatal(err)
	}
	remoteJobs := &mocks.MockRemoteJobStore{
		Jobs:          map[int]*domain.RemoteJob{1: {ID: 1, RunID: runID, TaskName: "build"}},
		CompleteJobOK: true,
	}
	svc := NewRunService(runStore, &mocks.MockPipelineRunner{}, &mocks.MockPipelineRunner{})
	svc.SetRemoteJobStore(&fakeRemoteJobStore{MockRemoteJobStore: remoteJobs, runStore: runStore})
	seedSnapshot(t, remoteJobs, runID)
	remoteJobs.CreateRemoteJobErrFor = map[string]error{"deploy": domain.ErrRemoteJobExists}

	if _, err := svc.CompleteRemoteJob(1, 1, "passed", "", "", nil); err != nil {
		t.Fatalf("an already dispatched task is not a failure: %v", err)
	}
	run, err := runStore.GetRun(runID)
	if err != nil || run == nil {
		t.Fatalf("GetRun = %+v, %v", run, err)
	}
	for _, tr := range run.TaskResults {
		if tr.Name == "deploy" && tr.Status == "failed" {
			t.Fatalf("an already dispatched task must not be recorded failed: %+v", run.TaskResults)
		}
	}
	if got := runStore.status(runID); got != "running" {
		t.Fatalf("run status = %q, want running: deploy is still in flight", got)
	}
}

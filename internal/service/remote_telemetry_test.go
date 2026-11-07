package service

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"kestrix/internal/domain"
	"kestrix/internal/metrics"
	"kestrix/internal/mocks"
	"kestrix/internal/model"
)

func TestRemoteRunTelemetry_LastJobCompletion(t *testing.T) {
	t.Run("passed completion increments passed counter and observes duration", func(t *testing.T) {
		runStore := newFakeStatefulRunStore()
		remoteJobs := &mocks.MockRemoteJobStore{Jobs: map[int]*domain.RemoteJob{}, CompleteJobOK: true}
		svc := NewRunService(runStore, &mocks.MockPipelineRunner{}, &mocks.MockPipelineRunner{})
		fakeJobs := &fakeRemoteJobStore{MockRemoteJobStore: remoteJobs, runStore: runStore}
		svc.SetRemoteJobStore(fakeJobs)
		reg := metrics.NewRegistry()
		svc.Metrics = reg

		runID, err := svc.Start(context.Background(), "proj", buildThenDeploy(), []string{"app"}, domain.RunnerOptions{
			Mode: "remote", WorkDir: t.TempDir(),
		})
		if err != nil {
			t.Fatalf("Start: %v", err)
		}

		// Initial state: 0 completed runs in telemetry
		snap := reg.Snapshot(0, 0, 0, "")
		if count := snap.RunTotal["passed"]["proj"]; count != 0 {
			t.Fatalf("expected 0 passed runs, got %d", count)
		}

		// build passes
		remoteJobs.Jobs[1] = &domain.RemoteJob{ID: 1, RunID: runID, TaskName: "build"}
		if _, err := svc.CompleteRemoteJob(1, 1, "passed", "", "", nil); err != nil {
			t.Fatalf("complete build: %v", err)
		}

		// intermediate job completion leaves run in running state, no completion metrics recorded yet
		snap = reg.Snapshot(0, 0, 0, "")
		if count := snap.RunTotal["passed"]["proj"]; count != 0 {
			t.Fatalf("expected 0 passed runs after intermediate job, got %d", count)
		}

		// deploy passes
		remoteJobs.Jobs[2] = &domain.RemoteJob{ID: 2, RunID: runID, TaskName: "deploy"}
		if _, err := svc.CompleteRemoteJob(2, 1, "passed", "", "", nil); err != nil {
			t.Fatalf("complete deploy: %v", err)
		}

		// Terminal transition reached: telemetry recorded exactly once
		snap = reg.Snapshot(0, 0, 0, "")
		if count := snap.RunTotal["passed"]["proj"]; count != 1 {
			t.Fatalf("expected 1 passed run, got %d", count)
		}
		if snap.DurationCountInf != 1 {
			t.Fatalf("expected duration count 1, got %d", snap.DurationCountInf)
		}
	})

	t.Run("failed completion increments failed counter and observes duration", func(t *testing.T) {
		runStore := newFakeStatefulRunStore()
		remoteJobs := &mocks.MockRemoteJobStore{Jobs: map[int]*domain.RemoteJob{}, CompleteJobOK: true}
		svc := NewRunService(runStore, &mocks.MockPipelineRunner{}, &mocks.MockPipelineRunner{})
		fakeJobs := &fakeRemoteJobStore{MockRemoteJobStore: remoteJobs, runStore: runStore}
		svc.SetRemoteJobStore(fakeJobs)
		reg := metrics.NewRegistry()
		svc.Metrics = reg

		proj := &model.Project{Name: "proj", Repos: []domain.Repo{{Name: "app"}}, Tasks: []model.Task{
			{Name: "test", Trigger: "repo.app.changed", Execute: domain.ExecuteSpec{Repo: "app", Script: "exit 1"}},
		}}

		runID, err := svc.Start(context.Background(), "proj", proj, []string{"app"}, domain.RunnerOptions{
			Mode: "remote", WorkDir: t.TempDir(),
		})
		if err != nil {
			t.Fatalf("Start: %v", err)
		}

		remoteJobs.Jobs[1] = &domain.RemoteJob{ID: 1, RunID: runID, TaskName: "test"}
		if _, err := svc.CompleteRemoteJob(1, 1, "failed", "failed output", "error details", nil); err != nil {
			t.Fatalf("complete test: %v", err)
		}

		snap := reg.Snapshot(0, 0, 0, "")
		if count := snap.RunTotal["failed"]["proj"]; count != 1 {
			t.Fatalf("expected 1 failed run, got %d", count)
		}
		if snap.DurationCountInf != 1 {
			t.Fatalf("expected duration count 1, got %d", snap.DurationCountInf)
		}
	})
}

func TestRemoteRunTelemetry_EarlyAborts(t *testing.T) {
	t.Run("initial dispatch failure records failed metric", func(t *testing.T) {
		runStore := newFakeStatefulRunStore()
		remoteJobs := &mocks.MockRemoteJobStore{
			CreateRemoteJobErr: errors.New("dispatch rejected"),
		}
		svc := NewRunService(runStore, &mocks.MockPipelineRunner{}, &mocks.MockPipelineRunner{})
		fakeJobs := &fakeRemoteJobStore{MockRemoteJobStore: remoteJobs, runStore: runStore}
		svc.SetRemoteJobStore(fakeJobs)
		recorder := &fakeRecorder{}
		svc.Metrics = recorder

		proj := &model.Project{Name: "proj", Repos: []domain.Repo{{Name: "app"}}, Tasks: []model.Task{
			{Name: "task1", Trigger: "repo.app.changed", Execute: domain.ExecuteSpec{Repo: "app", Script: "run"}},
		}}

		_, err := svc.Start(context.Background(), "proj", proj, []string{"app"}, domain.RunnerOptions{
			Mode: "remote", WorkDir: t.TempDir(),
		})
		if err == nil {
			t.Fatal("expected dispatch error")
		}

		runs := recorder.snapshot()
		if len(runs) != 1 {
			t.Fatalf("expected 1 recorded run, got %d (%+v)", len(runs), runs)
		}
		if runs[0].status != "failed" || runs[0].project != "proj" {
			t.Fatalf("unexpected recorded run: %+v", runs[0])
		}
	})

	t.Run("no triggered nodes records passed metric", func(t *testing.T) {
		runStore := newFakeStatefulRunStore()
		remoteJobs := &mocks.MockRemoteJobStore{}
		svc := NewRunService(runStore, &mocks.MockPipelineRunner{}, &mocks.MockPipelineRunner{})
		svc.SetRemoteJobStore(remoteJobs)
		recorder := &fakeRecorder{}
		svc.Metrics = recorder

		proj := &model.Project{Name: "proj", Repos: []domain.Repo{{Name: "app"}, {Name: "other"}}, Tasks: []model.Task{
			{Name: "other", Trigger: "repo.other.changed", Execute: domain.ExecuteSpec{Repo: "app", Script: "run"}},
		}}

		_, err := svc.Start(context.Background(), "proj", proj, []string{"app"}, domain.RunnerOptions{
			Mode: "remote", WorkDir: t.TempDir(),
		})
		if err != nil {
			t.Fatalf("Start: %v", err)
		}

		runs := recorder.snapshot()
		if len(runs) != 1 {
			t.Fatalf("expected 1 recorded run, got %d (%+v)", len(runs), runs)
		}
		if runs[0].status != "passed" || runs[0].project != "proj" {
			t.Fatalf("unexpected recorded run: %+v", runs[0])
		}
	})

	t.Run("workdir archive error records failed metric", func(t *testing.T) {
		runStore := newFakeStatefulRunStore()
		remoteJobs := &mocks.MockRemoteJobStore{}
		svc := NewRunService(runStore, &mocks.MockPipelineRunner{}, &mocks.MockPipelineRunner{})
		svc.SetRemoteJobStore(remoteJobs)
		recorder := &fakeRecorder{}
		svc.Metrics = recorder

		proj := &model.Project{Name: "proj", Repos: []domain.Repo{{Name: "app"}}, Tasks: []model.Task{
			{Name: "task1", Trigger: "repo.app.changed", Execute: domain.ExecuteSpec{Repo: "app", Script: "run"}},
		}}

		_, err := svc.Start(context.Background(), "proj", proj, []string{"app"}, domain.RunnerOptions{
			Mode: "remote", WorkDir: filepath.Join(t.TempDir(), "nonexistent"),
		})
		if err == nil {
			t.Fatal("expected archive error")
		}

		runs := recorder.snapshot()
		if len(runs) != 1 {
			t.Fatalf("expected 1 recorded run, got %d (%+v)", len(runs), runs)
		}
		if runs[0].status != "failed" || runs[0].project != "proj" {
			t.Fatalf("unexpected recorded run: %+v", runs[0])
		}
	})

	t.Run("snapshot persistence error records failed metric", func(t *testing.T) {
		runStore := newFakeStatefulRunStore()
		remoteJobs := &mocks.MockRemoteJobStore{
			SetRemoteRunSnapshotErr: errors.New("snapshot write failed"),
		}
		svc := NewRunService(runStore, &mocks.MockPipelineRunner{}, &mocks.MockPipelineRunner{})
		svc.SetRemoteJobStore(remoteJobs)
		recorder := &fakeRecorder{}
		svc.Metrics = recorder

		proj := &model.Project{Name: "proj", Repos: []domain.Repo{{Name: "app"}}, Tasks: []model.Task{
			{Name: "task1", Trigger: "repo.app.changed", Execute: domain.ExecuteSpec{Repo: "app", Script: "run"}},
		}}

		_, err := svc.Start(context.Background(), "proj", proj, []string{"app"}, domain.RunnerOptions{
			Mode: "remote", WorkDir: t.TempDir(),
		})
		if err == nil {
			t.Fatal("expected snapshot error")
		}

		runs := recorder.snapshot()
		if len(runs) != 1 {
			t.Fatalf("expected 1 recorded run, got %d (%+v)", len(runs), runs)
		}
		if runs[0].status != "failed" || runs[0].project != "proj" {
			t.Fatalf("unexpected recorded run: %+v", runs[0])
		}
	})

	t.Run("append skipped task failure records failed metric", func(t *testing.T) {
		mockStore := &mocks.MockRunStore{
			CreateRunID:    1,
			AppendTaskErr:  errors.New("append failed"),
			FinishRunCalls: nil,
		}
		remoteJobs := &mocks.MockRemoteJobStore{}
		svc := NewRunService(mockStore, &mocks.MockPipelineRunner{}, &mocks.MockPipelineRunner{})
		svc.SetRemoteJobStore(remoteJobs)
		recorder := &fakeRecorder{}
		svc.Metrics = recorder

		proj := &model.Project{Name: "proj", Repos: []domain.Repo{{Name: "app"}, {Name: "other"}}, Tasks: []model.Task{
			{Name: "task1", Trigger: "repo.app.changed", Execute: domain.ExecuteSpec{Repo: "app", Script: "run"}},
			{Name: "skipped_task", Trigger: "repo.other.changed", Execute: domain.ExecuteSpec{Repo: "app", Script: "run"}},
		}}

		_, err := svc.Start(context.Background(), "proj", proj, []string{"app"}, domain.RunnerOptions{
			Mode: "remote", WorkDir: t.TempDir(),
		})
		if err == nil {
			t.Fatal("expected append error")
		}

		runs := recorder.snapshot()
		if len(runs) != 1 {
			t.Fatalf("expected 1 recorded run, got %d (%+v)", len(runs), runs)
		}
		if runs[0].status != "failed" || runs[0].project != "proj" {
			t.Fatalf("unexpected recorded run: %+v", runs[0])
		}
	})
}

func TestRemoteRunTelemetry_Cancellation(t *testing.T) {
	t.Run("cancellation records canceled metric exactly once", func(t *testing.T) {
		runStore := newFakeStatefulRunStore()
		remoteJobs := &mocks.MockRemoteJobStore{Jobs: map[int]*domain.RemoteJob{}, CompleteJobOK: true}
		svc := NewRunService(runStore, &mocks.MockPipelineRunner{}, &mocks.MockPipelineRunner{})
		fakeJobs := &fakeRemoteJobStore{MockRemoteJobStore: remoteJobs, runStore: runStore}
		svc.SetRemoteJobStore(fakeJobs)
		reg := metrics.NewRegistry()
		svc.Metrics = reg

		runID, err := svc.Start(context.Background(), "proj", buildThenDeploy(), []string{"app"}, domain.RunnerOptions{
			Mode: "remote", WorkDir: t.TempDir(),
		})
		if err != nil {
			t.Fatalf("Start: %v", err)
		}

		// Cancel the remote run
		if err := svc.Cancel(context.Background(), runID, "alice"); err != nil {
			t.Fatalf("Cancel: %v", err)
		}

		snap := reg.Snapshot(0, 0, 0, "")
		if count := snap.RunTotal["canceled"]["proj"]; count != 1 {
			t.Fatalf("expected 1 canceled run, got %d", count)
		}

		// Second cancel attempt does not increment metric again
		if err := svc.Cancel(context.Background(), runID, "alice"); err != nil {
			t.Fatalf("second Cancel: %v", err)
		}

		snap = reg.Snapshot(0, 0, 0, "")
		if count := snap.RunTotal["canceled"]["proj"]; count != 1 {
			t.Fatalf("expected still 1 canceled run after second cancel, got %d", count)
		}

		// A lagging runner completing a job on the canceled run does not record metric
		remoteJobs.Jobs[1] = &domain.RemoteJob{ID: 1, RunID: runID, TaskName: "build"}
		_, _ = svc.CompleteRemoteJob(1, 1, "passed", "", "", nil)

		snap = reg.Snapshot(0, 0, 0, "")
		if count := snap.RunTotal["canceled"]["proj"]; count != 1 {
			t.Fatalf("expected still 1 canceled run after late complete, got %d", count)
		}
		if count := snap.RunTotal["passed"]["proj"]; count != 0 {
			t.Fatalf("expected 0 passed runs, got %d", count)
		}
	})
}

func TestRemoteRunTelemetry_FinalizeRemoteRun_Coverage(t *testing.T) {
	// 1. runStore == nil
	rec1 := &fakeRecorder{}
	svcNilStore := &RunService{Metrics: rec1}
	svcNilStore.finalizeRemoteRun(1)
	if runs := rec1.snapshot(); len(runs) != 0 {
		t.Fatalf("expected 0 recorded runs when store is nil, got %d", len(runs))
	}

	// 2. GetRun error
	rec2 := &fakeRecorder{}
	mockStoreErr := &mocks.MockRunStore{GetRunErr: errors.New("db error")}
	svcErr := NewRunService(mockStoreErr, nil, nil)
	svcErr.Metrics = rec2
	svcErr.finalizeRemoteRun(1)
	if runs := rec2.snapshot(); len(runs) != 0 {
		t.Fatalf("expected 0 recorded runs on GetRun error, got %d", len(runs))
	}

	// 3. GetRun returns nil
	rec3 := &fakeRecorder{}
	mockStoreNilRun := &mocks.MockRunStore{GetRunResult: nil}
	svcNilRun := NewRunService(mockStoreNilRun, nil, nil)
	svcNilRun.Metrics = rec3
	svcNilRun.finalizeRemoteRun(1)
	if runs := rec3.snapshot(); len(runs) != 0 {
		t.Fatalf("expected 0 recorded runs on nil run, got %d", len(runs))
	}

	// 4. Non-terminal run status ("running")
	rec4 := &fakeRecorder{}
	mockStoreRunning := &mocks.MockRunStore{GetRunResult: &domain.Run{Status: "running"}}
	svcRunning := NewRunService(mockStoreRunning, nil, nil)
	svcRunning.Metrics = rec4
	svcRunning.finalizeRemoteRun(1)
	if runs := rec4.snapshot(); len(runs) != 0 {
		t.Fatalf("expected 0 recorded runs for non-terminal run, got %d", len(runs))
	}

	// 5. Canceled run status ("canceled") - asserts no metric recorded and no state published
	rec5 := &fakeRecorder{}
	hub := NewRunEventHub()
	ch := hub.Subscribe(1)
	defer hub.Unsubscribe(1, ch)
	mockStoreCanceled := &mocks.MockRunStore{GetRunResult: &domain.Run{Status: "canceled"}}
	svcCanceled := NewRunService(mockStoreCanceled, nil, nil)
	svcCanceled.SetEventHub(hub)
	svcCanceled.Metrics = rec5
	svcCanceled.finalizeRemoteRun(1)
	if runs := rec5.snapshot(); len(runs) != 0 {
		t.Fatalf("expected 0 recorded runs for canceled run, got %d", len(runs))
	}
	receivedEvents := 0
	select {
	case <-ch:
		receivedEvents++
	default:
	}
	if receivedEvents != 0 {
		t.Fatalf("expected no events published for canceled run, got %d", receivedEvents)
	}

	// 6. Terminal run with zero StartedAt clamps to time.Now()
	rec := &fakeRecorder{}
	mockStoreZeroTime := &mocks.MockRunStore{
		GetRunResult: &domain.Run{
			ID:          99,
			ProjectName: "proj",
			Status:      "passed",
			StartedAt:   time.Time{},
		},
	}
	svcZeroTime := NewRunService(mockStoreZeroTime, nil, nil)
	svcZeroTime.Metrics = rec
	svcZeroTime.finalizeRemoteRun(99)

	runs := rec.snapshot()
	if len(runs) != 1 || runs[0].status != "passed" {
		t.Fatalf("expected 1 recorded passed run, got %+v", runs)
	}

	// 7. recordRunFinished with zero time and Metrics == nil
	svcNoMetrics := &RunService{}
	svcNoMetrics.recordRunFinished("passed", "proj", time.Time{}) // should not panic
}

type interleavingRunStore struct {
	*fakeStatefulRunStore
	onAppend func(runID int, tr domain.TaskResult)
}

func (s *interleavingRunStore) AppendTaskResult(runID int, tr domain.TaskResult) error {
	err := s.fakeStatefulRunStore.AppendTaskResult(runID, tr)
	if s.onAppend != nil {
		s.onAppend(runID, tr)
	}
	return err
}

func TestRemoteRunTelemetry_Interleaving_DispatchFailureAndJobCompletion(t *testing.T) {
	statefulStore := newFakeStatefulRunStore()
	remoteJobs := &mocks.MockRemoteJobStore{
		Jobs:                  map[int]*domain.RemoteJob{},
		CompleteJobOK:         true,
		CreateRemoteJobErrFor: map[string]error{"task2": errors.New("cannot create task2")},
	}
	var svc *RunService
	store := &interleavingRunStore{
		fakeStatefulRunStore: statefulStore,
		onAppend: func(runID int, tr domain.TaskResult) {
			// When task2's dispatch failure is recorded, simulate runner completing task1
			if tr.Name == "task2" && tr.Status == domain.TaskStatusFailed {
				remoteJobs.Jobs[1] = &domain.RemoteJob{ID: 1, RunID: runID, TaskName: "task1"}
				_, _ = svc.CompleteRemoteJob(1, 1, "passed", "", "", nil)
			}
		},
	}
	svc = NewRunService(store, &mocks.MockPipelineRunner{}, &mocks.MockPipelineRunner{})
	fakeJobs := &fakeRemoteJobStore{MockRemoteJobStore: remoteJobs, runStore: statefulStore}
	svc.SetRemoteJobStore(fakeJobs)
	recorder := &fakeRecorder{}
	svc.Metrics = recorder

	proj := &model.Project{Name: "proj", Repos: []domain.Repo{{Name: "app"}}, Tasks: []model.Task{
		{Name: "task1", Trigger: "repo.app.changed", Execute: domain.ExecuteSpec{Repo: "app", Script: "run1"}},
		{Name: "task2", Trigger: "repo.app.changed", Execute: domain.ExecuteSpec{Repo: "app", Script: "run2"}},
	}}

	_, err := svc.Start(context.Background(), "proj", proj, []string{"app"}, domain.RunnerOptions{
		Mode: "remote", WorkDir: t.TempDir(),
	})
	if err == nil {
		t.Fatal("expected dispatch error for task2")
	}

	// Verify that the run was counted exactly once (not twice)
	runs := recorder.snapshot()
	if len(runs) != 1 {
		t.Fatalf("expected exactly 1 recorded run, got %d: %+v", len(runs), runs)
	}
	if runs[0].status != "failed" || runs[0].project != "proj" {
		t.Fatalf("unexpected recorded run: %+v", runs[0])
	}
}

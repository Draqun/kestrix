package service

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
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

	t.Run("initial dispatch failure getrun error skips finalize metric", func(t *testing.T) {
		baseStore := newFakeStatefulRunStore()
		getRunErrStore := &getRunErrorStore{fakeStatefulRunStore: baseStore, errOnGetRun: errors.New("db error")}
		remoteJobs := &mocks.MockRemoteJobStore{
			CreateRemoteJobErr: errors.New("dispatch rejected"),
		}
		svc := NewRunService(getRunErrStore, &mocks.MockPipelineRunner{}, &mocks.MockPipelineRunner{})
		fakeJobs := &fakeRemoteJobStore{MockRemoteJobStore: remoteJobs, runStore: baseStore}
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
		if len(runs) != 0 {
			t.Fatalf("expected 0 recorded runs when GetRun fails, got %d", len(runs))
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

type getRunErrorStore struct {
	*fakeStatefulRunStore
	errOnGetRun error
}

func (s *getRunErrorStore) GetRun(id int) (*domain.Run, error) {
	if s.errOnGetRun != nil {
		return nil, s.errOnGetRun
	}
	return s.fakeStatefulRunStore.GetRun(id)
}

func assertBothEvents(t *testing.T, ch <-chan RunEvent, wantRunID int, wantStatus string) {
	t.Helper()
	receivedStatus, receivedComplete := false, false
	for i := 0; i < 2; i++ {
		select {
		case ev := <-ch:
			if ev.Type == EventTypeStatus {
				if payload, ok := ev.Data.(StatusPayload); ok && payload.RunID == wantRunID && payload.Status == wantStatus {
					receivedStatus = true
				}
			} else if ev.Type == EventTypeComplete {
				if payload, ok := ev.Data.(CompletePayload); ok && payload.RunID == wantRunID && payload.Status == wantStatus {
					receivedComplete = true
				}
			}
		default:
		}
	}
	if !receivedStatus || !receivedComplete {
		t.Fatalf("run %d: expected both status and complete events on EventHub, got status=%v complete=%v", wantRunID, receivedStatus, receivedComplete)
	}
}

func TestRemoteRunTelemetry_ReclaimStaleJobs(t *testing.T) {
	t.Run("stale job reclaim finishing run records metric and broadcasts event", func(t *testing.T) {
		runStore := newFakeStatefulRunStore()
		runID, err := runStore.CreateRun("proj", nil, "", false, nil, "", nil)
		if err != nil {
			t.Fatalf("CreateRun: %v", err)
		}
		_ = runStore.AppendTaskResult(runID, domain.TaskResult{Name: "task1", Status: "failed", Error: "runner went silent"})
		_ = runStore.FinishRun(runID, domain.RunStatusFailed, "", "runner went silent", nil)

		remoteJobs := &mocks.MockRemoteJobStore{
			ReclaimedCount:  0,
			ReclaimedFailed: 1,
			ReclaimedRunIDs: []int{runID},
		}

		svc := NewRunService(runStore, &mocks.MockPipelineRunner{}, &mocks.MockPipelineRunner{})
		svc.SetRemoteJobStore(remoteJobs)
		reg := metrics.NewRegistry()
		svc.Metrics = reg

		hub := NewRunEventHub()
		ch := hub.Subscribe(runID)
		defer hub.Unsubscribe(runID, ch)
		svc.SetEventHub(hub)

		reclaimed, failed, err := svc.ReclaimStaleRemoteJobs(time.Minute, 1, "runner went silent")
		if err != nil {
			t.Fatalf("ReclaimStaleRemoteJobs: %v", err)
		}
		if reclaimed != 0 || failed != 1 {
			t.Fatalf("reclaimed=%d failed=%d, want 0, 1", reclaimed, failed)
		}

		snap := reg.Snapshot(0, 0, 0, "")
		if count := snap.RunTotal["failed"]["proj"]; count != 1 {
			t.Fatalf("expected 1 failed run metric recorded, got %d", count)
		}
		if snap.DurationCountInf != 1 {
			t.Fatalf("expected 1 duration recorded, got %d", snap.DurationCountInf)
		}

		assertBothEvents(t, ch, runID, domain.RunStatusFailed)
	})

	t.Run("stale job reclaim returning to pending does not record telemetry", func(t *testing.T) {
		runStore := newFakeStatefulRunStore()
		runID, err := runStore.CreateRun("proj", nil, "", false, nil, "", nil)
		if err != nil {
			t.Fatalf("CreateRun: %v", err)
		}
		run, _ := runStore.GetRun(runID)
		run.Status = domain.RunStatusRunning

		remoteJobs := &mocks.MockRemoteJobStore{
			ReclaimedCount:  1,
			ReclaimedFailed: 0,
			ReclaimedRunIDs: nil,
		}

		svc := NewRunService(runStore, &mocks.MockPipelineRunner{}, &mocks.MockPipelineRunner{})
		svc.SetRemoteJobStore(remoteJobs)
		reg := metrics.NewRegistry()
		svc.Metrics = reg

		hub := NewRunEventHub()
		ch := hub.Subscribe(runID)
		defer hub.Unsubscribe(runID, ch)
		svc.SetEventHub(hub)

		reclaimed, failed, err := svc.ReclaimStaleRemoteJobs(time.Minute, 2, "runner went silent")
		if err != nil {
			t.Fatalf("ReclaimStaleRemoteJobs: %v", err)
		}
		if reclaimed != 1 || failed != 0 {
			t.Fatalf("reclaimed=%d failed=%d, want 1, 0", reclaimed, failed)
		}

		snap := reg.Snapshot(0, 0, 0, "")
		if count := snap.RunTotal["failed"]["proj"]; count != 0 {
			t.Fatalf("expected 0 failed run metric recorded, got %d", count)
		}
		if snap.DurationCountInf != 0 {
			t.Fatalf("expected 0 duration recorded, got %d", snap.DurationCountInf)
		}

		select {
		case ev := <-ch:
			t.Fatalf("unexpected event received: %+v", ev)
		default:
		}
	})

	t.Run("multiple finished runs each finalize telemetry and broadcast events", func(t *testing.T) {
		runStore := newFakeStatefulRunStore()
		runID1, _ := runStore.CreateRun("proj1", nil, "", false, nil, "", nil)
		_ = runStore.FinishRun(runID1, domain.RunStatusFailed, "", "", nil)

		runID2, _ := runStore.CreateRun("proj2", nil, "", false, nil, "", nil)
		_ = runStore.FinishRun(runID2, domain.RunStatusFailed, "", "", nil)

		remoteJobs := &mocks.MockRemoteJobStore{
			ReclaimedCount:  0,
			ReclaimedFailed: 2,
			ReclaimedRunIDs: []int{runID1, runID2},
		}

		svc := NewRunService(runStore, &mocks.MockPipelineRunner{}, &mocks.MockPipelineRunner{})
		svc.SetRemoteJobStore(remoteJobs)
		reg := metrics.NewRegistry()
		svc.Metrics = reg

		hub := NewRunEventHub()
		ch1 := hub.Subscribe(runID1)
		defer hub.Unsubscribe(runID1, ch1)
		ch2 := hub.Subscribe(runID2)
		defer hub.Unsubscribe(runID2, ch2)
		svc.SetEventHub(hub)

		_, _, err := svc.ReclaimStaleRemoteJobs(time.Minute, 1, "runner went silent")
		if err != nil {
			t.Fatalf("ReclaimStaleRemoteJobs: %v", err)
		}

		snap := reg.Snapshot(0, 0, 0, "")
		if count := snap.RunTotal["failed"]["proj1"]; count != 1 {
			t.Fatalf("expected 1 failed run for proj1, got %d", count)
		}
		if count := snap.RunTotal["failed"]["proj2"]; count != 1 {
			t.Fatalf("expected 1 failed run for proj2, got %d", count)
		}
		if snap.DurationCountInf != 2 {
			t.Fatalf("expected 2 duration observations, got %d", snap.DurationCountInf)
		}

		assertBothEvents(t, ch1, runID1, domain.RunStatusFailed)
		assertBothEvents(t, ch2, runID2, domain.RunStatusFailed)
	})

	t.Run("partial store error still finalizes completed runs", func(t *testing.T) {
		runStore := newFakeStatefulRunStore()
		runID, err := runStore.CreateRun("proj", nil, "", false, nil, "", nil)
		if err != nil {
			t.Fatalf("CreateRun: %v", err)
		}
		_ = runStore.FinishRun(runID, domain.RunStatusFailed, "", "silent", nil)

		remoteJobs := &mocks.MockRemoteJobStore{
			ReclaimedCount:  1,
			ReclaimedFailed: 1,
			ReclaimedRunIDs: []int{runID},
			ReclaimErr:      errors.New("store connection lost mid-sweep"),
		}

		svc := NewRunService(runStore, &mocks.MockPipelineRunner{}, &mocks.MockPipelineRunner{})
		svc.SetRemoteJobStore(remoteJobs)
		reg := metrics.NewRegistry()
		svc.Metrics = reg

		hub := NewRunEventHub()
		ch := hub.Subscribe(runID)
		defer hub.Unsubscribe(runID, ch)
		svc.SetEventHub(hub)

		reclaimed, failed, err := svc.ReclaimStaleRemoteJobs(time.Minute, 1, "runner went silent")
		if err == nil || !strings.Contains(err.Error(), "store connection lost mid-sweep") {
			t.Fatalf("expected store error, got %v", err)
		}
		if reclaimed != 1 || failed != 1 {
			t.Fatalf("reclaimed=%d failed=%d, want 1, 1", reclaimed, failed)
		}

		snap := reg.Snapshot(0, 0, 0, "")
		if count := snap.RunTotal["failed"]["proj"]; count != 1 {
			t.Fatalf("expected 1 failed run metric recorded, got %d", count)
		}

		assertBothEvents(t, ch, runID, domain.RunStatusFailed)
	})

	t.Run("missing run during reclaim finalization is handled safely", func(t *testing.T) {
		baseStore := newFakeStatefulRunStore()
		errStore := &getRunErrorStore{
			fakeStatefulRunStore: baseStore,
			errOnGetRun:          errors.New("run not found"),
		}
		remoteJobs := &mocks.MockRemoteJobStore{
			ReclaimedRunIDs: []int{404, 500},
		}

		svc := NewRunService(errStore, &mocks.MockPipelineRunner{}, &mocks.MockPipelineRunner{})
		svc.SetRemoteJobStore(remoteJobs)
		reg := metrics.NewRegistry()
		svc.Metrics = reg

		_, _, err := svc.ReclaimStaleRemoteJobs(time.Minute, 1, "runner went silent")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		snap := reg.Snapshot(0, 0, 0, "")
		if len(snap.RunTotal) != 0 || snap.DurationCountInf != 0 {
			t.Fatalf("expected 0 metrics for missing runs, got %+v", snap)
		}
	})

	t.Run("run already terminal and canceled skips duplicate metrics and broadcasts", func(t *testing.T) {
		runStore := newFakeStatefulRunStore()
		runID, err := runStore.CreateRun("proj", nil, "", false, nil, "", nil)
		if err != nil {
			t.Fatalf("CreateRun: %v", err)
		}
		_ = runStore.FinishRun(runID, domain.RunStatusCanceled, "", "canceled by operator", nil)

		remoteJobs := &mocks.MockRemoteJobStore{
			ReclaimedRunIDs: []int{runID},
		}

		svc := NewRunService(runStore, &mocks.MockPipelineRunner{}, &mocks.MockPipelineRunner{})
		svc.SetRemoteJobStore(remoteJobs)
		reg := metrics.NewRegistry()
		svc.Metrics = reg

		hub := NewRunEventHub()
		ch := hub.Subscribe(runID)
		defer hub.Unsubscribe(runID, ch)
		svc.SetEventHub(hub)

		_, _, err = svc.ReclaimStaleRemoteJobs(time.Minute, 1, "runner went silent")
		if err != nil {
			t.Fatalf("ReclaimStaleRemoteJobs: %v", err)
		}

		snap := reg.Snapshot(0, 0, 0, "")
		if count := snap.RunTotal["canceled"]["proj"]; count != 0 {
			t.Fatalf("expected 0 canceled run metric recorded, got %d", count)
		}
		if snap.DurationCountInf != 0 {
			t.Fatalf("expected 0 duration recorded, got %d", snap.DurationCountInf)
		}

		select {
		case ev := <-ch:
			t.Fatalf("unexpected event for canceled run: %+v", ev)
		default:
		}
	})
}

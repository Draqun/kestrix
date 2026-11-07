package store

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"kestrix/internal/domain"
	"kestrix/internal/metrics"
	"kestrix/internal/model"
	"kestrix/internal/service"
)

func lintThenDeployProject() *model.Project {
	return &model.Project{
		Name: "lint-deploy-proj",
		Repos: []domain.Repo{
			{Name: "docs"},
			{Name: "app"},
		},
		Tasks: []model.Task{
			{Name: "lint", Trigger: "repo.docs.changed", Execute: domain.ExecuteSpec{Repo: "docs", Script: "echo lint"}},
			{Name: "deploy", Trigger: "repo.app.changed", DependsOn: []string{"lint"}, Execute: domain.ExecuteSpec{Repo: "app", Script: "echo deploy"}},
		},
	}
}

// 1.1(a) lint (trigger repo.docs.changed) and deploy (trigger repo.app.changed, depends on lint),
// only app changed: a runner can claim deploy.
func TestPG_RemoteRunStart_DependencyOutsideRunIgnored(t *testing.T) {
	pg := testPG(t)
	proj := lintThenDeployProject()
	mustUpsert(t, pg, proj.Name, "")
	svc := service.NewRunService(pg, nil, nil)
	svc.SetRemoteJobStore(pg)

	runID, err := svc.Start(context.Background(), proj.Name, proj, []string{"app"}, domain.RunnerOptions{
		Mode: "remote", WorkDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	runnerID := mustCreateRunner(t, pg, "runner-a")
	job, err := pg.ClaimNextJob(runnerID)
	if err != nil {
		t.Fatalf("ClaimNextJob: %v", err)
	}
	if job == nil {
		t.Fatalf("ClaimNextJob returned no job, want deploy")
	}
	if job.TaskName != "deploy" || job.RunID != runID {
		t.Fatalf("claimed job = %+v, want deploy for run %d", job, runID)
	}
}

// 1.1(b) the same pipeline with app and docs changed: lint is claimable and deploy is not until lint passes.
func TestPG_RemoteRunStart_DependencyInsideRunHolds(t *testing.T) {
	pg := testPG(t)
	proj := lintThenDeployProject()
	mustUpsert(t, pg, proj.Name, "")
	svc := service.NewRunService(pg, nil, nil)
	svc.SetRemoteJobStore(pg)

	runID, err := svc.Start(context.Background(), proj.Name, proj, []string{"app", "docs"}, domain.RunnerOptions{
		Mode: "remote", WorkDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	runnerID := mustCreateRunner(t, pg, "runner-b")

	lintJob, err := pg.ClaimNextJob(runnerID)
	if err != nil {
		t.Fatalf("ClaimNextJob: %v", err)
	}
	if lintJob == nil || lintJob.TaskName != "lint" || lintJob.RunID != runID {
		t.Fatalf("claimed job = %+v, want lint for run %d", lintJob, runID)
	}

	secondJob, err := pg.ClaimNextJob(runnerID)
	if err != nil {
		t.Fatalf("ClaimNextJob: %v", err)
	}
	if secondJob != nil {
		t.Fatalf("expected no second job before lint finishes, got %+v", secondJob)
	}

	if ok, err := svc.CompleteRemoteJob(lintJob.ID, lintJob.ClaimEpoch, "passed", "lint ok", "", &runnerID); err != nil || !ok {
		t.Fatalf("CompleteRemoteJob(lint): %v, %v", ok, err)
	}

	deployJob, err := pg.ClaimNextJob(runnerID)
	if err != nil {
		t.Fatalf("ClaimNextJob: %v", err)
	}
	if deployJob == nil || deployJob.TaskName != "deploy" || deployJob.RunID != runID {
		t.Fatalf("claimed job = %+v, want deploy for run %d", deployJob, runID)
	}
}

// 1.1(c) no task triggered: the run is passed and finished, with no result and no job.
func TestPG_RemoteRunStart_NothingTriggeredFinishesPassed(t *testing.T) {
	pg := testPG(t)
	proj := lintThenDeployProject()
	mustUpsert(t, pg, proj.Name, "")
	svc := service.NewRunService(pg, nil, nil)
	svc.SetRemoteJobStore(pg)

	runID, err := svc.Start(context.Background(), proj.Name, proj, []string{"other"}, domain.RunnerOptions{
		Mode: "remote", WorkDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	run, err := pg.GetRun(runID)
	if err != nil || run == nil {
		t.Fatalf("GetRun: %+v, %v", run, err)
	}
	if run.Status != "passed" {
		t.Fatalf("run status = %q, want passed", run.Status)
	}
	if run.FinishedAt == nil {
		t.Fatal("expected finished_at to be set")
	}
	if len(run.TaskResults) != 2 {
		t.Fatalf("expected 2 task results (trigger_unsatisfied), got %d", len(run.TaskResults))
	}
	for _, tr := range run.TaskResults {
		if tr.Status != "skipped" || tr.SkipReason == nil || tr.SkipReason.Category != domain.SkipCategoryTriggerUnsatisfied {
			t.Errorf("task result = %+v, want skipped trigger_unsatisfied", tr)
		}
	}
	runnerID := mustCreateRunner(t, pg, "runner-c")
	job, err := pg.ClaimNextJob(runnerID)
	if err != nil {
		t.Fatalf("ClaimNextJob: %v", err)
	}
	if job != nil {
		t.Fatalf("expected no job, got %+v", job)
	}
}

// 1.1(d) a working directory that does not exist: Start returns the error,
// the run is failed with an error naming the cause, and it has no job.
func TestPG_RemoteRunStart_NonExistentWorkDirFailsRun(t *testing.T) {
	pg := testPG(t)
	proj := lintThenDeployProject()
	mustUpsert(t, pg, proj.Name, "")
	svc := service.NewRunService(pg, nil, nil)
	svc.SetRemoteJobStore(pg)

	badDir := filepath.Join(t.TempDir(), "does-not-exist")
	runID, err := svc.Start(context.Background(), proj.Name, proj, []string{"app"}, domain.RunnerOptions{
		Mode: "remote", WorkDir: badDir,
	})
	if err == nil {
		t.Fatal("expected Start error for non-existent workdir")
	}
	run, getErr := pg.GetRun(runID)
	if getErr != nil || run == nil {
		t.Fatalf("GetRun: %+v, %v", run, getErr)
	}
	if run.Status != "failed" {
		t.Fatalf("run status = %q, want failed", run.Status)
	}
	if run.FinishedAt == nil {
		t.Fatal("expected finished_at to be set")
	}
	if !strings.Contains(run.Error, "archive") && !strings.Contains(run.Error, "no such file or directory") {
		t.Fatalf("run error %q does not name cause", run.Error)
	}
	runnerID := mustCreateRunner(t, pg, "runner-d")
	job, err := pg.ClaimNextJob(runnerID)
	if err != nil {
		t.Fatalf("ClaimNextJob: %v", err)
	}
	if job != nil {
		t.Fatalf("expected no job, got %+v", job)
	}
}

func TestPG_ReclaimStaleRemoteJobs_TelemetryAndEvents(t *testing.T) {
	pg := testPG(t)
	proj := &model.Project{
		Name:  "reclaim-telemetry-proj",
		Repos: []domain.Repo{{Name: "app"}},
		Tasks: []model.Task{
			{Name: "build", Trigger: "repo.app.changed", Execute: domain.ExecuteSpec{Repo: "app", Script: "echo ok"}},
		},
	}
	mustUpsert(t, pg, proj.Name, "")

	svc := service.NewRunService(pg, nil, nil)
	svc.SetRemoteJobStore(pg)
	reg := metrics.NewRegistry()
	svc.Metrics = reg
	hub := service.NewRunEventHub()
	svc.SetEventHub(hub)

	runID, err := svc.Start(context.Background(), proj.Name, proj, []string{"app"}, domain.RunnerOptions{
		Mode: "remote", WorkDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	ch := hub.Subscribe(runID)
	defer hub.Unsubscribe(runID, ch)

	runnerID := mustCreateRunner(t, pg, "runner-stale-tel")
	job, err := pg.ClaimNextJob(runnerID)
	if err != nil || job == nil {
		t.Fatalf("ClaimNextJob: %+v, %v", job, err)
	}

	// 1. Silent runner recovery returns to pending when reclaim_count < maxReclaims
	reclaimed, failed, err := svc.ReclaimStaleRemoteJobs(-time.Hour, 2, "runner went silent")
	if err != nil || reclaimed != 1 || failed != 0 {
		t.Fatalf("first reclaim: reclaimed=%d, failed=%d, err=%v, want 1, 0, nil", reclaimed, failed, err)
	}

	snap := reg.Snapshot(0, 0, 0, "")
	if count := snap.RunTotal["failed"][proj.Name]; count != 0 {
		t.Fatalf("expected 0 failed runs in telemetry while in flight, got %d", count)
	}
	select {
	case ev := <-ch:
		t.Fatalf("expected no EventHub events while in flight, got %+v", ev)
	default:
	}

	// Claim again
	job2, err := pg.ClaimNextJob(mustCreateRunner(t, pg, "runner-stale-tel-2"))
	if err != nil || job2 == nil {
		t.Fatalf("ClaimNextJob 2: %+v, %v", job2, err)
	}

	// 2. Silent runner recovery reaches maxReclaims -> fails job, ends run in PG, records telemetry and broadcasts events
	reclaimed, failed, err = svc.ReclaimStaleRemoteJobs(-time.Hour, 1, "runner went silent")
	if err != nil || reclaimed != 0 || failed != 1 {
		t.Fatalf("second reclaim: reclaimed=%d, failed=%d, err=%v, want 0, 1, nil", reclaimed, failed, err)
	}

	// Run in PG is failed
	run, err := pg.GetRun(runID)
	if err != nil || run == nil {
		t.Fatalf("GetRun: %+v, %v", run, err)
	}
	if run.Status != domain.RunStatusFailed {
		t.Fatalf("run status in PG = %q, want failed", run.Status)
	}
	if run.FinishedAt == nil {
		t.Fatal("expected finished_at to be set on run")
	}

	// Telemetry metrics recorded
	snap = reg.Snapshot(0, 0, 0, "")
	if count := snap.RunTotal["failed"][proj.Name]; count != 1 {
		t.Fatalf("expected 1 failed run in telemetry, got %d", count)
	}
	if snap.DurationCountInf != 1 {
		t.Fatalf("expected duration count 1 in telemetry, got %d", snap.DurationCountInf)
	}

	// EventHub broadcasted status and complete events
	receivedStatus, receivedComplete := false, false
	for i := 0; i < 2; i++ {
		select {
		case ev := <-ch:
			if ev.Type == service.EventTypeStatus {
				if payload, ok := ev.Data.(service.StatusPayload); ok && payload.RunID == runID && payload.Status == domain.RunStatusFailed {
					receivedStatus = true
				}
			} else if ev.Type == service.EventTypeComplete {
				if payload, ok := ev.Data.(service.CompletePayload); ok && payload.RunID == runID && payload.Status == domain.RunStatusFailed {
					receivedComplete = true
				}
			}
		default:
		}
	}
	if !receivedStatus || !receivedComplete {
		t.Fatalf("expected status and complete events on EventHub, got status=%v, complete=%v", receivedStatus, receivedComplete)
	}
}

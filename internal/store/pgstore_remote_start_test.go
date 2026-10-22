package store

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"kestrix/internal/domain"
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
	if len(run.TaskResults) != 0 {
		t.Fatalf("expected 0 task results, got %d", len(run.TaskResults))
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

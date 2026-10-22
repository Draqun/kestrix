package store

import (
	"context"
	"testing"

	"kestrix/internal/domain"
	"kestrix/internal/model"
	"kestrix/internal/service"
)

// claimNextJob claims the next job for runnerID and fails the test when it
// is not the expected task - "no job" is the failure this file exists to
// catch, so it must never be read as an empty success.
func claimNextJob(t *testing.T, pg *PGStore, runnerID int, taskName string) *domain.RemoteJob {
	t.Helper()
	job, err := pg.ClaimNextJob(runnerID)
	if err != nil {
		t.Fatalf("ClaimNextJob: %v", err)
	}
	if job == nil {
		t.Fatalf("ClaimNextJob returned no job, want the %q job", taskName)
	}
	if job.TaskName != taskName {
		t.Fatalf("claimed job = %q, want %q", job.TaskName, taskName)
	}
	return job
}

// remote-run-waits-for-dependents: a whole remote run's dependent runs after
// the task that unblocks it, and the run does not read "passed" before that
// dependent has run. Before the change, build's completion finished the run
// on the spot - deploy had no result yet, so nothing was pending or running
// and the run was "passed" - and deploy's job was never claimed, because
// ClaimNextJob skips the jobs of a finished run.
func TestPG_RemoteRunWaitsForDependents(t *testing.T) {
	pg := testPG(t)
	spec := domain.ExecuteSpec{Repo: "app", Script: "echo hi"}
	proj := &model.Project{Name: "deps-proj", Repos: []domain.Repo{{Name: "app"}}, Tasks: []model.Task{
		{Name: "build", Trigger: "repo.app.changed", Execute: spec},
		{Name: "deploy", Trigger: "repo.app.changed", DependsOn: []string{"build"}, Execute: spec},
	}}
	mustUpsert(t, pg, "deps-proj", "")
	svc := service.NewRunService(pg, nil, nil)
	svc.SetRemoteJobStore(pg)

	runID, err := svc.Start(context.Background(), "deps-proj", proj, []string{"app"}, domain.RunnerOptions{
		Mode: "remote", WorkDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	runnerID := mustCreateRunner(t, pg, "deps-runner")

	build := claimNextJob(t, pg, runnerID, "build")
	if ok, err := svc.CompleteRemoteJob(build.ID, build.ClaimEpoch, "passed", "built", "", &runnerID); err != nil || !ok {
		t.Fatalf("CompleteRemoteJob(build) = %v, %v", ok, err)
	}

	// The run is not finished: deploy has not run yet, so the run is still
	// running and its dependent is claimable.
	run, err := pg.GetRun(runID)
	if err != nil || run == nil {
		t.Fatalf("GetRun = %+v, %v", run, err)
	}
	if run.Status != "running" {
		t.Fatalf("run status after build passed = %q, want running", run.Status)
	}
	deploy := claimNextJob(t, pg, runnerID, "deploy")

	if ok, err := svc.CompleteRemoteJob(deploy.ID, deploy.ClaimEpoch, "passed", "deployed", "", &runnerID); err != nil || !ok {
		t.Fatalf("CompleteRemoteJob(deploy) = %v, %v", ok, err)
	}
	run, err = pg.GetRun(runID)
	if err != nil || run == nil {
		t.Fatalf("GetRun = %+v, %v", run, err)
	}
	if run.Status != "passed" {
		t.Fatalf("run status after deploy passed = %q, want passed", run.Status)
	}
}

// The other half of the same fix: a failed task skips its dependents and
// ends the run - the run is not left running with a task that can never
// run.
func TestPG_RemoteRunFailureSkipsDependents(t *testing.T) {
	pg := testPG(t)
	spec := domain.ExecuteSpec{Repo: "app", Script: "echo hi"}
	proj := &model.Project{Name: "deps-fail-proj", Repos: []domain.Repo{{Name: "app"}}, Tasks: []model.Task{
		{Name: "build", Trigger: "repo.app.changed", Execute: spec},
		{Name: "deploy", Trigger: "repo.app.changed", DependsOn: []string{"build"}, Execute: spec},
	}}
	mustUpsert(t, pg, "deps-fail-proj", "")
	svc := service.NewRunService(pg, nil, nil)
	svc.SetRemoteJobStore(pg)

	runID, err := svc.Start(context.Background(), "deps-fail-proj", proj, []string{"app"}, domain.RunnerOptions{
		Mode: "remote", WorkDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	runnerID := mustCreateRunner(t, pg, "deps-fail-runner")

	build := claimNextJob(t, pg, runnerID, "build")
	if ok, err := svc.CompleteRemoteJob(build.ID, build.ClaimEpoch, "failed", "broke", "exit 1", &runnerID); err != nil || !ok {
		t.Fatalf("CompleteRemoteJob(build) = %v, %v", ok, err)
	}

	run, err := pg.GetRun(runID)
	if err != nil || run == nil {
		t.Fatalf("GetRun = %+v, %v", run, err)
	}
	if run.Status != "failed" {
		t.Fatalf("run status after build failed = %q, want failed", run.Status)
	}
	skipped := false
	for _, tr := range run.TaskResults {
		if tr.Name == "deploy" && tr.Status == "skipped" {
			skipped = true
		}
	}
	if !skipped {
		t.Fatalf("deploy was not recorded skipped: %+v", run.TaskResults)
	}
}

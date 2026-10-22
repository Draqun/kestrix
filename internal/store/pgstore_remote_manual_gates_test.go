package store

import (
	"context"
	"testing"

	"kestrix/internal/domain"
	"kestrix/internal/model"
	"kestrix/internal/service"
)

// 1.1(a) build -> deploy (manual): after build passes the run is running, deploy is manual and no job exists for it.
func TestPG_RemoteManualGate_BuildPassesLeavesDeployManualAndRunRunning(t *testing.T) {
	pg := testPG(t)
	spec := domain.ExecuteSpec{Repo: "app", Script: "echo hi"}
	proj := &model.Project{
		Name:  "gate-a-proj",
		Repos: []domain.Repo{{Name: "app"}},
		Tasks: []model.Task{
			{Name: "build", Trigger: "repo.app.changed", Execute: spec},
			{Name: "deploy", Trigger: "repo.app.changed", DependsOn: []string{"build"}, Manual: true, Execute: spec},
		},
	}
	mustUpsert(t, pg, proj.Name, "")
	svc := service.NewRunService(pg, nil, nil)
	svc.SetRemoteJobStore(pg)

	runID, err := svc.Start(context.Background(), proj.Name, proj, []string{"app"}, domain.RunnerOptions{
		Mode: "remote", WorkDir: t.TempDir(), IncludeManual: false,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	runnerID := mustCreateRunner(t, pg, "runner-a")

	buildJob := claimNextJob(t, pg, runnerID, "build")
	if ok, err := svc.CompleteRemoteJob(buildJob.ID, buildJob.ClaimEpoch, "passed", "built", "", &runnerID); err != nil || !ok {
		t.Fatalf("CompleteRemoteJob(build) = %v, %v", ok, err)
	}

	run, err := pg.GetRun(runID)
	if err != nil || run == nil {
		t.Fatalf("GetRun = %+v, %v", run, err)
	}
	if run.Status != "running" {
		t.Fatalf("run status after build passed = %q, want running", run.Status)
	}

	var deployResult *domain.TaskResult
	for _, tr := range run.TaskResults {
		if tr.Name == "deploy" {
			r := tr
			deployResult = &r
		}
	}
	if deployResult == nil {
		t.Fatalf("expected deploy task result, got none in %+v", run.TaskResults)
	}
	if deployResult.Status != "manual" {
		t.Fatalf("deploy task status = %q, want manual", deployResult.Status)
	}

	job, err := pg.ClaimNextJob(runnerID)
	if err != nil {
		t.Fatalf("ClaimNextJob: %v", err)
	}
	if job != nil {
		t.Fatalf("expected no job for manual deploy, got %+v", job)
	}
}

// 1.1(b) the same with include_manual: a job for deploy is claimable after build.
func TestPG_RemoteManualGate_IncludeManualDispatchesDeployAfterBuild(t *testing.T) {
	pg := testPG(t)
	spec := domain.ExecuteSpec{Repo: "app", Script: "echo hi"}
	proj := &model.Project{
		Name:  "gate-b-proj",
		Repos: []domain.Repo{{Name: "app"}},
		Tasks: []model.Task{
			{Name: "build", Trigger: "repo.app.changed", Execute: spec},
			{Name: "deploy", Trigger: "repo.app.changed", DependsOn: []string{"build"}, Manual: true, Execute: spec},
		},
	}
	mustUpsert(t, pg, proj.Name, "")
	svc := service.NewRunService(pg, nil, nil)
	svc.SetRemoteJobStore(pg)

	runID, err := svc.Start(context.Background(), proj.Name, proj, []string{"app"}, domain.RunnerOptions{
		Mode: "remote", WorkDir: t.TempDir(), IncludeManual: true,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	runnerID := mustCreateRunner(t, pg, "runner-b")

	buildJob := claimNextJob(t, pg, runnerID, "build")
	if ok, err := svc.CompleteRemoteJob(buildJob.ID, buildJob.ClaimEpoch, "passed", "built", "", &runnerID); err != nil || !ok {
		t.Fatalf("CompleteRemoteJob(build) = %v, %v", ok, err)
	}

	deployJob := claimNextJob(t, pg, runnerID, "deploy")
	if deployJob.RunID != runID {
		t.Fatalf("claimed deploy job runID = %d, want %d", deployJob.RunID, runID)
	}
}

// 1.1(c) build fails: deploy is skipped with dependency failed and the run is failed.
func TestPG_RemoteManualGate_BuildFailsSkipsDeployAndFailsRun(t *testing.T) {
	pg := testPG(t)
	spec := domain.ExecuteSpec{Repo: "app", Script: "echo hi"}
	proj := &model.Project{
		Name:  "gate-c-proj",
		Repos: []domain.Repo{{Name: "app"}},
		Tasks: []model.Task{
			{Name: "build", Trigger: "repo.app.changed", Execute: spec},
			{Name: "deploy", Trigger: "repo.app.changed", DependsOn: []string{"build"}, Manual: true, Execute: spec},
		},
	}
	mustUpsert(t, pg, proj.Name, "")
	svc := service.NewRunService(pg, nil, nil)
	svc.SetRemoteJobStore(pg)

	runID, err := svc.Start(context.Background(), proj.Name, proj, []string{"app"}, domain.RunnerOptions{
		Mode: "remote", WorkDir: t.TempDir(), IncludeManual: false,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	runnerID := mustCreateRunner(t, pg, "runner-c")

	buildJob := claimNextJob(t, pg, runnerID, "build")
	if ok, err := svc.CompleteRemoteJob(buildJob.ID, buildJob.ClaimEpoch, "failed", "build failed", "exit 1", &runnerID); err != nil || !ok {
		t.Fatalf("CompleteRemoteJob(build) = %v, %v", ok, err)
	}

	run, err := pg.GetRun(runID)
	if err != nil || run == nil {
		t.Fatalf("GetRun = %+v, %v", run, err)
	}
	if run.Status != "failed" {
		t.Fatalf("run status after build failed = %q, want failed", run.Status)
	}

	var deployResult *domain.TaskResult
	for _, tr := range run.TaskResults {
		if tr.Name == "deploy" {
			r := tr
			deployResult = &r
		}
	}
	if deployResult == nil {
		t.Fatalf("expected deploy task result, got none in %+v", run.TaskResults)
	}
	if deployResult.Status != "skipped" || deployResult.Error != "dependency failed" {
		t.Fatalf("deploy task result = %+v, want skipped with 'dependency failed'", deployResult)
	}
}

// 1.1(d) every triggered task manual: each is manual and the run is running.
func TestPG_RemoteManualGate_AllTasksManualRunIsRunningAndTasksManual(t *testing.T) {
	pg := testPG(t)
	spec := domain.ExecuteSpec{Repo: "app", Script: "echo hi"}
	proj := &model.Project{
		Name:  "gate-d-proj",
		Repos: []domain.Repo{{Name: "app"}},
		Tasks: []model.Task{
			{Name: "deploy-a", Trigger: "repo.app.changed", Manual: true, Execute: spec},
			{Name: "deploy-b", Trigger: "repo.app.changed", Manual: true, Execute: spec},
		},
	}
	mustUpsert(t, pg, proj.Name, "")
	svc := service.NewRunService(pg, nil, nil)
	svc.SetRemoteJobStore(pg)

	runID, err := svc.Start(context.Background(), proj.Name, proj, []string{"app"}, domain.RunnerOptions{
		Mode: "remote", WorkDir: t.TempDir(), IncludeManual: false,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	run, err := pg.GetRun(runID)
	if err != nil || run == nil {
		t.Fatalf("GetRun = %+v, %v", run, err)
	}
	if run.Status != "running" {
		t.Fatalf("run status with only manual tasks = %q, want running", run.Status)
	}

	resultsByName := make(map[string]domain.TaskResult)
	for _, tr := range run.TaskResults {
		resultsByName[tr.Name] = tr
	}
	for _, taskName := range []string{"deploy-a", "deploy-b"} {
		res, ok := resultsByName[taskName]
		if !ok {
			t.Fatalf("missing task result for %q in %+v", taskName, run.TaskResults)
		}
		if res.Status != "manual" {
			t.Fatalf("task %q status = %q, want manual", taskName, res.Status)
		}
	}

	runnerID := mustCreateRunner(t, pg, "runner-d")
	job, err := pg.ClaimNextJob(runnerID)
	if err != nil {
		t.Fatalf("ClaimNextJob: %v", err)
	}
	if job != nil {
		t.Fatalf("expected no jobs claimed for manual tasks, got %+v", job)
	}
}

// 1.1(e) Start given InitialSources: the run records them.
func TestPG_RemoteManualGate_StartGivenInitialSourcesRecordsThem(t *testing.T) {
	pg := testPG(t)
	spec := domain.ExecuteSpec{Repo: "app", Script: "echo hi"}
	proj := &model.Project{
		Name:  "gate-e-proj",
		Repos: []domain.Repo{{Name: "app"}},
		Tasks: []model.Task{
			{Name: "build", Trigger: "repo.app.changed", Execute: spec},
		},
	}
	mustUpsert(t, pg, proj.Name, "")
	svc := service.NewRunService(pg, nil, nil)
	svc.SetRemoteJobStore(pg)

	initialSources := []domain.AcquiredSource{
		{RepoName: "app", URL: "https://example.com/repo.git", RequestedRef: "main", ResolvedRevision: "abcdef123456"},
	}

	runID, err := svc.Start(context.Background(), proj.Name, proj, []string{"app"}, domain.RunnerOptions{
		Mode: "remote", WorkDir: t.TempDir(), InitialSources: initialSources,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	run, err := pg.GetRun(runID)
	if err != nil || run == nil {
		t.Fatalf("GetRun = %+v, %v", run, err)
	}
	if len(run.Sources) != 1 {
		t.Fatalf("run.Sources len = %d, want 1; got %+v", len(run.Sources), run.Sources)
	}
	if run.Sources[0].RepoName != "app" || run.Sources[0].ResolvedRevision != "abcdef123456" {
		t.Fatalf("run.Sources[0] = %+v, want RepoName app, ResolvedRevision abcdef123456", run.Sources[0])
	}
}

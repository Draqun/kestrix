package store

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"kestrix/internal/domain"
	"kestrix/internal/model"
	"kestrix/internal/service"
)

// remote-run-ux D2: completing a single-task re-run's job must not resume the
// run. lint's negated trigger holds for any changed set, so resuming a run
// that has no snapshot dispatched lint - and deploy, once build passed - too,
// with no source archive and no variables.
func TestPG_SingleTaskRerun_CompletionDispatchesNothingElse(t *testing.T) {
	pg := testPG(t)
	spec := domain.ExecuteSpec{Repo: "app", Script: "echo hi"}
	proj := &model.Project{Name: "rerun-proj", Repos: []domain.Repo{{Name: "app"}, {Name: "docs"}}, Tasks: []model.Task{
		{Name: "lint", Trigger: "!repo.docs.changed", Execute: spec},
		{Name: "build", Trigger: "!repo.docs.changed", Execute: spec},
		{Name: "deploy", Trigger: "!repo.docs.changed", DependsOn: []string{"build"}, Execute: spec},
	}}
	mustUpsert(t, pg, "rerun-proj", "")
	runID := mustCreateRun(t, pg, "rerun-proj", proj)
	svc := service.NewRunService(pg, nil, nil)
	svc.SetRemoteJobStore(pg)

	opts := domain.RunnerOptions{Mode: "remote", WorkDir: t.TempDir()}
	if _, err := svc.StartTask(context.Background(), runID, "rerun-proj", proj, "build", opts); err != nil {
		t.Fatalf("StartTask: %v", err)
	}
	runnerID := mustCreateRunner(t, pg, "rerun-runner")
	job, err := pg.ClaimNextJob(runnerID)
	if err != nil || job == nil || job.TaskName != "build" {
		t.Fatalf("ClaimNextJob = %+v, %v; want the build job", job, err)
	}
	if ok, err := svc.CompleteRemoteJob(job.ID, job.ClaimEpoch, "passed", "", "", &runnerID); err != nil || !ok {
		t.Fatalf("CompleteRemoteJob = %v, %v", ok, err)
	}

	rows, err := pg.db.Query(`SELECT task_name FROM remote_jobs WHERE run_id = $1 ORDER BY id`, runID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var jobs []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		jobs = append(jobs, name)
	}
	if want := []string{"build"}; !reflect.DeepEqual(jobs, want) {
		t.Fatalf("remote_jobs task names = %v, want %v", jobs, want)
	}
	run, err := pg.GetRun(runID)
	if err != nil || run == nil {
		t.Fatalf("GetRun = %+v, %v", run, err)
	}
	for _, tr := range run.TaskResults {
		if tr.Name != "build" {
			t.Fatalf("task results name %q, want only build: %+v", tr.Name, run.TaskResults)
		}
	}
	if run.Status != "passed" {
		t.Fatalf("run status = %q, want passed", run.Status)
	}
}

// remote-run-ux D3: a single-task re-run records its task in rerun_task and
// no changed repositories; every query that returns a run carries the field.
func TestPG_CreateRun_RerunTaskRoundTrips(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "rerun-label", "")
	id, err := pg.CreateRun("rerun-label", nil, "build", false, nil, "user:alice")
	if err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	got, err := pg.GetRun(id)
	if err != nil || got == nil {
		t.Fatalf("GetRun = %+v, %v", got, err)
	}
	if got.RerunTask != "build" || len(got.Changed) != 0 {
		t.Fatalf("GetRun rerun_task = %q, changed = %v; want build, none", got.RerunTask, got.Changed)
	}
	b, _ := json.Marshal(got)
	for _, want := range []string{`"rerun_task":"build"`, `"changed":[]`} {
		if !strings.Contains(string(b), want) {
			t.Fatalf("run JSON %s lacks %s", b, want)
		}
	}
	listed, err := pg.ListRuns("rerun-label", 10, 0)
	if err != nil || len(listed) != 1 || listed[0].RerunTask != "build" {
		t.Fatalf("ListRuns = %+v, %v; want one run with rerun_task build", listed, err)
	}
	all, err := pg.ListAllRuns(50)
	if err != nil {
		t.Fatalf("ListAllRuns: %v", err)
	}
	found := false
	for _, r := range all {
		if r.ID == id {
			found = r.RerunTask == "build"
		}
	}
	if !found {
		t.Fatalf("ListAllRuns did not return run %d with rerun_task build: %+v", id, all)
	}
}

// A run stored before rerun_task existed (its INSERT names no such column)
// reads back with an empty rerun_task and keeps its changed repositories.
func TestPG_RunWithoutRerunTaskReadsEmpty(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "rerun-old", "")
	var id int
	if err := pg.db.QueryRow(
		`INSERT INTO runs (project_name, changed, include_manual, started_by) VALUES ($1, '["app"]', false, '') RETURNING id`,
		"rerun-old",
	).Scan(&id); err != nil {
		t.Fatal(err)
	}
	got, err := pg.GetRun(id)
	if err != nil || got == nil {
		t.Fatalf("GetRun = %+v, %v", got, err)
	}
	if got.RerunTask != "" || !reflect.DeepEqual(got.Changed, []string{"app"}) {
		t.Fatalf("old run rerun_task = %q, changed = %v; want empty, [app]", got.RerunTask, got.Changed)
	}
}

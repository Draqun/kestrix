package store

import (
	"strings"
	"testing"
	"time"

	"kestrix/internal/domain"
)

// mustRunWithResults creates a run whose task_results hold the given
// results - the state a remote run's results are in once its jobs reported.
func mustRunWithResults(t *testing.T, pg *PGStore, projectName string, results ...domain.TaskResult) int {
	t.Helper()
	runID := mustCreateRun(t, pg, projectName, nil)
	for _, tr := range results {
		if err := pg.AppendTaskResult(runID, tr); err != nil {
			t.Fatalf("AppendTaskResult(%q): %v", tr.Name, err)
		}
	}
	return runID
}

// ── CompleteJob: one protocol on the port, one in the store ─────────

// The port's completion records the result, the output and the job, and
// leaves the run's status, error and finished_at alone: the service finishes
// the run itself, once it has decided what the completion unblocked.
func TestPG_CompleteJob_LeavesTheRunAlone(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "proj-no-finish", "")
	runID := mustRunWithResults(t, pg, "proj-no-finish", domain.TaskResult{Name: "build", Status: "pending"})
	jobID := mustCreateRemoteJob(t, pg, runID, "build")
	job, err := pg.ClaimNextJob(mustCreateRunner(t, pg, "r-no-finish"))
	if err != nil || job == nil || job.ID != jobID {
		t.Fatalf("ClaimNextJob = %+v, %v", job, err)
	}

	ok, err := pg.CompleteJob(jobID, job.ClaimEpoch, domain.RemoteJobStatusPassed, "build output", "", nil, 0)
	if err != nil || !ok {
		t.Fatalf("CompleteJob = (%v, %v)", ok, err)
	}

	run, err := pg.GetRun(runID)
	if err != nil || run == nil {
		t.Fatalf("GetRun = %+v, %v", run, err)
	}
	if run.Status != "running" {
		t.Fatalf("run status = %q, want running (the completion does not finish the run)", run.Status)
	}
	if run.FinishedAt != nil {
		t.Fatalf("run finished_at = %v, want nil", run.FinishedAt)
	}
	if run.Error != "" {
		t.Fatalf("run error = %q, want empty", run.Error)
	}
	if !strings.Contains(run.Output, "build output") {
		t.Fatalf("run output = %q, want it to hold the completion's output", run.Output)
	}
	passed := false
	for _, tr := range run.TaskResults {
		if tr.Name == "build" && tr.Status == "passed" {
			passed = true
		}
	}
	if !passed {
		t.Fatalf("task results = %+v, want a passed result for build", run.TaskResults)
	}
	after, err := pg.GetRemoteJob(jobID)
	if err != nil || after == nil {
		t.Fatalf("GetRemoteJob = %+v, %v", after, err)
	}
	if after.Status != domain.RemoteJobStatusPassed {
		t.Fatalf("job status = %q, want passed", after.Status)
	}
}

// The store's own second protocol - the one ReclaimStaleJobs uses - derives
// the run's status from the recorded results and finishes the run when that
// status is terminal: a stale job ends the pipeline's useful work.
func TestPG_completeJob_FinishesTheRun(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "proj-finish", "")
	runID := mustRunWithResults(t, pg, "proj-finish", domain.TaskResult{Name: "build", Status: "pending"})
	jobID := mustCreateRemoteJob(t, pg, runID, "build")
	job, err := pg.ClaimNextJob(mustCreateRunner(t, pg, "r-finish"))
	if err != nil || job == nil || job.ID != jobID {
		t.Fatalf("ClaimNextJob = %+v, %v", job, err)
	}

	ok, err := pg.completeJob(jobID, job.ClaimEpoch, domain.RemoteJobStatusPassed, "build output", "", nil, 0, true)
	if err != nil || !ok {
		t.Fatalf("CompleteJob = (%v, %v)", ok, err)
	}

	run, err := pg.GetRun(runID)
	if err != nil || run == nil {
		t.Fatalf("GetRun = %+v, %v", run, err)
	}
	if run.Status != "passed" {
		t.Fatalf("run status = %q, want passed", run.Status)
	}
	if run.FinishedAt == nil {
		t.Fatal("run finished_at is nil, want it set")
	}
}

// A fenced completion - one whose run went terminal for another reason - is
// rejected the same way in both protocols: no result is recorded and the run
// keeps its terminal status.
func TestPG_CompleteJob_FencedCompletionIsUnchanged(t *testing.T) {
	for _, finishRun := range []bool{false, true} {
		t.Run(map[bool]string{false: "no-finish", true: "finish"}[finishRun], func(t *testing.T) {
			pg := testPG(t)
			mustUpsert(t, pg, "proj-fenced", "")
			runID := mustRunWithResults(t, pg, "proj-fenced", domain.TaskResult{Name: "build", Status: "pending"})
			jobID := mustCreateRemoteJob(t, pg, runID, "build")
			job, err := pg.ClaimNextJob(mustCreateRunner(t, pg, "r-fenced"))
			if err != nil || job == nil || job.ID != jobID {
				t.Fatalf("ClaimNextJob = %+v, %v", job, err)
			}
			if _, err := pg.db.Exec(`UPDATE runs SET status = 'failed' WHERE id = $1`, runID); err != nil {
				t.Fatal(err)
			}

			ok, err := pg.completeJob(jobID, job.ClaimEpoch, domain.RemoteJobStatusPassed, "late output", "", nil, 0, finishRun)
			if err != nil {
				t.Fatalf("CompleteJob: %v", err)
			}
			if ok {
				t.Fatal("expected the completion to be rejected: the run is already terminal")
			}
			run, err := pg.GetRun(runID)
			if err != nil || run == nil {
				t.Fatalf("GetRun = %+v, %v", run, err)
			}
			if run.Status != "failed" {
				t.Fatalf("run status = %q, want failed", run.Status)
			}
			for _, tr := range run.TaskResults {
				if tr.Name == "build" && tr.Status == "passed" {
					t.Fatalf("a fenced completion must record no result: %+v", run.TaskResults)
				}
			}
		})
	}
}

// ── FinishRunIfDone (remote-run-waits-for-dependents design.md D2) ──

// The run takes the status its results imply - passed, failed or canceled -
// and only then.
func TestPG_FinishRunIfDone_TakesTheStatusTheResultsImply(t *testing.T) {
	cases := []struct {
		name    string
		results []domain.TaskResult
		want    string
		wantErr string
	}{
		{"passed", []domain.TaskResult{{Name: "build", Status: "passed"}}, "passed", ""},
		{"failed", []domain.TaskResult{{Name: "build", Status: "failed", Error: "exit 1"}}, "failed", "exit 1"},
		{"canceled", []domain.TaskResult{{Name: "build", Status: "canceled"}}, "canceled", "canceled"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pg := testPG(t)
			mustUpsert(t, pg, "proj-done", "")
			runID := mustRunWithResults(t, pg, "proj-done", tc.results...)

			if err := pg.FinishRunIfDone(runID); err != nil {
				t.Fatalf("FinishRunIfDone: %v", err)
			}
			run, err := pg.GetRun(runID)
			if err != nil || run == nil {
				t.Fatalf("GetRun = %+v, %v", run, err)
			}
			if run.Status != tc.want {
				t.Fatalf("run status = %q, want %q", run.Status, tc.want)
			}
			if run.Error != tc.wantErr {
				t.Fatalf("run error = %q, want %q", run.Error, tc.wantErr)
			}
			if run.FinishedAt == nil {
				t.Fatal("run finished_at is nil, want it set")
			}
		})
	}
}

// A run with a task still pending or running is not done: it keeps running.
func TestPG_FinishRunIfDone_KeepsARunWithWorkInFlightRunning(t *testing.T) {
	for _, status := range []string{"pending", "running"} {
		t.Run(status, func(t *testing.T) {
			pg := testPG(t)
			mustUpsert(t, pg, "proj-inflight", "")
			runID := mustRunWithResults(t, pg, "proj-inflight",
				domain.TaskResult{Name: "build", Status: "passed"},
				domain.TaskResult{Name: "deploy", Status: status},
			)

			if err := pg.FinishRunIfDone(runID); err != nil {
				t.Fatalf("FinishRunIfDone: %v", err)
			}
			run, err := pg.GetRun(runID)
			if err != nil || run == nil {
				t.Fatalf("GetRun = %+v, %v", run, err)
			}
			if run.Status != "running" {
				t.Fatalf("run status = %q, want running while deploy is %s", run.Status, status)
			}
			if run.FinishedAt != nil {
				t.Fatalf("run finished_at = %v, want nil", run.FinishedAt)
			}
		})
	}
}

// Results that cannot be read decide nothing: the run is left as it is
// instead of being finished on a status nobody could derive.
func TestPG_FinishRunIfDone_LeavesTheRunAloneWhenTheResultsCannotBeRead(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "proj-unreadable", "")
	runID := mustRunWithResults(t, pg, "proj-unreadable", domain.TaskResult{Name: "build", Status: "passed"})
	if _, err := pg.db.Exec(`UPDATE runs SET task_results = '{"build":"passed"}'::jsonb WHERE id = $1`, runID); err != nil {
		t.Fatalf("write unreadable task_results: %v", err)
	}

	if err := pg.FinishRunIfDone(runID); err == nil {
		t.Fatal("FinishRunIfDone: no error, want one for results it cannot read")
	}
	run, err := pg.GetRun(runID)
	if err != nil || run == nil {
		t.Fatalf("GetRun = %+v, %v", run, err)
	}
	if run.Status != "running" {
		t.Fatalf("run status = %q, want running - the run is not finished on a decision nobody could read", run.Status)
	}
	if run.FinishedAt != nil {
		t.Fatalf("run finished_at = %v, want nil", run.FinishedAt)
	}
}

// A stale job that exhausts its reclaims fails through the store's own
// second protocol. It ends the run only when nothing is left in flight: a
// sibling task that is still pending keeps the run running, the same rule
// FinishRunIfDone follows.
func TestPG_ReclaimFailures_KeepTheRunRunningWhileASiblingIsInFlight(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "proj-reclaim-sibling", "")
	runID := mustRunWithResults(t, pg, "proj-reclaim-sibling",
		domain.TaskResult{Name: "build", Status: "pending"},
		domain.TaskResult{Name: "deploy", Status: "pending"},
	)
	jobID := mustCreateRemoteJob(t, pg, runID, "build")
	mustCreateRemoteJob(t, pg, runID, "deploy")
	pg.ClaimNextJob(mustCreateRunner(t, pg, "r-reclaim-sibling"))

	// maxReclaims 0 fails the stale job on the first sweep.
	reclaimed, failed, err := pg.ReclaimStaleJobs(time.Now().Add(time.Hour), 0, "silent runner, giving up")
	if err != nil {
		t.Fatal(err)
	}
	if reclaimed != 0 || failed != 1 {
		t.Fatalf("reclaimed=%d failed=%d, want 0,1", reclaimed, failed)
	}
	after, err := pg.GetRemoteJob(jobID)
	if err != nil || after == nil {
		t.Fatalf("GetRemoteJob: %+v, %v", after, err)
	}
	if after.Status != domain.RemoteJobStatusFailed {
		t.Fatalf("job status = %q, want failed", after.Status)
	}

	run, err := pg.GetRun(runID)
	if err != nil || run == nil {
		t.Fatalf("GetRun = %+v, %v", run, err)
	}
	if run.Status != "running" {
		t.Fatalf("run status = %q, want running - deploy is still in flight", run.Status)
	}
	if run.FinishedAt != nil {
		t.Fatalf("run finished_at = %v, want nil", run.FinishedAt)
	}
}

// A run that is already terminal is left alone - a cancel that won the race
// is never undone by a late finish.
func TestPG_FinishRunIfDone_LeavesAFinishedRunAlone(t *testing.T) {
	for _, status := range []string{"passed", "failed", "canceled"} {
		t.Run(status, func(t *testing.T) {
			pg := testPG(t)
			mustUpsert(t, pg, "proj-terminal", "")
			runID := mustRunWithResults(t, pg, "proj-terminal", domain.TaskResult{Name: "build", Status: "passed"})
			if _, err := pg.db.Exec(`UPDATE runs SET status = $1 WHERE id = $2`, status, runID); err != nil {
				t.Fatal(err)
			}

			if err := pg.FinishRunIfDone(runID); err != nil {
				t.Fatalf("FinishRunIfDone: %v", err)
			}
			run, err := pg.GetRun(runID)
			if err != nil || run == nil {
				t.Fatalf("GetRun = %+v, %v", run, err)
			}
			if run.Status != status {
				t.Fatalf("run status = %q, want %q", run.Status, status)
			}
		})
	}
}

// Finishing a run prunes the run history, as FinishRun and the completion's
// own finishing do.
func TestPG_FinishRunIfDone_PrunesHistory(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "proj-prune", "")
	pg.SetRunHistoryLimit(1)

	older := mustRunWithResults(t, pg, "proj-prune", domain.TaskResult{Name: "build", Status: "passed"})
	if err := pg.FinishRunIfDone(older); err != nil {
		t.Fatalf("FinishRunIfDone(older): %v", err)
	}
	time.Sleep(5 * time.Millisecond)
	newer := mustRunWithResults(t, pg, "proj-prune", domain.TaskResult{Name: "build", Status: "passed"})
	if err := pg.FinishRunIfDone(newer); err != nil {
		t.Fatalf("FinishRunIfDone(newer): %v", err)
	}

	if run, _ := pg.GetRun(older); run != nil {
		t.Error("the older terminal run should have been pruned")
	}
	if run, err := pg.GetRun(newer); err != nil || run == nil {
		t.Fatalf("the newly finished run should still exist: err=%v run=%v", err, run)
	}
}

// A run that does not exist is not an error: the caller asked whether it is
// done, and it is not.
func TestPG_FinishRunIfDone_UnknownRunIsNoOp(t *testing.T) {
	pg := testPG(t)
	if err := pg.FinishRunIfDone(999999); err != nil {
		t.Fatalf("FinishRunIfDone: %v", err)
	}
}

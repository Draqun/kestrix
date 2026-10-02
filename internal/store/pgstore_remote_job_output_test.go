package store

import (
	"testing"
	"time"

	"kestrix/internal/domain"
)

// ── remote-run-ux D1: a task's result carries the job's whole output ────

// storedJobOutput reads remote_jobs.output, the per-job accumulation of
// every accepted trace append.
func storedJobOutput(t *testing.T, pg *PGStore, jobID int) string {
	t.Helper()
	var out string
	if err := pg.db.QueryRow(`SELECT output FROM remote_jobs WHERE id = $1`, jobID).Scan(&out); err != nil {
		t.Fatalf("read remote_jobs.output: %v", err)
	}
	return out
}

// latestTaskOutput returns the output of runID's most recent result for task.
func latestTaskOutput(t *testing.T, pg *PGStore, runID int, task string) string {
	t.Helper()
	run, err := pg.GetRun(runID)
	if err != nil || run == nil {
		t.Fatalf("GetRun = %+v, %v", run, err)
	}
	for i := len(run.TaskResults) - 1; i >= 0; i-- {
		if run.TaskResults[i].Name == task {
			return run.TaskResults[i].Output
		}
	}
	t.Fatalf("no result for task %q: %+v", task, run.TaskResults)
	return ""
}

func TestPG_AppendJobTrace_AccumulatesJobOutput(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "proj-job-output", "")
	runID := mustCreateRun(t, pg, "proj-job-output", nil)
	jobID := mustCreateRemoteJob(t, pg, runID, "build")
	job, _ := pg.ClaimNextJob(mustCreateRunner(t, pg, "r-job-output"))

	for _, c := range []struct {
		offset int
		chunk  string
	}{{0, "hello "}, {0, "hello "}, {6, "world"}} { // the middle one is a runner retry
		if ok, err := pg.AppendJobTrace(jobID, job.ClaimEpoch, c.offset, c.chunk, 0, nil); err != nil || !ok {
			t.Fatalf("AppendJobTrace(offset %d) = (%v, %v)", c.offset, ok, err)
		}
	}
	if got := storedJobOutput(t, pg, jobID); got != "hello world" {
		t.Fatalf("remote_jobs.output = %q, want %q", got, "hello world")
	}
}

func TestPG_AppendJobTrace_StaleEpochStoresNoJobOutput(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "proj-job-output-stale", "")
	runID := mustCreateRun(t, pg, "proj-job-output-stale", nil)
	jobID := mustCreateRemoteJob(t, pg, runID, "build")
	job, _ := pg.ClaimNextJob(mustCreateRunner(t, pg, "r-job-output-stale"))

	if ok, err := pg.AppendJobTrace(jobID, job.ClaimEpoch+1, 0, "ghost", 0, nil); err != nil || ok {
		t.Fatalf("stale AppendJobTrace = (%v, %v), want rejected", ok, err)
	}
	if got := storedJobOutput(t, pg, jobID); got != "" {
		t.Fatalf("remote_jobs.output = %q after a rejected append, want empty", got)
	}
}

// The bound is per task: a sibling job that already used up the run's
// aggregate budget must not shorten this job's own output.
func TestPG_AppendJobTrace_JobOutputBoundedPerJobNotPerRun(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "proj-job-output-bound", "")
	runID := mustCreateRun(t, pg, "proj-job-output-bound", nil)
	aID := mustCreateRemoteJob(t, pg, runID, "a")
	bID := mustCreateRemoteJob(t, pg, runID, "b")
	a, _ := pg.ClaimNextJob(mustCreateRunner(t, pg, "r-job-output-bound-a"))
	b, _ := pg.ClaimNextJob(mustCreateRunner(t, pg, "r-job-output-bound-b"))

	const bound = 6
	if _, err := pg.AppendJobTrace(aID, a.ClaimEpoch, 0, "aaaaa", bound, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := pg.AppendJobTrace(bID, b.ClaimEpoch, 0, "bbbbb", bound, nil); err != nil {
		t.Fatal(err)
	}
	if got := storedJobOutput(t, pg, bID); got != "bbbbb" {
		t.Fatalf("job b output = %q, want its full %q although the run aggregate is over budget", got, "bbbbb")
	}

	// Crossing its own bound, a job is cut and marked once, then drops the rest.
	if _, err := pg.AppendJobTrace(aID, a.ClaimEpoch, 5, "AAAAA", bound, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := pg.AppendJobTrace(aID, a.ClaimEpoch, 10, "MORE", bound, nil); err != nil {
		t.Fatal(err)
	}
	if got, want := storedJobOutput(t, pg, aID), "aaaaaA"+remoteJobTruncationMarker; got != want {
		t.Fatalf("job a output = %q, want %q", got, want)
	}
}

func TestPG_CompleteJob_TaskOutput(t *testing.T) {
	for _, tc := range []struct {
		name      string
		trace     []string
		remainder string
		bound     int
		want      string
	}{
		{"trace only", []string{"stream", "ed"}, "", 0, "streamed"},
		{"trace then remainder", []string{"abc"}, "def", 0, "abcdef"},
		{"remainder only", nil, "done", 0, "done"},
		{"trace over the bound", []string{"0123456789ABC"}, "", 10, "0123456789" + remoteJobTruncationMarker},
		{"remainder crosses the bound", []string{"abc"}, "0123456789", 10, "abc0123456" + remoteJobTruncationMarker},
		{"remainder after the marker is dropped", []string{"0123456789ABC"}, "MORE", 10, "0123456789" + remoteJobTruncationMarker},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pg := testPG(t)
			mustUpsert(t, pg, "proj-task-output", "")
			runID := mustCreateRun(t, pg, "proj-task-output", nil)
			jobID := mustCreateRemoteJob(t, pg, runID, "build")
			job, _ := pg.ClaimNextJob(mustCreateRunner(t, pg, "r-task-output"))
			offset := 0
			for _, chunk := range tc.trace {
				if ok, err := pg.AppendJobTrace(jobID, job.ClaimEpoch, offset, chunk, tc.bound, nil); err != nil || !ok {
					t.Fatalf("AppendJobTrace = (%v, %v)", ok, err)
				}
				offset += len(chunk)
			}

			if ok, err := pg.CompleteJob(jobID, job.ClaimEpoch, domain.RemoteJobStatusPassed, tc.remainder, "", nil, tc.bound); err != nil || !ok {
				t.Fatalf("CompleteJob = (%v, %v)", ok, err)
			}
			if got := latestTaskOutput(t, pg, runID, "build"); got != tc.want {
				t.Fatalf("task output = %q, want %q", got, tc.want)
			}
		})
	}
}

// The run's aggregate output is built exactly as before: the trace, then the
// completion's remainder on its own line.
func TestPG_CompleteJob_AggregateOutputUnchangedByTaskOutput(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "proj-task-output-agg", "")
	runID := mustCreateRun(t, pg, "proj-task-output-agg", nil)
	jobID := mustCreateRemoteJob(t, pg, runID, "build")
	job, _ := pg.ClaimNextJob(mustCreateRunner(t, pg, "r-task-output-agg"))
	if _, err := pg.AppendJobTrace(jobID, job.ClaimEpoch, 0, "abc", 0, nil); err != nil {
		t.Fatal(err)
	}
	if ok, err := pg.CompleteJob(jobID, job.ClaimEpoch, domain.RemoteJobStatusPassed, "def", "", nil, 0); err != nil || !ok {
		t.Fatalf("CompleteJob = (%v, %v)", ok, err)
	}
	run, err := pg.GetRun(runID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Output != "abc\ndef" {
		t.Fatalf("run.Output = %q, want %q", run.Output, "abc\ndef")
	}
}

// A reclaimed job's replacement claim starts its own trace at offset 0 (a
// fresh traceWriter), and the task's result comes only from the claim that
// completes, never from what the superseded claim had streamed.
func TestPG_ReclaimedJob_NewClaimTracesFromItsOwnStart(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "proj-reclaim-trace", "")
	runID := mustCreateRun(t, pg, "proj-reclaim-trace", nil)
	jobID := mustCreateRemoteJob(t, pg, runID, "build")
	first := mustCreateRunner(t, pg, "r-reclaim-trace-1")
	claim1, _ := pg.ClaimNextJob(first)
	if ok, err := pg.AppendJobTrace(jobID, claim1.ClaimEpoch, 0, "partial output of the first claim", 0, &first); err != nil || !ok {
		t.Fatalf("first claim AppendJobTrace = (%v, %v)", ok, err)
	}
	// The first runner goes silent: silent-runner recovery returns the job to pending.
	if reclaimed, _, err := pg.ReclaimStaleJobs(time.Now().Add(time.Hour), 3, "silent"); err != nil || reclaimed != 1 {
		t.Fatalf("ReclaimStaleJobs reclaimed %d, err %v; want 1", reclaimed, err)
	}

	second := mustCreateRunner(t, pg, "r-reclaim-trace-2")
	claim2, _ := pg.ClaimNextJob(second)
	for _, c := range []struct {
		offset int
		chunk  string
	}{{0, "second "}, {7, "claim"}} {
		if ok, err := pg.AppendJobTrace(jobID, claim2.ClaimEpoch, c.offset, c.chunk, 0, &second); err != nil || !ok {
			t.Fatalf("second claim AppendJobTrace(offset %d) = (%v, %v)", c.offset, ok, err)
		}
	}
	if ok, err := pg.CompleteJob(jobID, claim2.ClaimEpoch, domain.RemoteJobStatusPassed, "", "", &second, 0); err != nil || !ok {
		t.Fatalf("CompleteJob = (%v, %v)", ok, err)
	}
	if got := latestTaskOutput(t, pg, runID, "build"); got != "second claim" {
		t.Fatalf("task output = %q, want only the completing claim's %q", got, "second claim")
	}
}

// remote-run-ux design.md: the job-row copy of the output exists only until the
// task result is written, so a finished job must not keep up to
// max_output_bytes in its row - on the recorded path or the fenced one.
func TestPG_CompleteJob_ClearsTheJobsOutput(t *testing.T) {
	for _, tc := range []struct {
		name      string
		runIsOver bool // FailInterruptedRuns finalized the run before the report arrived
	}{
		{"result recorded", false},
		{"run already terminal (fenced)", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pg := testPG(t)
			mustUpsert(t, pg, "proj-job-output-cleared", "")
			runID := mustCreateRun(t, pg, "proj-job-output-cleared", nil)
			jobID := mustCreateRemoteJob(t, pg, runID, "build")
			job, _ := pg.ClaimNextJob(mustCreateRunner(t, pg, "r-job-output-cleared"))
			if ok, err := pg.AppendJobTrace(jobID, job.ClaimEpoch, 0, "streamed output", 0, nil); err != nil || !ok {
				t.Fatalf("AppendJobTrace = (%v, %v)", ok, err)
			}
			if got := storedJobOutput(t, pg, jobID); got != "streamed output" {
				t.Fatalf("remote_jobs.output = %q before completion, want the trace", got)
			}
			if tc.runIsOver {
				if _, err := pg.FailInterruptedRuns("interrupted"); err != nil {
					t.Fatal(err)
				}
			}

			ok, err := pg.CompleteJob(jobID, job.ClaimEpoch, domain.RemoteJobStatusPassed, "", "", nil, 0)
			if err != nil || ok == tc.runIsOver {
				t.Fatalf("CompleteJob = (%v, %v), want ok=%v", ok, err, !tc.runIsOver)
			}
			if got := storedJobOutput(t, pg, jobID); got != "" {
				t.Fatalf("remote_jobs.output = %q after completion, want it cleared", got)
			}
		})
	}
}

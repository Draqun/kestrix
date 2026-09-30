package store

import (
	"testing"

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

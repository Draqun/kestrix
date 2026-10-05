package store

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"kestrix/internal/domain"
)

// ── remote-run-ux D1: a task's result carries the completing claim's output ──

// storedJobOutput reads remote_jobs.output, the accumulation of every trace
// append accepted for the job's current claim.
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
		{"remainder goes on its own line after a trace that ends mid-line", []string{"abc"}, "def", 0, "abc\ndef"},
		{"no extra line break after a trace that ends in one", []string{"abc\n"}, "def", 0, "abc\ndef"},
		{"remainder only", nil, "done", 0, "done"},
		{"trace over the bound", []string{"0123456789ABC"}, "", 10, "0123456789" + remoteJobTruncationMarker},
		{"remainder crosses the bound", []string{"abc"}, "0123456789", 10, "abc\n012345" + remoteJobTruncationMarker},
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
// completes, never from what the superseded claim had streamed. The run's
// aggregate output, unlike the task's, keeps both streams
// (docs/remote-runners.md, "Task output").
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
	run, err := pg.GetRun(runID)
	if err != nil || run == nil {
		t.Fatalf("GetRun = %+v, %v", run, err)
	}
	for _, want := range []string{"partial output of the first claim", "second claim"} {
		if !strings.Contains(run.Output, want) {
			t.Fatalf("run.Output = %q, want the aggregate to keep %q", run.Output, want)
		}
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

// boundAppendChunk cuts on a byte count, but the text is UTF-8 and PostgreSQL
// rejects an invalid sequence in a text parameter, so a cut must never split a
// character.
func TestBoundAppendChunk_NeverSplitsACharacter(t *testing.T) {
	for _, tc := range []struct {
		name, current, chunk string
		max                  int
		want                 string
	}{
		{"cut inside a 3-byte character backs off to its start", "", "€€€", 4, "€" + remoteJobTruncationMarker},
		{"cut on a character boundary is kept", "", "€€€", 6, "€€" + remoteJobTruncationMarker},
		{"4-byte character", "", "😀😀", 5, "😀" + remoteJobTruncationMarker},
		{"budget smaller than the first character leaves only the marker", "", "€", 2, remoteJobTruncationMarker},
		{"budget left by earlier output", "ab", "€€", 4, remoteJobTruncationMarker},
		{"fits exactly", "", "€€", 6, "€€"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := boundAppendChunk(tc.current, tc.chunk, tc.max)
			if got != tc.want {
				t.Fatalf("boundAppendChunk(%q, %q, %d) = %q, want %q", tc.current, tc.chunk, tc.max, got, tc.want)
			}
			if !utf8.ValidString(got) {
				t.Fatalf("result %q is not valid UTF-8", got)
			}
		})
	}
}

// The cut has to survive the database too: an append that crossed the bound
// inside a multi-byte character used to fail the whole request.
func TestPG_MultiByteOutputAtTheBound(t *testing.T) {
	for _, tc := range []struct {
		name, trace, remainder string
	}{
		{"trace chunk", "€€€", ""},
		{"completion remainder", "", "€€€"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pg := testPG(t)
			mustUpsert(t, pg, "proj-multibyte-bound", "")
			runID := mustCreateRun(t, pg, "proj-multibyte-bound", nil)
			jobID := mustCreateRemoteJob(t, pg, runID, "build")
			job, _ := pg.ClaimNextJob(mustCreateRunner(t, pg, "r-multibyte-bound"))
			const bound = 4
			want := "€" + remoteJobTruncationMarker

			if tc.trace != "" {
				if ok, err := pg.AppendJobTrace(jobID, job.ClaimEpoch, 0, tc.trace, bound, nil); err != nil || !ok {
					t.Fatalf("AppendJobTrace = (%v, %v)", ok, err)
				}
				if got := storedJobOutput(t, pg, jobID); got != want {
					t.Fatalf("remote_jobs.output = %q, want %q", got, want)
				}
			}
			if ok, err := pg.CompleteJob(jobID, job.ClaimEpoch, domain.RemoteJobStatusPassed, tc.remainder, "", nil, bound); err != nil || !ok {
				t.Fatalf("CompleteJob = (%v, %v)", ok, err)
			}
			if got := latestTaskOutput(t, pg, runID, "build"); got != want {
				t.Fatalf("task output = %q, want %q", got, want)
			}
			run, err := pg.GetRun(runID)
			if err != nil || run.Output != want {
				t.Fatalf("run.Output = %q (err %v), want %q", run.Output, err, want)
			}
		})
	}
}

// The bound is in bytes, not characters: with 6 bytes of "€€" in the column and
// a bound of 7, one more byte fits and the second does not.
func TestPG_AppendJobTrace_JobOutputBoundCountsBytes(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "proj-job-output-bytes", "")
	runID := mustCreateRun(t, pg, "proj-job-output-bytes", nil)
	jobID := mustCreateRemoteJob(t, pg, runID, "build")
	job, _ := pg.ClaimNextJob(mustCreateRunner(t, pg, "r-job-output-bytes"))

	const bound = 7
	offset := 0
	for _, chunk := range []string{"€€", "ab", "more"} {
		if ok, err := pg.AppendJobTrace(jobID, job.ClaimEpoch, offset, chunk, bound, nil); err != nil || !ok {
			t.Fatalf("AppendJobTrace(%q) = (%v, %v)", chunk, ok, err)
		}
		offset += len(chunk)
	}
	if got, want := storedJobOutput(t, pg, jobID), "€€a"+remoteJobTruncationMarker; got != want {
		t.Fatalf("remote_jobs.output = %q, want %q", got, want)
	}
}

// Cancelling is a third way a job ends. A job that streamed output, was
// returned to pending by silent-runner recovery (which keeps the output until
// the next claim) and is cancelled before any runner re-claims it must not
// keep that output either.
func TestPG_CancelJobsForRun_ClearsTheOutputOfAReclaimedJob(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "proj-cancel-reclaimed", "")
	runID := mustCreateRun(t, pg, "proj-cancel-reclaimed", nil)
	jobID := mustCreateRemoteJob(t, pg, runID, "build")
	runner := mustCreateRunner(t, pg, "r-cancel-reclaimed")
	claim, _ := pg.ClaimNextJob(runner)
	if ok, err := pg.AppendJobTrace(jobID, claim.ClaimEpoch, 0, "partial output", 0, &runner); err != nil || !ok {
		t.Fatalf("AppendJobTrace = (%v, %v)", ok, err)
	}
	if reclaimed, _, err := pg.ReclaimStaleJobs(time.Now().Add(time.Hour), 3, "silent"); err != nil || reclaimed != 1 {
		t.Fatalf("ReclaimStaleJobs reclaimed %d, err %v; want 1", reclaimed, err)
	}

	if err := pg.CancelJobsForRun(runID); err != nil {
		t.Fatalf("CancelJobsForRun: %v", err)
	}
	if job, _ := pg.GetRemoteJob(jobID); job.Status != domain.RemoteJobStatusCanceled {
		t.Fatalf("job status = %q, want canceled", job.Status)
	}
	if got := storedJobOutput(t, pg, jobID); got != "" {
		t.Fatalf("remote_jobs.output = %q after cancelling, want it cleared", got)
	}
}

// Past the bound a chunk is dropped, but the stream must still advance: the
// next chunk's offset is checked against trace_offset (a miss is rejected as a
// gap), and silent-runner recovery treats updated_at as the runner's
// heartbeat, so a healthy runner that keeps printing must not look silent.
func TestPG_AppendJobTrace_ChunksPastTheBoundStillAdvanceTheStream(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "proj-past-bound", "")
	runID := mustCreateRun(t, pg, "proj-past-bound", nil)
	jobID := mustCreateRemoteJob(t, pg, runID, "build")
	job, _ := pg.ClaimNextJob(mustCreateRunner(t, pg, "r-past-bound"))

	const bound = 4
	offset := 0
	send := func(chunk string) {
		t.Helper()
		if ok, err := pg.AppendJobTrace(jobID, job.ClaimEpoch, offset, chunk, bound, nil); err != nil || !ok {
			t.Fatalf("AppendJobTrace(%q at offset %d) = (%v, %v)", chunk, offset, ok, err)
		}
		offset += len(chunk)
	}
	send("abcdef") // crosses the bound: "abcd" and the marker
	want := "abcd" + remoteJobTruncationMarker

	for _, chunk := range []string{"ghi", "jkl"} { // both dropped
		if _, err := pg.db.Exec(`UPDATE remote_jobs SET updated_at = now() - interval '1 hour' WHERE id = $1`, jobID); err != nil {
			t.Fatal(err)
		}
		send(chunk)

		var fresh bool
		var traceOffset int
		if err := pg.db.QueryRow(
			`SELECT now() - updated_at < interval '1 minute', trace_offset FROM remote_jobs WHERE id = $1`, jobID,
		).Scan(&fresh, &traceOffset); err != nil {
			t.Fatal(err)
		}
		if !fresh {
			t.Fatalf("updated_at was not refreshed by the dropped chunk %q", chunk)
		}
		if traceOffset != offset {
			t.Fatalf("trace_offset = %d after the dropped chunk %q, want %d", traceOffset, chunk, offset)
		}
		if got := storedJobOutput(t, pg, jobID); got != want {
			t.Fatalf("remote_jobs.output = %q after the dropped chunk %q, want it unchanged: %q", got, chunk, want)
		}
	}
}

// lineBreakBefore is the one place for "this text goes on its own line": the
// run aggregate, a task's output, an interrupted run's reason and an applied
// task result's extra output all use it.
func TestLineBreakBefore(t *testing.T) {
	for _, tc := range []struct{ name, existing, chunk, want string }{
		{"nothing before it", "", "x", "x"},
		{"existing output ends mid-line", "abc", "x", "\nx"},
		{"existing output ends in a newline", "abc\n", "x", "x"},
		{"nothing to add", "abc", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := lineBreakBefore(tc.existing, tc.chunk); got != tc.want {
				t.Fatalf("lineBreakBefore(%q, %q) = %q, want %q", tc.existing, tc.chunk, got, tc.want)
			}
		})
	}
}

// ApplyTaskResult puts a task's extra output on its own line after an
// aggregate that ends mid-line, and adds no blank line after one that does not.
func TestPG_ApplyTaskResult_PutsExtraOutputOnItsOwnLine(t *testing.T) {
	for _, tc := range []struct{ name, existing, extra, want string }{
		{"aggregate ends mid-line", "abc", "extra", "abc\nextra"},
		{"aggregate ends in a newline", "abc\n", "extra", "abc\nextra"},
		{"no extra output", "abc", "", "abc"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pg := testPG(t)
			mustUpsert(t, pg, "proj-apply-line", "")
			runID := mustCreateRun(t, pg, "proj-apply-line", nil)
			if err := pg.FlushRunOutput(runID, tc.existing); err != nil {
				t.Fatal(err)
			}
			if _, err := pg.ApplyTaskResult(runID, TaskResult{Name: "build", Status: "running"}, tc.extra); err != nil {
				t.Fatalf("ApplyTaskResult: %v", err)
			}
			run, err := pg.GetRun(runID)
			if err != nil || run.Output != tc.want {
				t.Fatalf("run.Output = %q (err %v), want %q", run.Output, err, tc.want)
			}
		})
	}
}

// FailInterruptedRuns names the interruption on a line of its own.
func TestPG_FailInterruptedRuns_PutsTheReasonOnItsOwnLine(t *testing.T) {
	for _, tc := range []struct{ name, existing, want string }{
		{"output ends mid-line", "abc", "abc\ninterrupted"},
		{"output ends in a newline", "abc\n", "abc\ninterrupted"},
		{"no output yet", "", "interrupted"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pg := testPG(t)
			mustUpsert(t, pg, "proj-interrupted-line", "")
			runID := mustCreateRun(t, pg, "proj-interrupted-line", nil)
			if err := pg.FlushRunOutput(runID, tc.existing); err != nil {
				t.Fatal(err)
			}
			if n, err := pg.FailInterruptedRuns("interrupted"); err != nil || n != 1 {
				t.Fatalf("FailInterruptedRuns = (%d, %v), want (1, nil)", n, err)
			}
			run, err := pg.GetRun(runID)
			if err != nil || run.Output != tc.want {
				t.Fatalf("run.Output = %q (err %v), want %q", run.Output, err, tc.want)
			}
		})
	}
}

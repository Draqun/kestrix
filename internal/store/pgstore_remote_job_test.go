package store

import (
	"sync"
	"testing"
	"time"

	"kestrix/internal/domain"
)

func mustCreateRunner(t *testing.T, pg *PGStore, name string) int {
	t.Helper()
	id, _, err := pg.CreateRunner(name, nil)
	if err != nil {
		t.Fatalf("CreateRunner(%q, nil): %v", name, err)
	}
	return id
}

func mustCreateRemoteJob(t *testing.T, pg *PGStore, runID int, taskName string) int {
	t.Helper()
	id, err := pg.CreateRemoteJob(runID, taskName, domain.RemoteJobPayload{
		Task: domain.Task{Name: taskName, Execute: domain.ExecuteSpec{Repo: "checkout", Script: "echo hi"}},
	})
	if err != nil {
		t.Fatalf("CreateRemoteJob: %v", err)
	}
	return id
}

// ── RunnerStore ────────────────────────────────────────────────────

func TestPG_CreateAndAuthenticateRunner(t *testing.T) {
	pg := testPG(t)
	id, token, err := pg.CreateRunner("runner-a", nil)
	if err != nil {
		t.Fatalf("CreateRunner: %v", err)
	}
	if token == "" {
		t.Fatal("token is empty")
	}
	gotID, ok, err := pg.AuthenticateRunner(token)
	if err != nil {
		t.Fatalf("AuthenticateRunner: %v", err)
	}
	if !ok || gotID != id {
		t.Fatalf("AuthenticateRunner = (%d, %v), want (%d, true)", gotID, ok, id)
	}
}

func TestPG_AuthenticateRunner_UnknownToken(t *testing.T) {
	pg := testPG(t)
	_, ok, err := pg.AuthenticateRunner("not-a-real-token")
	if err != nil {
		t.Fatalf("AuthenticateRunner: %v", err)
	}
	if ok {
		t.Fatal("expected ok=false for unknown token")
	}
}

func TestPG_RevokeRunner_RejectsToken(t *testing.T) {
	pg := testPG(t)
	id, token, err := pg.CreateRunner("runner-b", nil)
	if err != nil {
		t.Fatalf("CreateRunner: %v", err)
	}
	if err := pg.RevokeRunner(id); err != nil {
		t.Fatalf("RevokeRunner: %v", err)
	}
	_, ok, err := pg.AuthenticateRunner(token)
	if err != nil {
		t.Fatalf("AuthenticateRunner: %v", err)
	}
	if ok {
		t.Fatal("expected revoked token to be rejected")
	}
}

// ── RemoteJobStore: claim ──────────────────────────────────────────

func TestPG_ClaimNextJob_NoneWhenEmpty(t *testing.T) {
	pg := testPG(t)
	runnerID := mustCreateRunner(t, pg, "r-empty")
	job, err := pg.ClaimNextJob(runnerID)
	if err != nil {
		t.Fatalf("ClaimNextJob: %v", err)
	}
	if job != nil {
		t.Fatalf("expected nil job, got %+v", job)
	}
}

func TestPG_ClaimNextJob_ClaimsPendingJob(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "proj-claim", "")
	runID := mustCreateRun(t, pg, "proj-claim", nil)
	jobID := mustCreateRemoteJob(t, pg, runID, "build")
	runnerID := mustCreateRunner(t, pg, "r-claim-1")

	job, err := pg.ClaimNextJob(runnerID)
	if err != nil {
		t.Fatalf("ClaimNextJob: %v", err)
	}
	if job == nil {
		t.Fatal("expected a claimed job")
	}
	if job.ID != jobID || job.Status != domain.RemoteJobStatusClaimed || job.RunnerID == nil || *job.RunnerID != runnerID {
		t.Fatalf("claimed job = %+v", job)
	}
	if job.ClaimEpoch != 1 {
		t.Fatalf("ClaimEpoch = %d, want 1", job.ClaimEpoch)
	}
	if job.Payload.Task.Name != "build" {
		t.Fatalf("payload task name = %q, want build", job.Payload.Task.Name)
	}

	// Now claimed, not pending: a second claim finds nothing.
	otherRunnerID := mustCreateRunner(t, pg, "r-claim-2")
	again, err := pg.ClaimNextJob(otherRunnerID)
	if err != nil {
		t.Fatalf("ClaimNextJob (second): %v", err)
	}
	if again != nil {
		t.Fatalf("expected no job left to claim, got %+v", again)
	}
}

// TestPG_ClaimNextJob_ConcurrentClaimsExactlyOnce is the claim-race coverage
// tasks.md 1.2 requires: two concurrent claims against one pending job must
// result in exactly one winner, via SELECT ... FOR UPDATE SKIP LOCKED.
func TestPG_ClaimNextJob_ConcurrentClaimsExactlyOnce(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "proj-race", "")
	runID := mustCreateRun(t, pg, "proj-race", nil)
	mustCreateRemoteJob(t, pg, runID, "build")

	const attempts = 8
	runnerIDs := make([]int, attempts)
	for i := range runnerIDs {
		runnerIDs[i] = mustCreateRunner(t, pg, "r-race-"+string(rune('a'+i)))
	}

	var wg sync.WaitGroup
	claims := make([]*domain.RemoteJob, attempts)
	errs := make([]error, attempts)
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			claims[i], errs[i] = pg.ClaimNextJob(runnerIDs[i])
		}(i)
	}
	wg.Wait()

	won := 0
	for i, c := range claims {
		if errs[i] != nil {
			t.Fatalf("ClaimNextJob[%d]: %v", i, errs[i])
		}
		if c != nil {
			won++
		}
	}
	if won != 1 {
		t.Fatalf("winners = %d, want exactly 1", won)
	}
}

// ── RemoteJobStore: trace append ──────────────────────────────────

func TestPG_AppendJobTrace_AppendsAndAdvancesToRunning(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "proj-trace", "")
	runID := mustCreateRun(t, pg, "proj-trace", nil)
	jobID := mustCreateRemoteJob(t, pg, runID, "build")
	job, _ := pg.ClaimNextJob(mustCreateRunner(t, pg, "r-trace"))

	ok, err := pg.AppendJobTrace(jobID, job.ClaimEpoch, 0, "hello ", 0, nil)
	if err != nil || !ok {
		t.Fatalf("AppendJobTrace #1 = (%v, %v)", ok, err)
	}
	ok, err = pg.AppendJobTrace(jobID, job.ClaimEpoch, 6, "world", 0, nil)
	if err != nil || !ok {
		t.Fatalf("AppendJobTrace #2 = (%v, %v)", ok, err)
	}

	run, err := pg.GetRun(runID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Output != "hello world" {
		t.Fatalf("run.Output = %q, want %q", run.Output, "hello world")
	}
	after, err := pg.GetRemoteJob(jobID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != domain.RemoteJobStatusRunning {
		t.Fatalf("job status = %q, want running", after.Status)
	}
}

func TestPG_AppendJobTrace_TwoJobsOfOneRunBothSurvive(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "proj-two-jobs", "")
	runID := mustCreateRun(t, pg, "proj-two-jobs", nil)
	job1ID := mustCreateRemoteJob(t, pg, runID, "a")
	job2ID := mustCreateRemoteJob(t, pg, runID, "b")
	j1, _ := pg.ClaimNextJob(mustCreateRunner(t, pg, "r-two-1"))
	j2, _ := pg.ClaimNextJob(mustCreateRunner(t, pg, "r-two-2"))
	if j1.ID != job1ID || j2.ID != job2ID {
		t.Fatalf("unexpected claim assignment: %+v %+v", j1, j2)
	}

	if _, err := pg.AppendJobTrace(job1ID, j1.ClaimEpoch, 0, "A", 0, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := pg.AppendJobTrace(job2ID, j2.ClaimEpoch, 0, "B", 0, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := pg.AppendJobTrace(job1ID, j1.ClaimEpoch, 1, "A2", 0, nil); err != nil {
		t.Fatal(err)
	}

	run, err := pg.GetRun(runID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Output != "ABA2" {
		t.Fatalf("run.Output = %q, want a run of all three chunks (order not otherwise guaranteed, but none lost)", run.Output)
	}
}

func TestPG_AppendJobTrace_StaleEpochRejected(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "proj-stale-trace", "")
	runID := mustCreateRun(t, pg, "proj-stale-trace", nil)
	jobID := mustCreateRemoteJob(t, pg, runID, "build")
	job, _ := pg.ClaimNextJob(mustCreateRunner(t, pg, "r-stale-trace"))

	ok, err := pg.AppendJobTrace(jobID, job.ClaimEpoch+1, 0, "ghost", 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected stale-epoch append to be rejected")
	}
	run, err := pg.GetRun(runID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Output != "" {
		t.Fatalf("run.Output = %q, want empty after rejected append", run.Output)
	}
}

func TestPG_AppendJobTrace_BoundedTruncatesOnce(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "proj-bound", "")
	runID := mustCreateRun(t, pg, "proj-bound", nil)
	jobID := mustCreateRemoteJob(t, pg, runID, "build")
	job, _ := pg.ClaimNextJob(mustCreateRunner(t, pg, "r-bound"))

	const bound = 10
	if _, err := pg.AppendJobTrace(jobID, job.ClaimEpoch, 0, "0123456789", bound, nil); err != nil {
		t.Fatal(err)
	}
	// Crossing chunk: cut at the bound (already exactly at it here, so this
	// one contributes only the marker) then a later chunk is dropped.
	if _, err := pg.AppendJobTrace(jobID, job.ClaimEpoch, 10, "OVERFLOW", bound, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := pg.AppendJobTrace(jobID, job.ClaimEpoch, 18, "MORE", bound, nil); err != nil {
		t.Fatal(err)
	}

	run, err := pg.GetRun(runID)
	if err != nil {
		t.Fatal(err)
	}
	want := "0123456789" + domain.TruncationMarker(bound)
	if run.Output != want {
		t.Fatalf("run.Output = %q, want %q", run.Output, want)
	}

	// The job still completes normally after truncation - it is not failed
	// by the bound.
	if ok, err := pg.CompleteJob(jobID, job.ClaimEpoch, domain.RemoteJobStatusPassed, "", "", nil, 0); err != nil || !ok {
		t.Fatalf("CompleteJob after truncation = (%v, %v)", ok, err)
	}
}

func TestPG_AppendJobTrace_DuplicateOffsetIsNoOp(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "proj-dup-offset", "")
	runID := mustCreateRun(t, pg, "proj-dup-offset", nil)
	jobID := mustCreateRemoteJob(t, pg, runID, "build")
	job, _ := pg.ClaimNextJob(mustCreateRunner(t, pg, "r-dup-offset"))

	if ok, err := pg.AppendJobTrace(jobID, job.ClaimEpoch, 0, "hello", 0, nil); err != nil || !ok {
		t.Fatalf("first append = (%v, %v)", ok, err)
	}
	// A retry of the same chunk at the same offset (as after a dropped HTTP
	// response) is a safe no-op, not an error.
	if ok, err := pg.AppendJobTrace(jobID, job.ClaimEpoch, 0, "hello", 0, nil); err != nil || !ok {
		t.Fatalf("duplicate append = (%v, %v)", ok, err)
	}
	run, err := pg.GetRun(runID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Output != "hello" {
		t.Fatalf("run.Output = %q, want %q (duplicate must not double-apply)", run.Output, "hello")
	}
}

func TestPG_AppendJobTrace_GappedOffsetRejected(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "proj-gap-offset", "")
	runID := mustCreateRun(t, pg, "proj-gap-offset", nil)
	jobID := mustCreateRemoteJob(t, pg, runID, "build")
	job, _ := pg.ClaimNextJob(mustCreateRunner(t, pg, "r-gap-offset"))

	// Nothing has been appended yet, so offset 0 is expected; offset 5 is a
	// gap - reject rather than silently misplace content.
	if _, err := pg.AppendJobTrace(jobID, job.ClaimEpoch, 5, "world", 0, nil); err == nil {
		t.Fatal("expected an error for a gapped offset")
	}
	run, err := pg.GetRun(runID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Output != "" {
		t.Fatalf("run.Output = %q, want empty after a rejected gapped append", run.Output)
	}
}

func TestPG_AppendJobTrace_WrongRunnerRejected(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "proj-trace-owner", "")
	runID := mustCreateRun(t, pg, "proj-trace-owner", nil)
	jobID := mustCreateRemoteJob(t, pg, runID, "build")
	job, _ := pg.ClaimNextJob(mustCreateRunner(t, pg, "r-owner-trace"))
	intruder := mustCreateRunner(t, pg, "r-intruder-trace")

	ok, err := pg.AppendJobTrace(jobID, job.ClaimEpoch, 0, "hijack", 0, &intruder)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected trace from a runner that did not claim this job to be rejected")
	}
	run, err := pg.GetRun(runID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Output != "" {
		t.Fatalf("run.Output = %q, want empty after a rejected cross-runner trace", run.Output)
	}
}

func TestPG_AppendJobTrace_NeverClaimedJobRejectsGuessedZeroEpoch(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "proj-trace-unclaimed", "")
	runID := mustCreateRun(t, pg, "proj-trace-unclaimed", nil)
	jobID := mustCreateRemoteJob(t, pg, runID, "build")
	attacker := mustCreateRunner(t, pg, "r-attacker-trace")

	// A never-claimed job's claim_epoch defaults to 0 - a value a caller can
	// simply guess without ever claiming anything. The ownership check must
	// reject it regardless, since the job's stored runner_id is NULL.
	ok, err := pg.AppendJobTrace(jobID, 0, 0, "forged", 0, &attacker)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected a trace on a never-claimed job to be rejected even with a correctly-guessed epoch of 0")
	}
}

func TestPG_AppendJobTrace_OwningRunnerSucceeds(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "proj-trace-owner-ok", "")
	runID := mustCreateRun(t, pg, "proj-trace-owner-ok", nil)
	jobID := mustCreateRemoteJob(t, pg, runID, "build")
	runnerID := mustCreateRunner(t, pg, "r-owner-trace-ok")
	job, _ := pg.ClaimNextJob(runnerID)

	ok, err := pg.AppendJobTrace(jobID, job.ClaimEpoch, 0, "hello", 0, &runnerID)
	if err != nil || !ok {
		t.Fatalf("AppendJobTrace from the owning runner = (%v, %v)", ok, err)
	}
}

func TestPG_AppendJobTrace_UnknownJob(t *testing.T) {
	pg := testPG(t)
	ok, err := pg.AppendJobTrace(999999, 1, 0, "x", 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected ok=false for unknown job")
	}
}

// ── RemoteJobStore: complete ───────────────────────────────────────

func TestPG_CompleteJob_RecordsResultAndResumesNothingBlocked(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "proj-complete", "")
	runID := mustCreateRun(t, pg, "proj-complete", nil)
	jobID := mustCreateRemoteJob(t, pg, runID, "build")
	job, _ := pg.ClaimNextJob(mustCreateRunner(t, pg, "r-complete"))

	ok, err := pg.CompleteJob(jobID, job.ClaimEpoch, domain.RemoteJobStatusPassed, "build output", "", nil, 0)
	if err != nil || !ok {
		t.Fatalf("CompleteJob = (%v, %v)", ok, err)
	}
	run, err := pg.GetRun(runID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "passed" {
		t.Fatalf("run.Status = %q, want passed", run.Status)
	}
	if run.Output != "build output" {
		t.Fatalf("run.Output = %q, want %q", run.Output, "build output")
	}
	after, err := pg.GetRemoteJob(jobID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != domain.RemoteJobStatusPassed {
		t.Fatalf("job status = %q, want passed", after.Status)
	}
}

func TestPG_CompleteJob_OutputBoundedByMaxOutputBytes(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "proj-complete-bound", "")
	runID := mustCreateRun(t, pg, "proj-complete-bound", nil)
	jobID := mustCreateRemoteJob(t, pg, runID, "build")
	job, _ := pg.ClaimNextJob(mustCreateRunner(t, pg, "r-complete-bound"))

	const bound = 10
	ok, err := pg.CompleteJob(jobID, job.ClaimEpoch, domain.RemoteJobStatusPassed, "0123456789OVERFLOW", "", nil, bound)
	if err != nil || !ok {
		t.Fatalf("CompleteJob = (%v, %v)", ok, err)
	}
	run, err := pg.GetRun(runID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Output != "0123456789"+domain.TruncationMarker(bound) {
		t.Fatalf("run.Output = %q, want the output cut at the bound plus the truncation marker", run.Output)
	}
}

func TestPG_CompleteJob_FencedRunOutputAlsoBounded(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "proj-complete-bound-fenced", "")
	runID := mustCreateRun(t, pg, "proj-complete-bound-fenced", nil)
	jobID := mustCreateRemoteJob(t, pg, runID, "build")
	job, _ := pg.ClaimNextJob(mustCreateRunner(t, pg, "r-complete-bound-fenced"))

	// FailInterruptedRuns leaves its reason string as the run's output,
	// finalizing the run before this job's own completion report arrives -
	// exactly the fenced path this test targets.
	if _, err := pg.FailInterruptedRuns("interrupted"); err != nil {
		t.Fatal(err)
	}
	priorOutput, err := pg.GetRun(runID)
	if err != nil {
		t.Fatal(err)
	}

	bound := len(priorOutput.Output) + 5
	ok, err := pg.CompleteJob(jobID, job.ClaimEpoch, domain.RemoteJobStatusPassed, "0123456789", "", nil, bound)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected ok=false for a completion against an already-terminal run")
	}
	run, err := pg.GetRun(runID)
	if err != nil {
		t.Fatal(err)
	}
	want := priorOutput.Output + "01234" + domain.TruncationMarker(bound)
	if run.Output != want {
		t.Fatalf("run.Output = %q, want %q (the fenced-path append bounded too)", run.Output, want)
	}
}

func TestPG_CompleteJob_StaleEpochRejected(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "proj-complete-stale", "")
	runID := mustCreateRun(t, pg, "proj-complete-stale", nil)
	jobID := mustCreateRemoteJob(t, pg, runID, "build")
	job, _ := pg.ClaimNextJob(mustCreateRunner(t, pg, "r-complete-stale"))

	ok, err := pg.CompleteJob(jobID, job.ClaimEpoch+1, domain.RemoteJobStatusPassed, "ghost", "", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected stale-epoch completion to be rejected")
	}
	run, err := pg.GetRun(runID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status == "passed" {
		t.Fatal("run must not be marked passed by a stale-epoch completion")
	}
}

func TestPG_CompleteJob_WrongRunnerRejected(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "proj-complete-owner", "")
	runID := mustCreateRun(t, pg, "proj-complete-owner", nil)
	jobID := mustCreateRemoteJob(t, pg, runID, "build")
	job, _ := pg.ClaimNextJob(mustCreateRunner(t, pg, "r-owner-complete"))
	intruder := mustCreateRunner(t, pg, "r-intruder-complete")

	ok, err := pg.CompleteJob(jobID, job.ClaimEpoch, domain.RemoteJobStatusPassed, "forged", "", &intruder, 0)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected completion from a runner that did not claim this job to be rejected")
	}
	run, err := pg.GetRun(runID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status == "passed" {
		t.Fatal("run must not be marked passed by a cross-runner completion")
	}
}

func TestPG_CompleteJob_NeverClaimedJobRejectsGuessedZeroEpoch(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "proj-complete-unclaimed", "")
	runID := mustCreateRun(t, pg, "proj-complete-unclaimed", nil)
	jobID := mustCreateRemoteJob(t, pg, runID, "build")
	attacker := mustCreateRunner(t, pg, "r-attacker-complete")

	ok, err := pg.CompleteJob(jobID, 0, domain.RemoteJobStatusPassed, "forged", "", &attacker, 0)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected completion of a never-claimed job to be rejected even with a correctly-guessed epoch of 0")
	}
	run, err := pg.GetRun(runID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status == "passed" {
		t.Fatal("run must not be marked passed by a forged completion of an unclaimed job")
	}
}

func TestPG_CompleteJob_OwningRunnerSucceeds(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "proj-complete-owner-ok", "")
	runID := mustCreateRun(t, pg, "proj-complete-owner-ok", nil)
	jobID := mustCreateRemoteJob(t, pg, runID, "build")
	runnerID := mustCreateRunner(t, pg, "r-owner-complete-ok")
	job, _ := pg.ClaimNextJob(runnerID)

	ok, err := pg.CompleteJob(jobID, job.ClaimEpoch, domain.RemoteJobStatusPassed, "build output", "", &runnerID, 0)
	if err != nil || !ok {
		t.Fatalf("CompleteJob from the owning runner = (%v, %v)", ok, err)
	}
}

func TestPG_CompleteJob_NilCallerBypassesOwnershipForSystemCalls(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "proj-complete-system", "")
	runID := mustCreateRun(t, pg, "proj-complete-system", nil)
	jobID := mustCreateRemoteJob(t, pg, runID, "build")
	job, _ := pg.ClaimNextJob(mustCreateRunner(t, pg, "r-system"))

	// A nil callerRunnerID is what ReclaimStaleJobs's own internal fail-path
	// uses - not attributed to any authenticated runner, so it must still be
	// able to finalize the job regardless of who claimed it.
	ok, err := pg.CompleteJob(jobID, job.ClaimEpoch, domain.RemoteJobStatusFailed, "reclaimed", "", nil, 0)
	if err != nil || !ok {
		t.Fatalf("CompleteJob with nil callerRunnerID = (%v, %v)", ok, err)
	}
}

func TestPG_CompleteJob_DuplicateForSameEpochIsNoOp(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "proj-dup", "")
	runID := mustCreateRun(t, pg, "proj-dup", nil)
	jobID := mustCreateRemoteJob(t, pg, runID, "build")
	job, _ := pg.ClaimNextJob(mustCreateRunner(t, pg, "r-dup"))

	if ok, err := pg.CompleteJob(jobID, job.ClaimEpoch, domain.RemoteJobStatusPassed, "first", "", nil, 0); err != nil || !ok {
		t.Fatalf("first CompleteJob = (%v, %v)", ok, err)
	}
	ok, err := pg.CompleteJob(jobID, job.ClaimEpoch, domain.RemoteJobStatusPassed, "second", "", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected duplicate completion under the same epoch to be a no-op")
	}
	run, err := pg.GetRun(runID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Output != "first" {
		t.Fatalf("run.Output = %q, want unchanged %q", run.Output, "first")
	}
}

func TestPG_CompleteJob_UnderSupersededEpochRejected(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "proj-superseded", "")
	runID := mustCreateRun(t, pg, "proj-superseded", nil)
	jobID := mustCreateRemoteJob(t, pg, runID, "build")
	job, _ := pg.ClaimNextJob(mustCreateRunner(t, pg, "r-superseded"))
	// Reclaim bumps the epoch (simulating 4.2's silent-runner recovery).
	if _, _, err := pg.ReclaimStaleJobs(time.Now().Add(time.Hour), 100, "reclaimed"); err != nil {
		t.Fatal(err)
	}
	// The original, merely-slow runner's completion under its old epoch
	// must be rejected.
	ok, err := pg.CompleteJob(jobID, job.ClaimEpoch, domain.RemoteJobStatusPassed, "late", "", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected completion under a superseded epoch to be rejected")
	}
}

func TestPG_CompleteJob_RunAlreadyTerminal_StoresOutputOnlyByFailInterruptedRuns(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "proj-interrupted", "")
	runID := mustCreateRun(t, pg, "proj-interrupted", nil)
	jobID := mustCreateRemoteJob(t, pg, runID, "build")
	job, _ := pg.ClaimNextJob(mustCreateRunner(t, pg, "r-interrupted"))

	// FailInterruptedRuns finalizes the run without any idea remote_jobs
	// exists, so the job's own epoch stays current.
	if _, err := pg.FailInterruptedRuns("interrupted by server restart"); err != nil {
		t.Fatal(err)
	}
	run, err := pg.GetRun(runID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "failed" {
		t.Fatalf("precondition: run.Status = %q, want failed", run.Status)
	}
	priorOutput := run.Output

	ok, err := pg.CompleteJob(jobID, job.ClaimEpoch, domain.RemoteJobStatusPassed, "late but real output", "", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected ok=false for a completion against an already-terminal run")
	}
	run, err = pg.GetRun(runID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "failed" {
		t.Fatalf("run.Status = %q, must stay failed, not be revived", run.Status)
	}
	if run.Output != priorOutput+"late but real output" {
		t.Fatalf("run.Output = %q, want prior output with the trailing chunk appended", run.Output)
	}
	after, err := pg.GetRemoteJob(jobID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != domain.RemoteJobStatusPassed {
		t.Fatalf("job status = %q, want passed (finalized so it stops being reclaimed)", after.Status)
	}
}

func TestPG_CompleteJob_RunInterleavedTerminalRejected(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "proj-interleave", "")
	runID := mustCreateRun(t, pg, "proj-interleave", nil)
	jobAID := mustCreateRemoteJob(t, pg, runID, "a")
	jobBID := mustCreateRemoteJob(t, pg, runID, "b")
	jobA, _ := pg.ClaimNextJob(mustCreateRunner(t, pg, "r-interleave-a"))
	jobB, _ := pg.ClaimNextJob(mustCreateRunner(t, pg, "r-interleave-b"))
	if jobA.ID != jobAID || jobB.ID != jobBID {
		t.Fatalf("unexpected claims: %+v %+v", jobA, jobB)
	}

	// A sibling job fails. b is still running (its claim recorded it so,
	// remote-runner-fixes D5), so the run stays running like an in-process
	// run would...
	if ok, err := pg.CompleteJob(jobAID, jobA.ClaimEpoch, domain.RemoteJobStatusFailed, "a failed", "", nil, 0); err != nil || !ok {
		t.Fatalf("CompleteJob(a) = (%v, %v)", ok, err)
	}
	run, err := pg.GetRun(runID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "running" {
		t.Fatalf("run.Status = %q, want running while b still executes", run.Status)
	}
	// ...until something else finalizes it (e.g. FailInterruptedRuns).
	if _, err := pg.db.Exec(`UPDATE runs SET status = 'failed' WHERE id = $1`, runID); err != nil {
		t.Fatal(err)
	}

	// Job b's completion now arrives against an already-terminal run.
	ok, err := pg.CompleteJob(jobBID, jobB.ClaimEpoch, domain.RemoteJobStatusPassed, "b output", "", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected job b's completion to be rejected once the run is already terminal")
	}
}

func TestPG_CompleteJob_UnknownJob(t *testing.T) {
	pg := testPG(t)
	ok, err := pg.CompleteJob(999999, 1, domain.RemoteJobStatusPassed, "x", "", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected ok=false for unknown job")
	}
}

// ── RemoteJobStore: cancel / reclaim / outstanding ────────────────

func TestPG_CancelJobsForRun_CancelsPendingLeavesClaimedAlone(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "proj-cancel-jobs", "")
	runID := mustCreateRun(t, pg, "proj-cancel-jobs", nil)
	firstID := mustCreateRemoteJob(t, pg, runID, "a")
	secondID := mustCreateRemoteJob(t, pg, runID, "b")
	if _, err := pg.ClaimNextJob(mustCreateRunner(t, pg, "r-cancel-jobs")); err != nil {
		t.Fatal(err)
	}
	// CreateRemoteJob's insertion order determines which of the two got
	// claimed above (the oldest pending); derive it rather than assume it.
	first, err := pg.GetRemoteJob(firstID)
	if err != nil {
		t.Fatal(err)
	}
	claimedID, stillPendingID := firstID, secondID
	if first.Status != domain.RemoteJobStatusClaimed {
		claimedID, stillPendingID = secondID, firstID
	}

	if err := pg.CancelJobsForRun(runID); err != nil {
		t.Fatal(err)
	}
	pending, err := pg.GetRemoteJob(stillPendingID)
	if err != nil {
		t.Fatal(err)
	}
	if pending.Status != domain.RemoteJobStatusCanceled {
		t.Fatalf("pending job status = %q, want canceled", pending.Status)
	}
	stillClaimed, err := pg.GetRemoteJob(claimedID)
	if err != nil {
		t.Fatal(err)
	}
	if stillClaimed.Status != domain.RemoteJobStatusClaimed {
		t.Fatalf("claimed job status = %q, want left claimed (no stop signal exists)", stillClaimed.Status)
	}
}

func TestPG_PendingOrClaimedJobIDsForRun(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "proj-outstanding-ids", "")
	runID := mustCreateRun(t, pg, "proj-outstanding-ids", nil)
	aID := mustCreateRemoteJob(t, pg, runID, "a")
	bID := mustCreateRemoteJob(t, pg, runID, "b")
	jobA, _ := pg.ClaimNextJob(mustCreateRunner(t, pg, "r-outstanding-ids"))
	if _, err := pg.CompleteJob(jobA.ID, jobA.ClaimEpoch, domain.RemoteJobStatusPassed, "", "", nil, 0); err != nil {
		t.Fatal(err)
	}

	ids, err := pg.PendingOrClaimedJobIDsForRun(runID)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || (ids[0] != aID && ids[0] != bID) {
		t.Fatalf("ids = %v, want exactly the one still-outstanding job", ids)
	}
}

func TestPG_ReclaimStaleJobs_ReturnsToPendingAndBumpsEpoch(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "proj-reclaim", "")
	runID := mustCreateRun(t, pg, "proj-reclaim", nil)
	jobID := mustCreateRemoteJob(t, pg, runID, "build")
	job, _ := pg.ClaimNextJob(mustCreateRunner(t, pg, "r-reclaim-1"))

	reclaimed, failed, err := pg.ReclaimStaleJobs(time.Now().Add(time.Hour), 100, "runner went silent")
	if err != nil {
		t.Fatal(err)
	}
	if reclaimed != 1 || failed != 0 {
		t.Fatalf("reclaimed=%d failed=%d, want 1,0", reclaimed, failed)
	}
	after, err := pg.GetRemoteJob(jobID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != domain.RemoteJobStatusPending {
		t.Fatalf("status = %q, want pending", after.Status)
	}
	if after.ClaimEpoch != job.ClaimEpoch+1 {
		t.Fatalf("ClaimEpoch = %d, want %d", after.ClaimEpoch, job.ClaimEpoch+1)
	}

	// The old, merely-slow runner's trace/complete calls under the old
	// epoch are now rejected.
	if ok, _ := pg.AppendJobTrace(jobID, job.ClaimEpoch, 0, "late", 0, nil); ok {
		t.Fatal("expected old-epoch trace append to be rejected after reclaim")
	}
	if ok, _ := pg.CompleteJob(jobID, job.ClaimEpoch, domain.RemoteJobStatusPassed, "late", "", nil, 0); ok {
		t.Fatal("expected old-epoch completion to be rejected after reclaim")
	}

	// A new runner can now claim it under the bumped epoch.
	reclaimedJob, err := pg.ClaimNextJob(mustCreateRunner(t, pg, "r-reclaim-2"))
	if err != nil {
		t.Fatal(err)
	}
	if reclaimedJob == nil || reclaimedJob.ID != jobID {
		t.Fatalf("expected the reclaimed job to be claimable again, got %+v", reclaimedJob)
	}
}

func TestPG_ReclaimStaleJobs_MaxReclaimsFails(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "proj-max-reclaim", "")
	runID := mustCreateRun(t, pg, "proj-max-reclaim", nil)
	jobID := mustCreateRemoteJob(t, pg, runID, "build")
	pg.ClaimNextJob(mustCreateRunner(t, pg, "r-max-reclaim"))

	cutoff := time.Now().Add(time.Hour)
	// maxReclaims=1 allows exactly one reclaim (remote-runner-fixes D3):
	// the first silence returns the job to pending...
	if reclaimed, failed, err := pg.ReclaimStaleJobs(cutoff, 1, "silent runner, giving up"); err != nil || reclaimed != 1 || failed != 0 {
		t.Fatalf("first sweep: reclaimed=%d failed=%d err=%v, want 1,0", reclaimed, failed, err)
	}
	// ...and once a second claim goes silent too, it is finalized failed.
	pg.ClaimNextJob(mustCreateRunner(t, pg, "r-max-reclaim-2"))
	reclaimed, failed, err := pg.ReclaimStaleJobs(cutoff, 1, "silent runner, giving up")
	if err != nil {
		t.Fatal(err)
	}
	if reclaimed != 0 || failed != 1 {
		t.Fatalf("reclaimed=%d failed=%d, want 0,1", reclaimed, failed)
	}
	after, err := pg.GetRemoteJob(jobID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != domain.RemoteJobStatusFailed {
		t.Fatalf("status = %q, want failed", after.Status)
	}
	run, err := pg.GetRun(runID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "failed" {
		t.Fatalf("run.Status = %q, want failed", run.Status)
	}
}

func TestPG_ReclaimStaleJobs_TerminalRunJobNeverReclaimedOrOffered(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "proj-terminal-reclaim", "")
	runID := mustCreateRun(t, pg, "proj-terminal-reclaim", nil)
	jobAID := mustCreateRemoteJob(t, pg, runID, "a")
	jobBID := mustCreateRemoteJob(t, pg, runID, "b")
	jobA, _ := pg.ClaimNextJob(mustCreateRunner(t, pg, "r-terminal-reclaim"))
	// Job b stays pending. Fail the run out from under both via job a.
	if ok, err := pg.CompleteJob(jobAID, jobA.ClaimEpoch, domain.RemoteJobStatusFailed, "a failed", "", nil, 0); err != nil || !ok {
		t.Fatalf("CompleteJob(a) = (%v, %v)", ok, err)
	}

	// A still-pending job of a now-failed run must never be handed to a
	// poller.
	claimed, err := pg.ClaimNextJob(mustCreateRunner(t, pg, "r-terminal-reclaim-2"))
	if err != nil {
		t.Fatal(err)
	}
	if claimed != nil {
		t.Fatalf("expected ClaimNextJob to skip a pending job of a terminal run, got %+v", claimed)
	}

	// A stale *claimed* job of a terminal run is finalized in place by
	// ReclaimStaleJobs rather than returned to "pending" - simulate job b
	// having been claimed and then gone stale, on the same now-failed run.
	if _, err := pg.db.Exec(`UPDATE remote_jobs SET status = $1, claim_epoch = 1 WHERE id = $2`, domain.RemoteJobStatusClaimed, jobBID); err != nil {
		t.Fatal(err)
	}
	reclaimed, failed, err := pg.ReclaimStaleJobs(time.Now().Add(time.Hour), 100, "runner went silent")
	if err != nil {
		t.Fatal(err)
	}
	if reclaimed != 0 || failed != 1 {
		t.Fatalf("reclaimed=%d failed=%d, want 0,1 (job b's run is already terminal)", reclaimed, failed)
	}
	jobB, err := pg.GetRemoteJob(jobBID)
	if err != nil {
		t.Fatal(err)
	}
	if jobB.Status != domain.RemoteJobStatusFailed {
		t.Fatalf("job b status = %q, want failed, not returned to pending", jobB.Status)
	}
}

// ── OutstandingJobRunIDsForProject ─────────────────────────────────

func TestPG_OutstandingJobRunIDsForProject(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "proj-has-outstanding", "")
	ids, err := pg.OutstandingJobRunIDsForProject("proj-has-outstanding")
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 0 {
		t.Fatalf("expected no outstanding jobs before any run, got %v", ids)
	}

	runID := mustCreateRun(t, pg, "proj-has-outstanding", nil)
	mustCreateRemoteJob(t, pg, runID, "build")

	ids, err = pg.OutstandingJobRunIDsForProject("proj-has-outstanding")
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != runID {
		t.Fatalf("ids = %v, want [%d]", ids, runID)
	}

	job, _ := pg.ClaimNextJob(mustCreateRunner(t, pg, "r-has-outstanding"))
	if _, err := pg.CompleteJob(job.ID, job.ClaimEpoch, domain.RemoteJobStatusPassed, "", "", nil, 0); err != nil {
		t.Fatal(err)
	}
	ids, err = pg.OutstandingJobRunIDsForProject("proj-has-outstanding")
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 0 {
		t.Fatalf("expected no outstanding jobs once the only one completed, got %v", ids)
	}
}

func TestPG_OutstandingJobRunIDsForProject_UnknownProject(t *testing.T) {
	pg := testPG(t)
	ids, err := pg.OutstandingJobRunIDsForProject("does-not-exist")
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 0 {
		t.Fatalf("expected no ids for an unknown project, got %v", ids)
	}
}

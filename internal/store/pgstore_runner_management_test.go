package store

import (
	"reflect"
	"testing"

	"kestrix/internal/domain"
)

// Integration tests for remote-runner-management: runner tags, liveness,
// listing, and tag-matched claiming.

func mustCreateTaggedRunner(t *testing.T, pg *PGStore, name string, tags ...string) (int, string) {
	t.Helper()
	id, token, err := pg.CreateRunner(name, tags)
	if err != nil {
		t.Fatalf("CreateRunner(%q, %q): %v", name, tags, err)
	}
	return id, token
}

func mustCreateTaggedJob(t *testing.T, pg *PGStore, runID int, taskName string, tags ...string) int {
	t.Helper()
	id, err := pg.CreateRemoteJob(runID, taskName, domain.RemoteJobPayload{
		Task: domain.Task{Name: taskName, RunnerTags: tags, Execute: domain.ExecuteSpec{Repo: "checkout", Script: "true"}},
	})
	if err != nil {
		t.Fatalf("CreateRemoteJob: %v", err)
	}
	return id
}

func findRunner(t *testing.T, pg *PGStore, id int) domain.Runner {
	t.Helper()
	runners, err := pg.ListRunners()
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range runners {
		if r.ID == id {
			return r
		}
	}
	t.Fatalf("runner %d not listed", id)
	return domain.Runner{}
}

func TestPG_Runner_UntaggedRowsReadBackEmpty(t *testing.T) {
	pg := testPG(t)
	var runnerID int
	if err := pg.db.QueryRow(`INSERT INTO runners (name, token_hash) VALUES ('legacy', 'legacy-hash-mgmt') RETURNING id`).Scan(&runnerID); err != nil {
		t.Fatal(err)
	}
	if r := findRunner(t, pg, runnerID); len(r.Tags) != 0 || r.LastSeenAt != nil || r.Busy {
		t.Fatalf("legacy runner = %+v, want no tags, never seen, not busy", r)
	}
	var tags string
	mustUpsert(t, pg, "proj-legacy-job-tags", "")
	runID := mustCreateRun(t, pg, "proj-legacy-job-tags", nil)
	jobID := mustCreateRemoteJob(t, pg, runID, "build")
	if err := pg.db.QueryRow(`SELECT array_to_json(required_tags)::text FROM remote_jobs WHERE id = $1`, jobID).Scan(&tags); err != nil {
		t.Fatal(err)
	}
	if tags != "[]" {
		t.Fatalf("untagged job required_tags = %q, want empty", tags)
	}
}

func TestPG_Runner_TagsPersistedAndLiveness(t *testing.T) {
	pg := testPG(t)
	id, token := mustCreateTaggedRunner(t, pg, "gpu-box", "gpu", "linux")
	r := findRunner(t, pg, id)
	if !reflect.DeepEqual(r.Tags, []string{"gpu", "linux"}) || r.LastSeenAt != nil {
		t.Fatalf("new runner = %+v, want tags [gpu linux] and never seen", r)
	}
	if _, ok, err := pg.AuthenticateRunner(token); err != nil || !ok {
		t.Fatalf("AuthenticateRunner = %v, %v", ok, err)
	}
	if r := findRunner(t, pg, id); r.LastSeenAt == nil {
		t.Fatal("a valid token's request did not record last_seen_at")
	}
}

func TestPG_Runner_UnknownOrRevokedTokenRecordsNothing(t *testing.T) {
	pg := testPG(t)
	id, token := mustCreateTaggedRunner(t, pg, "revoked-box")
	if _, ok, _ := pg.AuthenticateRunner("not-a-real-token"); ok {
		t.Fatal("unknown token authenticated")
	}
	if err := pg.RevokeRunner(id); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := pg.AuthenticateRunner(token); ok {
		t.Fatal("revoked token authenticated")
	}
	var lastSeen *string
	if err := pg.db.QueryRow(`SELECT last_seen_at::text FROM runners WHERE id = $1`, id).Scan(&lastSeen); err != nil {
		t.Fatal(err)
	}
	if lastSeen != nil {
		t.Fatalf("revoked runner last_seen_at = %s, want untouched", *lastSeen)
	}
	for _, r := range mustListRunners(t, pg) {
		if r.ID == id {
			t.Fatal("revoked runner is still listed")
		}
	}
}

func mustListRunners(t *testing.T, pg *PGStore) []domain.Runner {
	t.Helper()
	runners, err := pg.ListRunners()
	if err != nil {
		t.Fatal(err)
	}
	return runners
}

func TestPG_Runner_BusyWhileHoldingAJob(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "proj-runner-busy", "")
	runID := mustCreateRun(t, pg, "proj-runner-busy", nil)
	mustCreateRemoteJob(t, pg, runID, "build")
	id, _ := mustCreateTaggedRunner(t, pg, "busy-box")
	if job, err := pg.ClaimNextJob(id); err != nil || job == nil {
		t.Fatalf("ClaimNextJob = %+v, %v", job, err)
	}
	if r := findRunner(t, pg, id); !r.Busy {
		t.Fatal("runner holding a claimed job is not busy")
	}
}

func TestPG_ClaimNextJob_TagMatching(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "proj-tag-matching", "")
	runID := mustCreateRun(t, pg, "proj-tag-matching", nil)
	gpuJob := mustCreateTaggedJob(t, pg, runID, "train", "gpu")
	plainJob := mustCreateTaggedJob(t, pg, runID, "lint")
	plain, _ := mustCreateTaggedRunner(t, pg, "plain-box")
	gpu, _ := mustCreateTaggedRunner(t, pg, "gpu-box-2", "gpu", "linux")

	// The oldest pending job needs gpu; an untagged runner skips it and
	// gets the newer untagged job instead of nothing.
	job, err := pg.ClaimNextJob(plain)
	if err != nil || job == nil || job.ID != plainJob {
		t.Fatalf("untagged runner claimed %+v, %v; want the untagged job %d", job, err, plainJob)
	}
	if job, _ := pg.ClaimNextJob(plain); job != nil {
		t.Fatalf("untagged runner claimed %d, want the gpu job withheld", job.ID)
	}
	// A superset runner gets the tagged job.
	if job, err := pg.ClaimNextJob(gpu); err != nil || job == nil || job.ID != gpuJob {
		t.Fatalf("gpu runner claimed %+v, %v; want %d", job, err, gpuJob)
	}
}

func TestPG_ClaimNextJob_TaggedRunnerTakesUntaggedJob(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "proj-tagged-runner-untagged-job", "")
	runID := mustCreateRun(t, pg, "proj-tagged-runner-untagged-job", nil)
	jobID := mustCreateTaggedJob(t, pg, runID, "anywhere")
	deploy, _ := mustCreateTaggedRunner(t, pg, "deploy-box", "deploy")
	if job, err := pg.ClaimNextJob(deploy); err != nil || job == nil || job.ID != jobID {
		t.Fatalf("tagged runner claimed %+v, %v; want untagged job %d", job, err, jobID)
	}
}

// 3.2: the task's tags are snapshotted onto the job at dispatch; editing
// the pipeline afterwards cannot retarget a pending job.
func TestPG_CreateRemoteJob_SnapshotsRequiredTags(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "proj-tag-snapshot", "")
	runID := mustCreateRun(t, pg, "proj-tag-snapshot", nil)
	jobID := mustCreateTaggedJob(t, pg, runID, "train", "gpu", "linux")
	var tags string
	if err := pg.db.QueryRow(`SELECT array_to_json(required_tags)::text FROM remote_jobs WHERE id = $1`, jobID).Scan(&tags); err != nil {
		t.Fatal(err)
	}
	if tags != `["gpu","linux"]` {
		t.Fatalf("required_tags = %s, want [\"gpu\",\"linux\"]", tags)
	}
}

func TestPG_UnmatchedPendingJobs(t *testing.T) {
	pg := testPG(t)

	// Clean slate for remote_jobs and runners
	if _, err := pg.db.Exec(`DELETE FROM remote_jobs; DELETE FROM runners`); err != nil {
		t.Fatal(err)
	}

	mustUpsert(t, pg, "proj-unmatched", "")
	run1 := mustCreateRun(t, pg, "proj-unmatched", nil)
	run2 := mustCreateRun(t, pg, "proj-unmatched", nil)

	// Case 4a: Untagged job with zero runners is unmatched
	untaggedJob := mustCreateTaggedJob(t, pg, run1, "untagged")
	unmatched, err := pg.UnmatchedPendingJobs(nil)
	if err != nil {
		t.Fatalf("UnmatchedPendingJobs: %v", err)
	}
	if len(unmatched) != 1 || unmatched[0].ID != untaggedJob || len(unmatched[0].RequiredTags) != 0 {
		t.Fatalf("untagged with 0 runners = %+v, want job %d", unmatched, untaggedJob)
	}

	// Case 4b: Untagged job with one runner is matched (not unmatched)
	r1, _ := mustCreateTaggedRunner(t, pg, "runner-1", "general")
	unmatched, err = pg.UnmatchedPendingJobs(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(unmatched) != 0 {
		t.Fatalf("untagged with 1 runner = %+v, want none", unmatched)
	}

	// Case 1: Tagged job with no matching runner
	gpuJob := mustCreateTaggedJob(t, pg, run1, "gpu-task", "gpu")
	unmatched, err = pg.UnmatchedPendingJobs(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(unmatched) != 1 || unmatched[0].ID != gpuJob {
		t.Fatalf("tagged job with no matching runner = %+v, want job %d", unmatched, gpuJob)
	}
	if len(unmatched[0].RequiredTags) != 1 || unmatched[0].RequiredTags[0] != "gpu" {
		t.Fatalf("tags = %v, want [gpu]", unmatched[0].RequiredTags)
	}

	// Filter by runID
	unmatchedRun1, err := pg.UnmatchedPendingJobs(&run1)
	if err != nil {
		t.Fatal(err)
	}
	if len(unmatchedRun1) != 1 || unmatchedRun1[0].ID != gpuJob {
		t.Fatalf("filtered run1 = %+v, want job %d", unmatchedRun1, gpuJob)
	}
	unmatchedRun2, err := pg.UnmatchedPendingJobs(&run2)
	if err != nil {
		t.Fatal(err)
	}
	if len(unmatchedRun2) != 0 {
		t.Fatalf("filtered run2 = %+v, want none", unmatchedRun2)
	}

	// Case 2: Matched by a superset runner
	gpuSuperset, _ := mustCreateTaggedRunner(t, pg, "runner-gpu", "gpu", "linux", "general")
	unmatched, err = pg.UnmatchedPendingJobs(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(unmatched) != 0 {
		t.Fatalf("matched by superset runner = %+v, want none", unmatched)
	}

	// Case 3: Matching runner revoked -> unmatched again
	if err := pg.RevokeRunner(gpuSuperset); err != nil {
		t.Fatal(err)
	}
	unmatched, err = pg.UnmatchedPendingJobs(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(unmatched) != 1 || unmatched[0].ID != gpuJob {
		t.Fatalf("revoked runner = %+v, want job %d", unmatched, gpuJob)
	}

	// Case 5: Claimed jobs excluded
	// First let r1 claim the untagged job so gpuJob is the next job
	if _, err := pg.ClaimNextJob(r1); err != nil {
		t.Fatal(err)
	}
	// Re-register a matching runner and claim the gpu job
	gpuRunner2, _ := mustCreateTaggedRunner(t, pg, "runner-gpu-2", "gpu")
	claimed, err := pg.ClaimNextJob(gpuRunner2)
	if err != nil || claimed == nil || claimed.ID != gpuJob {
		t.Fatalf("claim failed: %v, job: %+v, want job %d", err, claimed, gpuJob)
	}
	// Revoke the runner again: even with no runner for gpu, since gpuJob is claimed (not pending), it's excluded
	if err := pg.RevokeRunner(gpuRunner2); err != nil {
		t.Fatal(err)
	}
	unmatched, err = pg.UnmatchedPendingJobs(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(unmatched) != 0 {
		t.Fatalf("claimed job = %+v, want none (excluded from unmatched pending jobs)", unmatched)
	}
}

package store

import (
	"errors"
	"strings"
	"testing"
	"time"

	"kestrix/internal/credentials"
	"kestrix/internal/domain"
)

// Integration tests for remote-runner-fixes: secrets at rest (D2),
// reclaim counting (D3), the task error (D4), and running-on-claim (D5).

const fixSecret = "S3cr3t-Remote-Value-9731"

func mustCreateSecretJob(t *testing.T, pg *PGStore, runID int, taskName string) int {
	t.Helper()
	id, err := pg.CreateRemoteJob(runID, taskName, domain.RemoteJobPayload{
		Task:         domain.Task{Name: taskName, Execute: domain.ExecuteSpec{Repo: "checkout", Script: "echo $TOKEN"}},
		Variables:    map[string]string{"TOKEN": fixSecret, "PLAIN": "visible"},
		SecretValues: []string{fixSecret},
	})
	if err != nil {
		t.Fatalf("CreateRemoteJob: %v", err)
	}
	return id
}

// plaintextColumns counts rows of remote_jobs and runs whose any stored
// column contains fixSecret as plain text.
func plaintextColumns(t *testing.T, pg *PGStore) int {
	t.Helper()
	var n int
	err := pg.db.QueryRow(`SELECT
		(SELECT count(*) FROM remote_jobs WHERE payload::text LIKE '%' || $1 || '%' OR convert_from(coalesce(secrets_enc, ''), 'UTF8') LIKE '%' || $1 || '%') +
		(SELECT count(*) FROM runs WHERE coalesce(remote_snapshot::text, '') LIKE '%' || $1 || '%' OR convert_from(coalesce(remote_secrets_enc, ''), 'UTF8') LIKE '%' || $1 || '%'
		                              OR output LIKE '%' || $1 || '%' OR task_results::text LIKE '%' || $1 || '%')`, fixSecret).Scan(&n)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestPG_RemoteSecrets_EncryptedAtRestAndRoundTrip(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "proj-secrets-at-rest", "")
	runID := mustCreateRun(t, pg, "proj-secrets-at-rest", nil)
	if err := pg.SetRemoteRunSnapshot(runID, []byte("archive"), map[string]map[string]string{"build": {"TOKEN": fixSecret}}, []string{fixSecret}); err != nil {
		t.Fatal(err)
	}
	jobID := mustCreateSecretJob(t, pg, runID, "build")

	if n := plaintextColumns(t, pg); n != 0 {
		t.Fatalf("%d rows hold the secret in plaintext, want 0", n)
	}

	values, err := pg.RemoteJobSecretValues(jobID)
	if err != nil || len(values) != 1 || values[0] != fixSecret {
		t.Fatalf("RemoteJobSecretValues = %q, %v", values, err)
	}
	job, err := pg.ClaimNextJob(mustCreateRunner(t, pg, "r-secrets"))
	if err != nil || job == nil {
		t.Fatalf("ClaimNextJob = %+v, %v", job, err)
	}
	if job.Payload.Variables["TOKEN"] != fixSecret || job.Payload.Variables["PLAIN"] != "visible" || len(job.Payload.SecretValues) != 1 {
		t.Fatalf("claimed payload = %+v, want variables and secret values restored", job.Payload)
	}
	archive, vars, secrets, err := pg.GetRemoteRunSnapshot(runID)
	if err != nil || string(archive) != "archive" || vars["build"]["TOKEN"] != fixSecret || len(secrets) != 1 {
		t.Fatalf("GetRemoteRunSnapshot = %q, %v, %q, %v", archive, vars, secrets, err)
	}
}

func TestPG_RemoteSecrets_DeletedOnceTerminal(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "proj-secrets-deleted", "")
	runID := mustCreateRun(t, pg, "proj-secrets-deleted", nil)
	if err := pg.SetRemoteRunSnapshot(runID, nil, nil, []string{fixSecret}); err != nil {
		t.Fatal(err)
	}
	jobID := mustCreateSecretJob(t, pg, runID, "build")
	job, _ := pg.ClaimNextJob(mustCreateRunner(t, pg, "r-secrets-deleted"))
	if ok, err := pg.CompleteJob(jobID, job.ClaimEpoch, domain.RemoteJobStatusPassed, "done", "", nil, 0); err != nil || !ok {
		t.Fatalf("CompleteJob = %v, %v", ok, err)
	}
	if err := pg.DeleteTerminalRemoteSecrets(); err != nil {
		t.Fatal(err)
	}
	var jobEnc, runEnc []byte
	if err := pg.db.QueryRow(`SELECT secrets_enc FROM remote_jobs WHERE id = $1`, jobID).Scan(&jobEnc); err != nil {
		t.Fatal(err)
	}
	if err := pg.db.QueryRow(`SELECT remote_secrets_enc FROM runs WHERE id = $1`, runID).Scan(&runEnc); err != nil {
		t.Fatal(err)
	}
	if jobEnc != nil || runEnc != nil {
		t.Fatalf("secrets_enc=%d bytes remote_secrets_enc=%d bytes, want both deleted", len(jobEnc), len(runEnc))
	}
}

func TestPG_RemoteSecrets_CiphertextBoundToItsJob(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "proj-secrets-aad", "")
	runID := mustCreateRun(t, pg, "proj-secrets-aad", nil)
	aID := mustCreateSecretJob(t, pg, runID, "a")
	bID := mustCreateRemoteJob(t, pg, runID, "b")
	if _, err := pg.db.Exec(`UPDATE remote_jobs SET secrets_enc = (SELECT secrets_enc FROM remote_jobs WHERE id = $1) WHERE id = $2`, aID, bID); err != nil {
		t.Fatal(err)
	}
	if _, err := pg.RemoteJobSecretValues(bID); err == nil {
		t.Fatal("expected a ciphertext copied from another job to fail to decrypt")
	}
}

// A single-task re-run creates a second job for the same run and task once
// the first is terminal; its ciphertext must not open the other's.
func TestPG_RemoteSecrets_CiphertextBoundToJobNotTask(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "proj-secrets-aad-same-task", "")
	runID := mustCreateRun(t, pg, "proj-secrets-aad-same-task", nil)
	firstID := mustCreateSecretJob(t, pg, runID, "build")
	if _, err := pg.db.Exec(`UPDATE remote_jobs SET status = 'failed' WHERE id = $1`, firstID); err != nil {
		t.Fatal(err)
	}
	secondID := mustCreateSecretJob(t, pg, runID, "build")
	if _, err := pg.db.Exec(`UPDATE remote_jobs SET secrets_enc = (SELECT secrets_enc FROM remote_jobs WHERE id = $1) WHERE id = $2`, firstID, secondID); err != nil {
		t.Fatal(err)
	}
	if _, err := pg.RemoteJobSecretValues(secondID); err == nil {
		t.Fatal("expected the same task's earlier job ciphertext to fail to decrypt on the re-run's job")
	}
}

func TestPG_RemoteSecrets_NoMasterKeyFailsClosed(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "proj-secrets-nokey", "")
	runID := mustCreateRun(t, pg, "proj-secrets-nokey", nil)
	noKey := *pg
	noKey.masterKeys = nil
	if _, err := noKey.CreateRemoteJob(runID, "build", domain.RemoteJobPayload{}); !errors.Is(err, credentials.ErrMasterKeyUnavailable) {
		t.Fatalf("CreateRemoteJob err = %v, want ErrMasterKeyUnavailable", err)
	}
	if err := noKey.SetRemoteRunSnapshot(runID, nil, nil, nil); !errors.Is(err, credentials.ErrMasterKeyUnavailable) {
		t.Fatalf("SetRemoteRunSnapshot err = %v, want ErrMasterKeyUnavailable", err)
	}
}

func TestPG_Migrate_StripsLegacyPlaintextVariables(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "proj-legacy-plaintext", "")
	runID := mustCreateRun(t, pg, "proj-legacy-plaintext", nil)
	if _, err := pg.db.Exec(`INSERT INTO remote_jobs (run_id, task_name, status, payload) VALUES ($1, 'build', 'failed', $2)`,
		runID, `{"task":{"name":"build"},"variables":{"TOKEN":"`+fixSecret+`"}}`); err != nil {
		t.Fatal(err)
	}
	if _, err := pg.db.Exec(`UPDATE runs SET remote_snapshot = $1 WHERE id = $2`, `{"archive":null,"variables":{"build":{"TOKEN":"`+fixSecret+`"}}}`, runID); err != nil {
		t.Fatal(err)
	}
	if n := plaintextColumns(t, pg); n != 2 {
		t.Fatalf("plaintextColumns = %d before migrate, want 2 (the check must see legacy plaintext)", n)
	}
	if err := migrate(pg.db, "local"); err != nil {
		t.Fatal(err)
	}
	if n := plaintextColumns(t, pg); n != 0 {
		t.Fatalf("%d rows still hold the legacy plaintext secret after migrate", n)
	}
}

// D3: with every runner gone, a reclaimed job keeps counting while it sits
// unclaimed, and fails at the limit instead of staying pending forever.
func TestPG_ReclaimStaleJobs_NoRunnerLeftFailsAtLimit(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "proj-no-runner-left", "")
	runID := mustCreateRun(t, pg, "proj-no-runner-left", nil)
	jobID := mustCreateRemoteJob(t, pg, runID, "build")
	pg.ClaimNextJob(mustCreateRunner(t, pg, "r-no-runner-left"))
	cutoff := time.Now().Add(time.Hour)

	for i, want := range []string{domain.RemoteJobStatusPending, domain.RemoteJobStatusPending, domain.RemoteJobStatusFailed} {
		if _, _, err := pg.ReclaimStaleJobs(cutoff, 2, "abandoned by unresponsive runners"); err != nil {
			t.Fatal(err)
		}
		job, _ := pg.GetRemoteJob(jobID)
		if job.Status != want {
			t.Fatalf("after sweep %d: status = %q, want %q", i+1, job.Status, want)
		}
	}
	run, _ := pg.GetRun(runID)
	if run.Status != "failed" {
		t.Fatalf("run.Status = %q, want failed", run.Status)
	}
	last := run.TaskResults[len(run.TaskResults)-1]
	if last.Status != "failed" || !strings.Contains(last.Error, "unresponsive") {
		t.Fatalf("last task result = %+v, want failed naming the reason", last)
	}
}

func TestPG_ReclaimStaleJobs_NeverClaimedPendingLeftAlone(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "proj-never-claimed", "")
	runID := mustCreateRun(t, pg, "proj-never-claimed", nil)
	jobID := mustCreateRemoteJob(t, pg, runID, "build")
	reclaimed, failed, err := pg.ReclaimStaleJobs(time.Now().Add(time.Hour), 0, "silent")
	if err != nil || reclaimed != 0 || failed != 0 {
		t.Fatalf("reclaimed=%d failed=%d err=%v, want the never-claimed job untouched", reclaimed, failed, err)
	}
	if job, _ := pg.GetRemoteJob(jobID); job.Status != domain.RemoteJobStatusPending {
		t.Fatalf("status = %q, want pending", job.Status)
	}
}

// D5: claiming records the task running; a reclaim puts it back to pending.
func TestPG_ClaimNextJob_RecordsTaskRunning(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "proj-claim-running", "")
	runID := mustCreateRun(t, pg, "proj-claim-running", nil)
	mustCreateRemoteJob(t, pg, runID, "build")
	pg.ClaimNextJob(mustCreateRunner(t, pg, "r-claim-running"))
	latest := func() string {
		run, _ := pg.GetRun(runID)
		return run.TaskResults[len(run.TaskResults)-1].Status
	}
	if got := latest(); got != "running" {
		t.Fatalf("after claim: latest task status = %q, want running", got)
	}
	if _, _, err := pg.ReclaimStaleJobs(time.Now().Add(time.Hour), 3, "silent"); err != nil {
		t.Fatal(err)
	}
	if got := latest(); got != "pending" {
		t.Fatalf("after reclaim: latest task status = %q, want pending", got)
	}
}

// D4: a completion's error is stored on the task result.
func TestPG_CompleteJob_StoresTaskError(t *testing.T) {
	pg := testPG(t)
	mustUpsert(t, pg, "proj-task-error", "")
	runID := mustCreateRun(t, pg, "proj-task-error", nil)
	jobID := mustCreateRemoteJob(t, pg, runID, "build")
	job, _ := pg.ClaimNextJob(mustCreateRunner(t, pg, "r-task-error"))
	if ok, err := pg.CompleteJob(jobID, job.ClaimEpoch, domain.RemoteJobStatusFailed, "", `exec: "bash": executable file not found in $PATH`, nil, 0); err != nil || !ok {
		t.Fatalf("CompleteJob = %v, %v", ok, err)
	}
	run, _ := pg.GetRun(runID)
	last := run.TaskResults[len(run.TaskResults)-1]
	if last.Status != "failed" || !strings.Contains(last.Error, "bash") {
		t.Fatalf("last task result = %+v, want failed with the error", last)
	}
}

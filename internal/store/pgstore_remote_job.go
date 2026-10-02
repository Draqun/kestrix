package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"kestrix/internal/credentials"
	"kestrix/internal/domain"
)

// terminalJobStatuses are the remote_jobs.status values a job never leaves.
func isTerminalJobStatus(status string) bool {
	switch status {
	case domain.RemoteJobStatusPassed, domain.RemoteJobStatusFailed, domain.RemoteJobStatusCanceled:
		return true
	}
	return false
}

func isTerminalRunStatus(status string) bool {
	switch status {
	case "passed", "failed", "canceled":
		return true
	}
	return false
}

// callerOwnsJob reports whether callerRunnerID is allowed to act on a job
// whose stored runner_id is storedRunnerID. A nil callerRunnerID is a
// system-initiated call (never HTTP-driven) and always owns the job; a
// non-nil caller must match a non-null stored runner_id exactly - a
// never-claimed job (storedRunnerID NULL) is owned by no caller.
func callerOwnsJob(callerRunnerID *int, storedRunnerID sql.NullInt64) bool {
	if callerRunnerID == nil {
		return true
	}
	return storedRunnerID.Valid && int(storedRunnerID.Int64) == *callerRunnerID
}

// remoteSecrets is what remote_jobs.secrets_enc and runs.remote_secrets_enc
// hold once decrypted: the resolved variables (per job, or per task for a
// run's snapshot) and the resolved runtime-secret values among them. Never
// stored in plaintext - remote-runner-fixes design.md D2.
type remoteSecrets struct {
	Variables     map[string]string            `json:"variables,omitempty"`
	TaskVariables map[string]map[string]string `json:"task_variables,omitempty"`
	SecretValues  []string                     `json:"secret_values,omitempty"`
}

// remoteJobAAD binds a job's ciphertext to the job's own ID, which never
// changes across reclaims and is never shared - unlike run+task, which a
// single-task re-run repeats once the earlier job is terminal - so a
// ciphertext copied onto another job's row fails to decrypt.
func remoteJobAAD(jobID int) []byte {
	return []byte(fmt.Sprintf("remote_job:%d", jobID))
}

func remoteRunAAD(runID int) []byte {
	return []byte(fmt.Sprintf("run:%d", runID))
}

// sealRemoteSecrets encrypts v with the credential master key, failing
// closed when none is configured - the same custody the credential store
// uses.
func (s *PGStore) sealRemoteSecrets(aad []byte, v remoteSecrets) ([]byte, error) {
	if s.masterKeys == nil {
		return nil, credentials.ErrMasterKeyUnavailable
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("marshal remote secrets: %w", err)
	}
	return credentials.Encrypt(s.masterKeys, aad, b)
}

// openRemoteSecrets is sealRemoteSecrets' inverse. An empty ciphertext -
// never set, or already deleted once its job/run went terminal - opens to
// the zero value.
func (s *PGStore) openRemoteSecrets(aad, enc []byte) (remoteSecrets, error) {
	var v remoteSecrets
	if len(enc) == 0 {
		return v, nil
	}
	if s.masterKeys == nil {
		return v, credentials.ErrMasterKeyUnavailable
	}
	b, err := credentials.Decrypt(s.masterKeys, aad, enc)
	if err != nil {
		return v, fmt.Errorf("decrypt remote secrets: %w", err)
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return v, fmt.Errorf("unmarshal remote secrets: %w", err)
	}
	return v, nil
}

// storedRemoteJobPayload is the shape of the remote_jobs.payload column:
// domain.RemoteJobPayload without Variables and SecretValues. Keeping it a
// separate type means a secret-bearing field can never reach the plaintext
// column by being added to the wire payload - it has to be added here on
// purpose (remote-runner-fixes design.md D2).
type storedRemoteJobPayload struct {
	Task         domain.Task   `json:"task"`
	Repos        []domain.Repo `json:"repos"`
	DefaultImage string        `json:"default_image,omitempty"`
	Archive      []byte        `json:"archive"`
}

// CreateRemoteJob dispatches one task of runID as a pending job. The
// payload's Variables and SecretValues go into secrets_enc, encrypted; the
// payload column holds only storedRemoteJobPayload (design.md D2).
func (s *PGStore) CreateRemoteJob(runID int, taskName string, payload domain.RemoteJobPayload) (int, error) {
	// Reserve the ID first: the ciphertext's AAD names it.
	var id int
	if err := s.db.QueryRow(`SELECT nextval(pg_get_serial_sequence('remote_jobs', 'id'))`).Scan(&id); err != nil {
		return 0, fmt.Errorf("reserve remote job id: %w", err)
	}
	enc, err := s.sealRemoteSecrets(remoteJobAAD(id), remoteSecrets{Variables: payload.Variables, SecretValues: payload.SecretValues})
	if err != nil {
		return 0, fmt.Errorf("encrypt remote job secrets: %w", err)
	}
	b, err := json.Marshal(storedRemoteJobPayload{Task: payload.Task, Repos: payload.Repos, DefaultImage: payload.DefaultImage, Archive: payload.Archive})
	if err != nil {
		return 0, fmt.Errorf("marshal remote job payload: %w", err)
	}
	if _, err := s.db.Exec(
		`INSERT INTO remote_jobs (id, run_id, task_name, status, payload, secrets_enc, required_tags) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		id, runID, taskName, domain.RemoteJobStatusPending, string(b), enc, nonNilTags(payload.Task.RunnerTags),
	); err != nil {
		return 0, err
	}
	return id, nil
}

// scanRemoteJob scans one remote_jobs row into a domain.RemoteJob, merging
// the decrypted variables and secret values back into its payload.
func (s *PGStore) scanRemoteJob(row interface {
	Scan(dest ...any) error
}) (*domain.RemoteJob, error) {
	var j domain.RemoteJob
	var payloadJSON string
	var runnerID sql.NullInt64
	var claimedAt sql.NullTime
	var enc []byte
	if err := row.Scan(&j.ID, &j.RunID, &j.TaskName, &j.Status, &runnerID, &payloadJSON, &j.ClaimEpoch, &claimedAt, &j.UpdatedAt, &enc); err != nil {
		return nil, err
	}
	if runnerID.Valid {
		id := int(runnerID.Int64)
		j.RunnerID = &id
	}
	if claimedAt.Valid {
		j.ClaimedAt = &claimedAt.Time
	}
	var stored storedRemoteJobPayload
	if err := json.Unmarshal([]byte(payloadJSON), &stored); err != nil {
		return nil, fmt.Errorf("unmarshal remote job payload: %w", err)
	}
	secrets, err := s.openRemoteSecrets(remoteJobAAD(j.ID), enc)
	if err != nil {
		return nil, err
	}
	j.Payload = domain.RemoteJobPayload{
		Task: stored.Task, Repos: stored.Repos, DefaultImage: stored.DefaultImage, Archive: stored.Archive,
		Variables: secrets.Variables, SecretValues: secrets.SecretValues,
	}
	return &j, nil
}

const remoteJobColumns = `id, run_id, task_name, status, runner_id, payload, claim_epoch, claimed_at, updated_at, secrets_enc`

// RemoteJobSecretValues returns the resolved runtime-secret values stored
// with jobID, without loading its payload (the source archive can be
// large and trace appends call this per chunk).
func (s *PGStore) RemoteJobSecretValues(jobID int) ([]string, error) {
	var enc []byte
	err := s.db.QueryRow(`SELECT secrets_enc FROM remote_jobs WHERE id = $1`, jobID).Scan(&enc)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	secrets, err := s.openRemoteSecrets(remoteJobAAD(jobID), enc)
	if err != nil {
		return nil, err
	}
	return secrets.SecretValues, nil
}

// GetRemoteJob returns a job by ID, or nil if it does not exist.
func (s *PGStore) GetRemoteJob(jobID int) (*domain.RemoteJob, error) {
	row := s.db.QueryRow(`SELECT `+remoteJobColumns+` FROM remote_jobs WHERE id = $1`, jobID)
	j, err := s.scanRemoteJob(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return j, nil
}

// ClaimNextJob atomically claims the oldest pending job via SELECT ... FOR
// UPDATE SKIP LOCKED - Postgres's native pattern for multiple workers
// polling one queue with no application-level lock or busy-retry loop
// (design.md D1). Returns a nil job and no error when nothing is pending.
func (s *PGStore) ClaimNextJob(runnerID int) (*domain.RemoteJob, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin claim: %w", err)
	}
	defer tx.Rollback()

	// A pending job whose run already went terminal (canceled, or failed by
	// a sibling job/FailInterruptedRuns) is never handed to a poller - see
	// tasks.md 4.3 and design.md D9's cancellation carve-out. It stays
	// "pending" rather than being claimed and immediately fenced.
	var jobID int
	err = tx.QueryRow(
		`SELECT rj.id FROM remote_jobs rj JOIN runs r ON r.id = rj.run_id
		 WHERE rj.status = $1 AND r.status NOT IN ('passed', 'failed', 'canceled')
		   AND rj.required_tags <@ (SELECT tags FROM runners WHERE id = $2)
		 ORDER BY rj.id FOR UPDATE OF rj SKIP LOCKED LIMIT 1`,
		domain.RemoteJobStatusPending, runnerID,
	).Scan(&jobID)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("select next pending job: %w", err)
	}

	// A claim's runner traces from offset 0 with a fresh traceWriter, so a
	// re-claimed job drops the superseded claim's trace position and output
	// with it: the task's result comes only from the claim that completes.
	row := tx.QueryRow(
		`UPDATE remote_jobs SET status = $1, runner_id = $2, claim_epoch = claim_epoch + 1, claimed_at = now(), updated_at = now(),
		   trace_offset = 0, output = ''
		 WHERE id = $3 RETURNING `+remoteJobColumns,
		domain.RemoteJobStatusClaimed, runnerID, jobID,
	)
	j, err := s.scanRemoteJob(row)
	if err != nil {
		return nil, fmt.Errorf("claim job %d: %w", jobID, err)
	}
	// The claim is where a remote task starts, so record it "running" the
	// way OnTaskStart does in-process (remote-runner-fixes design.md D5).
	if err := appendTaskResult(tx, j.RunID, TaskResult{Name: j.TaskName, Status: "running"}); err != nil {
		return nil, fmt.Errorf("record claimed task running: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit claim: %w", err)
	}
	return j, nil
}

// AppendJobTrace appends chunk to the job's run's output via a database-side
// append (`UPDATE runs SET output = output || $1`), never FlushRunOutput's
// whole-column overwrite - design.md D9, since two jobs of one run may
// stream concurrently. Bounded by maxOutputBytes the same way the
// in-process boundedWriter is: the chunk that crosses the bound is cut at
// it and the truncation marker appended once; a chunk arriving after the
// bound is already reached is dropped entirely. ok is false when epoch no
// longer matches the job's stored claim_epoch (D1) or the job is already
// terminal; the append is then a no-op. A first successful append on a
// "claimed" job advances it to "running", informationally. The same chunk is
// appended to the job's own output (remote_jobs.output), bounded against that
// column, so CompleteJob can put the job's whole output on the task's result.
func (s *PGStore) AppendJobTrace(jobID, epoch, offset int, chunk string, maxOutputBytes int, callerRunnerID *int) (bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return false, fmt.Errorf("begin trace append: %w", err)
	}
	defer tx.Rollback()

	var runID, claimEpoch, traceOffset int
	var jobStatus, jobOutput string
	var runnerID sql.NullInt64
	err = tx.QueryRow(`SELECT run_id, claim_epoch, status, trace_offset, runner_id, output FROM remote_jobs WHERE id = $1 FOR UPDATE`, jobID).
		Scan(&runID, &claimEpoch, &jobStatus, &traceOffset, &runnerID, &jobOutput)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("select job for trace append: %w", err)
	}
	if epoch != claimEpoch || isTerminalJobStatus(jobStatus) || !callerOwnsJob(callerRunnerID, runnerID) {
		return false, nil
	}
	if offset < traceOffset {
		// The runner's own safe retry (for example after a dropped HTTP
		// response): this content was already accepted, so report success
		// without touching anything again.
		return true, tx.Commit()
	}
	if offset > traceOffset {
		return false, fmt.Errorf("trace offset %d is ahead of expected %d for job %d (gap)", offset, traceOffset, jobID)
	}

	var currentOutput string
	if err := tx.QueryRow(`SELECT output FROM runs WHERE id = $1 FOR UPDATE`, runID).Scan(&currentOutput); err != nil {
		return false, fmt.Errorf("select run output for trace append: %w", err)
	}

	toAppend := boundAppendChunk(currentOutput, chunk, maxOutputBytes)
	if toAppend != "" {
		if _, err := tx.Exec(`UPDATE runs SET output = output || $1 WHERE id = $2`, toAppend, runID); err != nil {
			return false, fmt.Errorf("append run output: %w", err)
		}
	}

	newStatus := jobStatus
	if jobStatus == domain.RemoteJobStatusClaimed {
		newStatus = domain.RemoteJobStatusRunning
	}
	// The job's own output is bounded against itself, not the run's
	// aggregate: a sibling that used up the aggregate's budget must not
	// shorten this task's output (remote-run-ux design.md D1).
	if _, err := tx.Exec(
		`UPDATE remote_jobs SET status = $1, trace_offset = $2, output = output || $3, updated_at = now() WHERE id = $4`,
		newStatus, offset+len(chunk), boundAppendChunk(jobOutput, chunk, maxOutputBytes), jobID,
	); err != nil {
		return false, fmt.Errorf("advance trace offset: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit trace append: %w", err)
	}
	return true, nil
}

// remoteJobTruncationMarker matches the marker the in-process boundedWriter
// uses, so a client (or a human) cannot tell the two paths apart from
// stored output alone.
const remoteJobTruncationMarker = "\n[output truncated: exceeded maximum size]"

// boundAppendChunk returns the slice of chunk that should still be appended
// given currentOutput already occupies len(currentOutput) bytes of a
// maxOutputBytes budget (a non-positive budget means unbounded): the whole
// chunk while under the bound, the chunk cut at the bound plus the
// truncation marker the one time the bound is crossed, or "" once the
// bound (marker included) has already been reached - see design.md D9.
func boundAppendChunk(currentOutput, chunk string, maxOutputBytes int) string {
	if maxOutputBytes <= 0 || chunk == "" {
		return chunk
	}
	if strings.HasSuffix(currentOutput, remoteJobTruncationMarker) {
		// The marker was already appended by an earlier chunk; every later
		// chunk for this run is discarded.
		return ""
	}
	remaining := maxOutputBytes - len(currentOutput)
	if remaining <= 0 {
		return remoteJobTruncationMarker
	}
	if len(chunk) <= remaining {
		return chunk
	}
	return chunk[:remaining] + remoteJobTruncationMarker
}

// CompleteJob records a job's terminal report. See domain.RemoteJobStore's
// doc for the full fencing contract; in short: the epoch check, the job's
// own terminal check, and the parent run's terminal check all happen under
// row locks in this one transaction, so a run that goes terminal for an
// unrelated reason between the epoch check and the result write is still
// caught - the two checks (task 1.2) must not be layered in the handler.
// output is bounded by maxOutputBytes via boundAppendChunk, the same way
// AppendJobTrace already bounds every trace chunk (REVIEW.md R5) - without
// this, one completion report could append up to the runner-job request
// body's own size bound directly to runs.output, ignoring the operator-
// configured output budget entirely. The task's result carries the job's
// whole output: what AppendJobTrace accumulated, then output, bounded the same
// way.
func (s *PGStore) CompleteJob(jobID, epoch int, status, output, taskError string, callerRunnerID *int, maxOutputBytes int) (bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return false, fmt.Errorf("begin complete job: %w", err)
	}
	defer tx.Rollback()

	var runID int
	var taskName, jobStatus, jobOutput string
	var claimEpoch int
	var runnerID sql.NullInt64
	err = tx.QueryRow(`SELECT run_id, task_name, claim_epoch, status, runner_id, output FROM remote_jobs WHERE id = $1 FOR UPDATE`, jobID).
		Scan(&runID, &taskName, &claimEpoch, &jobStatus, &runnerID, &jobOutput)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("select job for completion: %w", err)
	}
	if epoch != claimEpoch || isTerminalJobStatus(jobStatus) || !callerOwnsJob(callerRunnerID, runnerID) {
		return false, nil
	}

	var runStatus, runOutput, taskResultsJSON string
	if err := tx.QueryRow(`SELECT status, output, task_results FROM runs WHERE id = $1 FOR UPDATE`, runID).
		Scan(&runStatus, &runOutput, &taskResultsJSON); err != nil {
		return false, fmt.Errorf("select run for completion: %w", err)
	}

	if isTerminalRunStatus(runStatus) {
		// The run was finalized by something with no idea remote_jobs
		// exists (FailInterruptedRuns) or by a sibling job while this one
		// was still in flight. Recording a fresh task result here would let
		// computeRunStatus revive a run that is supposed to stay terminal -
		// see design.md D9 and tasks.md 3.4. Mark this job terminal so it
		// stops being reclaimed or re-offered, and still append its
		// trailing output for visibility, but touch nothing else.
		if _, err := tx.Exec(`UPDATE remote_jobs SET status = $1, updated_at = now() WHERE id = $2`, status, jobID); err != nil {
			return false, fmt.Errorf("finalize fenced job: %w", err)
		}
		if toAppend := boundAppendChunk(runOutput, output, maxOutputBytes); toAppend != "" {
			if _, err := tx.Exec(`UPDATE runs SET output = output || $1 WHERE id = $2`, toAppend, runID); err != nil {
				return false, fmt.Errorf("append output for fenced job: %w", err)
			}
		}
		return false, tx.Commit()
	}

	var results []TaskResult
	json.Unmarshal([]byte(taskResultsJSON), &results)
	// The task's output is everything its trace carried plus the completion's
	// remainder, bounded like an in-process task's - remote-run-ux design.md D1.
	taskOutput := jobOutput + boundAppendChunk(jobOutput, output, maxOutputBytes)
	results = append(results, TaskResult{Name: taskName, Status: status, Output: taskOutput, Error: taskError})
	newStatus, errMsg := computeRunStatus(results)

	chunk := output
	if chunk != "" && runOutput != "" && !strings.HasSuffix(runOutput, "\n") {
		chunk = "\n" + chunk
	}
	out := runOutput + boundAppendChunk(runOutput, chunk, maxOutputBytes)
	resultsJSON, _ := json.Marshal(results)
	finished := newStatus == "passed" || newStatus == "failed" || newStatus == "canceled"
	if finished {
		if _, err := tx.Exec(
			`UPDATE runs SET status = $1, task_results = $2, output = $3, error = $4, finished_at = now() WHERE id = $5`,
			newStatus, string(resultsJSON), out, errMsg, runID,
		); err != nil {
			return false, fmt.Errorf("finish run from job completion: %w", err)
		}
		if err := s.pruneRuns(tx); err != nil {
			return false, err
		}
	} else {
		if _, err := tx.Exec(
			`UPDATE runs SET status = $1, task_results = $2, output = $3, error = $4 WHERE id = $5`,
			newStatus, string(resultsJSON), out, errMsg, runID,
		); err != nil {
			return false, fmt.Errorf("update run from job completion: %w", err)
		}
	}

	if _, err := tx.Exec(`UPDATE remote_jobs SET status = $1, updated_at = now() WHERE id = $2`, status, jobID); err != nil {
		return false, fmt.Errorf("finalize completed job: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit complete job: %w", err)
	}
	return true, nil
}

// PendingOrClaimedJobIDsForRun returns the IDs of runID's non-terminal jobs.
func (s *PGStore) PendingOrClaimedJobIDsForRun(runID int) ([]int, error) {
	rows, err := s.db.Query(
		`SELECT id FROM remote_jobs WHERE run_id = $1 AND status IN ($2, $3, $4) ORDER BY id`,
		runID, domain.RemoteJobStatusPending, domain.RemoteJobStatusClaimed, domain.RemoteJobStatusRunning,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// CancelJobsForRun marks runID's still-"pending" jobs "canceled" outright.
// A "claimed"/"running" job is left alone - no stop signal exists (D8) - and
// relies on CompleteJob's terminal-run fencing once its runner eventually
// reports back.
func (s *PGStore) CancelJobsForRun(runID int) error {
	_, err := s.db.Exec(
		`UPDATE remote_jobs SET status = $1, updated_at = now() WHERE run_id = $2 AND status = $3`,
		domain.RemoteJobStatusCanceled, runID, domain.RemoteJobStatusPending,
	)
	return err
}

// ReclaimStaleJobs is silent-runner recovery (see domain.RemoteJobStore's
// doc). It selects every job last updated before olderThan that is either
// "claimed"/"running" (its runner went silent) or "pending" after an
// earlier reclaim (nobody picked it up again). Each counts as one
// unanswered attempt: a job whose reclaim_count already reached
// maxReclaims, or whose run is terminal, is finalized "failed" naming
// reason through CompleteJob, inheriting its run-terminal fencing;
// otherwise it goes (or stays) "pending" with reclaim_count+1. Only a claim
// or reclaim bumps claim_epoch (D5), so a slow original runner is fenced
// out; reclaim_count, not claim_epoch, is what the limit is checked
// against, since claim_epoch also moves on every claim
// (remote-runner-fixes design.md D3). A never-claimed pending job has
// reclaim_count 0 and is never selected. Selection uses no row lock: each
// per-job action is itself race-safe (CompleteJob's epoch fencing, or an
// epoch-guarded optimistic UPDATE), so a job a runner claims or completes
// concurrently is simply not counted here rather than double-processed.
func (s *PGStore) ReclaimStaleJobs(olderThan time.Time, maxReclaims int, reason string) (int, int, error) {
	rows, err := s.db.Query(
		`SELECT rj.id, rj.run_id, rj.task_name, rj.status, rj.claim_epoch, rj.reclaim_count, r.status
		 FROM remote_jobs rj JOIN runs r ON r.id = rj.run_id
		 WHERE rj.updated_at < $1 AND (rj.status IN ($2, $3) OR (rj.status = $4 AND rj.reclaim_count > 0))`,
		olderThan, domain.RemoteJobStatusClaimed, domain.RemoteJobStatusRunning, domain.RemoteJobStatusPending,
	)
	if err != nil {
		return 0, 0, fmt.Errorf("select stale jobs: %w", err)
	}
	type staleJob struct {
		id, runID                int
		taskName, status         string
		claimEpoch, reclaimCount int
		runStatus                string
	}
	var stale []staleJob
	for rows.Next() {
		var j staleJob
		if err := rows.Scan(&j.id, &j.runID, &j.taskName, &j.status, &j.claimEpoch, &j.reclaimCount, &j.runStatus); err != nil {
			rows.Close()
			return 0, 0, fmt.Errorf("scan stale job: %w", err)
		}
		stale = append(stale, j)
	}
	if err := rows.Err(); err != nil {
		return 0, 0, err
	}
	rows.Close()

	reclaimed, failed := 0, 0
	for _, j := range stale {
		if isTerminalRunStatus(j.runStatus) || j.reclaimCount >= maxReclaims {
			// reason is a short, fixed system string, never runner-controlled
			// output, so it needs no output-budget bound here.
			if _, err := s.CompleteJob(j.id, j.claimEpoch, domain.RemoteJobStatusFailed, reason, reason, nil, 0); err != nil {
				return reclaimed, failed, fmt.Errorf("fail stale job %d: %w", j.id, err)
			}
			// CompleteJob finalizes the job row to "failed" whenever the
			// epoch still matched, whether or not its run was also touched
			// (ok distinguishes only the latter) - re-read to count
			// accurately without duplicating that check here.
			if after, err := s.GetRemoteJob(j.id); err == nil && after != nil && after.Status == domain.RemoteJobStatusFailed {
				failed++
			}
			continue
		}
		// A silent claim gets a new epoch so its runner is fenced out; an
		// unanswered pending job has no claim to fence and keeps its epoch.
		epochBump := 1
		if j.status == domain.RemoteJobStatusPending {
			epochBump = 0
		}
		res, err := s.db.Exec(
			`UPDATE remote_jobs SET status = $1, runner_id = NULL, claim_epoch = claim_epoch + $2, reclaim_count = reclaim_count + 1, claimed_at = NULL, updated_at = now()
			 WHERE id = $3 AND claim_epoch = $4 AND status = $5`,
			domain.RemoteJobStatusPending, epochBump, j.id, j.claimEpoch, j.status,
		)
		if err != nil {
			return reclaimed, failed, fmt.Errorf("reclaim stale job %d: %w", j.id, err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			continue
		}
		reclaimed++
		if j.status != domain.RemoteJobStatusPending {
			// Undo the claim's "running" (design.md D5) so the task reads
			// pending again while it waits for another runner.
			if err := s.AppendTaskResult(j.runID, TaskResult{Name: j.taskName, Status: "pending"}); err != nil {
				return reclaimed, failed, fmt.Errorf("record reclaimed task pending: %w", err)
			}
		}
	}

	return reclaimed, failed, nil
}

// OutstandingJobRunIDsForProject returns the distinct IDs of projectName's
// runs that have a job in pending/claimed/running status.
func (s *PGStore) OutstandingJobRunIDsForProject(projectName string) ([]int, error) {
	rows, err := s.db.Query(
		`SELECT DISTINCT rj.run_id FROM remote_jobs rj JOIN runs r ON r.id = rj.run_id
		 WHERE r.project_name = $1 AND rj.status IN ($2, $3, $4) ORDER BY rj.run_id`,
		projectName, domain.RemoteJobStatusPending, domain.RemoteJobStatusClaimed, domain.RemoteJobStatusRunning,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// DeleteTerminalRemoteSecrets deletes the encrypted values of every
// terminal job and run (design.md D2). A late completion of a canceled run
// still redacts: its job is not terminal until that completion lands.
func (s *PGStore) DeleteTerminalRemoteSecrets() error {
	if _, err := s.db.Exec(
		`UPDATE remote_jobs SET secrets_enc = NULL WHERE secrets_enc IS NOT NULL AND status IN ($1, $2, $3)`,
		domain.RemoteJobStatusPassed, domain.RemoteJobStatusFailed, domain.RemoteJobStatusCanceled,
	); err != nil {
		return fmt.Errorf("delete terminal job secrets: %w", err)
	}
	if _, err := s.db.Exec(
		`UPDATE runs SET remote_secrets_enc = NULL WHERE remote_secrets_enc IS NOT NULL AND status IN ('passed', 'failed', 'canceled')`,
	); err != nil {
		return fmt.Errorf("delete terminal run secrets: %w", err)
	}
	return nil
}

// remoteRunSnapshot is the JSON shape stored in runs.remote_snapshot: only
// the archive. The per-task variables and the run's secret values live
// encrypted in runs.remote_secrets_enc (design.md D2).
type remoteRunSnapshot struct {
	Archive []byte `json:"archive"`
}

// SetRemoteRunSnapshot persists the one archive, per-task variables, and
// resolved secret values a remote-mode run's initial dispatch computed,
// for tasks.md 3.6's resumption walker to reuse later without re-acquiring
// source or re-resolving secrets.
func (s *PGStore) SetRemoteRunSnapshot(runID int, archive []byte, variables map[string]map[string]string, secretValues []string) error {
	enc, err := s.sealRemoteSecrets(remoteRunAAD(runID), remoteSecrets{TaskVariables: variables, SecretValues: secretValues})
	if err != nil {
		return fmt.Errorf("encrypt remote run secrets: %w", err)
	}
	b, err := json.Marshal(remoteRunSnapshot{Archive: archive})
	if err != nil {
		return fmt.Errorf("marshal remote run snapshot: %w", err)
	}
	_, err = s.db.Exec(`UPDATE runs SET remote_snapshot = $1, remote_secrets_enc = $2 WHERE id = $3`, string(b), enc, runID)
	return err
}

// GetRemoteRunSnapshot returns what SetRemoteRunSnapshot stored, or a nil
// archive and empty values if none was ever set.
func (s *PGStore) GetRemoteRunSnapshot(runID int) ([]byte, map[string]map[string]string, []string, error) {
	var snapJSON sql.NullString
	var enc []byte
	err := s.db.QueryRow(`SELECT remote_snapshot, remote_secrets_enc FROM runs WHERE id = $1`, runID).Scan(&snapJSON, &enc)
	if err == sql.ErrNoRows {
		return nil, nil, nil, nil
	}
	if err != nil {
		return nil, nil, nil, err
	}
	if !snapJSON.Valid {
		return nil, nil, nil, nil
	}
	var snap remoteRunSnapshot
	if err := json.Unmarshal([]byte(snapJSON.String), &snap); err != nil {
		return nil, nil, nil, fmt.Errorf("unmarshal remote run snapshot: %w", err)
	}
	secrets, err := s.openRemoteSecrets(remoteRunAAD(runID), enc)
	if err != nil {
		return nil, nil, nil, err
	}
	return snap.Archive, secrets.TaskVariables, secrets.SecretValues, nil
}

var _ domain.RemoteJobStore = (*PGStore)(nil)
var _ domain.RunnerStore = (*PGStore)(nil)

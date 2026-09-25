package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

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

// CreateRemoteJob dispatches one task of runID as a pending job.
func (s *PGStore) CreateRemoteJob(runID int, taskName string, payload domain.RemoteJobPayload) (int, error) {
	b, err := json.Marshal(payload)
	if err != nil {
		return 0, fmt.Errorf("marshal remote job payload: %w", err)
	}
	var id int
	err = s.db.QueryRow(
		`INSERT INTO remote_jobs (run_id, task_name, status, payload) VALUES ($1, $2, $3, $4) RETURNING id`,
		runID, taskName, domain.RemoteJobStatusPending, string(b),
	).Scan(&id)
	if err != nil {
		return 0, err
	}
	return id, nil
}

// scanRemoteJob scans one remote_jobs row into a domain.RemoteJob.
func scanRemoteJob(row interface {
	Scan(dest ...any) error
}) (*domain.RemoteJob, error) {
	var j domain.RemoteJob
	var payloadJSON string
	var runnerID sql.NullInt64
	var claimedAt sql.NullTime
	if err := row.Scan(&j.ID, &j.RunID, &j.TaskName, &j.Status, &runnerID, &payloadJSON, &j.ClaimEpoch, &claimedAt, &j.UpdatedAt); err != nil {
		return nil, err
	}
	if runnerID.Valid {
		id := int(runnerID.Int64)
		j.RunnerID = &id
	}
	if claimedAt.Valid {
		j.ClaimedAt = &claimedAt.Time
	}
	if err := json.Unmarshal([]byte(payloadJSON), &j.Payload); err != nil {
		return nil, fmt.Errorf("unmarshal remote job payload: %w", err)
	}
	return &j, nil
}

const remoteJobColumns = `id, run_id, task_name, status, runner_id, payload, claim_epoch, claimed_at, updated_at`

// GetRemoteJob returns a job by ID, or nil if it does not exist.
func (s *PGStore) GetRemoteJob(jobID int) (*domain.RemoteJob, error) {
	row := s.db.QueryRow(`SELECT `+remoteJobColumns+` FROM remote_jobs WHERE id = $1`, jobID)
	j, err := scanRemoteJob(row)
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
		 ORDER BY rj.id FOR UPDATE OF rj SKIP LOCKED LIMIT 1`,
		domain.RemoteJobStatusPending,
	).Scan(&jobID)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("select next pending job: %w", err)
	}

	row := tx.QueryRow(
		`UPDATE remote_jobs SET status = $1, runner_id = $2, claim_epoch = claim_epoch + 1, claimed_at = now(), updated_at = now()
		 WHERE id = $3 RETURNING `+remoteJobColumns,
		domain.RemoteJobStatusClaimed, runnerID, jobID,
	)
	j, err := scanRemoteJob(row)
	if err != nil {
		return nil, fmt.Errorf("claim job %d: %w", jobID, err)
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
// "claimed" job advances it to "running", informationally.
func (s *PGStore) AppendJobTrace(jobID, epoch, offset int, chunk string, maxOutputBytes int, callerRunnerID *int) (bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return false, fmt.Errorf("begin trace append: %w", err)
	}
	defer tx.Rollback()

	var runID, claimEpoch, traceOffset int
	var jobStatus string
	var runnerID sql.NullInt64
	err = tx.QueryRow(`SELECT run_id, claim_epoch, status, trace_offset, runner_id FROM remote_jobs WHERE id = $1 FOR UPDATE`, jobID).
		Scan(&runID, &claimEpoch, &jobStatus, &traceOffset, &runnerID)
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
	if _, err := tx.Exec(
		`UPDATE remote_jobs SET status = $1, trace_offset = $2, updated_at = now() WHERE id = $3`,
		newStatus, offset+len(chunk), jobID,
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
// configured output budget entirely.
func (s *PGStore) CompleteJob(jobID, epoch int, status, output string, callerRunnerID *int, maxOutputBytes int) (bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return false, fmt.Errorf("begin complete job: %w", err)
	}
	defer tx.Rollback()

	var runID int
	var taskName, jobStatus string
	var claimEpoch int
	var runnerID sql.NullInt64
	err = tx.QueryRow(`SELECT run_id, task_name, claim_epoch, status, runner_id FROM remote_jobs WHERE id = $1 FOR UPDATE`, jobID).
		Scan(&runID, &taskName, &claimEpoch, &jobStatus, &runnerID)
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
	results = append(results, TaskResult{Name: taskName, Status: status, Output: output})
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

// ReclaimStaleJobs returns to "pending" every "claimed"/"running" job whose
// run is not already terminal and whose updated_at is older than olderThan,
// bumping claim_epoch (D5) so a subsequent claim - by another runner, or a
// re-claim by the same one - is a distinct epoch from whatever the
// original, merely-slow runner still holds. A job already reclaimed
// maxReclaims times (tracked via claim_epoch, which increments once per
// claim including the initial one) is finalized "failed" naming reason
// instead of being returned to "pending" again, through the same
// CompleteJob path a runner's own report would use - so it inherits
// CompleteJob's run-terminal fencing for free. A stale job whose run is
// already terminal is finalized the same way. Selection uses no row lock:
// each per-job action is itself race-safe (CompleteJob's epoch fencing, or
// an epoch-guarded optimistic UPDATE for a plain reclaim), so a job a
// runner completes concurrently is simply not counted here rather than
// double-processed - see design.md D5 and tasks.md 4.2/4.3.
func (s *PGStore) ReclaimStaleJobs(olderThan time.Time, maxReclaims int, reason string) (int, int, error) {
	rows, err := s.db.Query(
		`SELECT rj.id, rj.claim_epoch, r.status FROM remote_jobs rj JOIN runs r ON r.id = rj.run_id
		 WHERE rj.status IN ($1, $2) AND rj.updated_at < $3`,
		domain.RemoteJobStatusClaimed, domain.RemoteJobStatusRunning, olderThan,
	)
	if err != nil {
		return 0, 0, fmt.Errorf("select stale jobs: %w", err)
	}
	type staleJob struct {
		id         int
		claimEpoch int
		runStatus  string
	}
	var stale []staleJob
	for rows.Next() {
		var j staleJob
		if err := rows.Scan(&j.id, &j.claimEpoch, &j.runStatus); err != nil {
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
		if isTerminalRunStatus(j.runStatus) || j.claimEpoch >= maxReclaims {
			// reason is a short, fixed system string, never runner-controlled
			// output, so it needs no output-budget bound here.
			if _, err := s.CompleteJob(j.id, j.claimEpoch, domain.RemoteJobStatusFailed, reason, nil, 0); err != nil {
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
		res, err := s.db.Exec(
			`UPDATE remote_jobs SET status = $1, runner_id = NULL, claim_epoch = claim_epoch + 1, claimed_at = NULL, updated_at = now()
			 WHERE id = $2 AND claim_epoch = $3 AND status IN ($4, $5)`,
			domain.RemoteJobStatusPending, j.id, j.claimEpoch, domain.RemoteJobStatusClaimed, domain.RemoteJobStatusRunning,
		)
		if err != nil {
			return reclaimed, failed, fmt.Errorf("reclaim stale job %d: %w", j.id, err)
		}
		if n, _ := res.RowsAffected(); n > 0 {
			reclaimed++
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

// remoteRunSnapshot is the JSON shape stored in runs.remote_snapshot.
type remoteRunSnapshot struct {
	Archive   []byte                       `json:"archive"`
	Variables map[string]map[string]string `json:"variables"`
}

// SetRemoteRunSnapshot persists the one archive and per-task variables a
// remote-mode run's initial dispatch computed, for tasks.md 3.6's
// resumption walker to reuse later without re-acquiring source.
func (s *PGStore) SetRemoteRunSnapshot(runID int, archive []byte, variables map[string]map[string]string) error {
	b, err := json.Marshal(remoteRunSnapshot{Archive: archive, Variables: variables})
	if err != nil {
		return fmt.Errorf("marshal remote run snapshot: %w", err)
	}
	_, err = s.db.Exec(`UPDATE runs SET remote_snapshot = $1 WHERE id = $2`, string(b), runID)
	return err
}

// GetRemoteRunSnapshot returns what SetRemoteRunSnapshot stored, or a nil
// archive and empty map if none was ever set.
func (s *PGStore) GetRemoteRunSnapshot(runID int) ([]byte, map[string]map[string]string, error) {
	var snapJSON sql.NullString
	err := s.db.QueryRow(`SELECT remote_snapshot FROM runs WHERE id = $1`, runID).Scan(&snapJSON)
	if err == sql.ErrNoRows {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	if !snapJSON.Valid {
		return nil, nil, nil
	}
	var snap remoteRunSnapshot
	if err := json.Unmarshal([]byte(snapJSON.String), &snap); err != nil {
		return nil, nil, fmt.Errorf("unmarshal remote run snapshot: %w", err)
	}
	return snap.Archive, snap.Variables, nil
}

var _ domain.RemoteJobStore = (*PGStore)(nil)
var _ domain.RunnerStore = (*PGStore)(nil)

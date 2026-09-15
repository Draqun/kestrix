package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"kestrix/internal/domain"
	"kestrix/internal/model"
)

func (s *PGStore) CreateRun(projectName string, changed []string, includeManual bool, snap *model.Project, startedBy string) (int, error) {
	changedJSON := "[]"
	if len(changed) > 0 {
		b, _ := json.Marshal(changed)
		changedJSON = string(b)
	}
	var snapJSON *string
	if snap != nil {
		b, _ := json.Marshal(snap)
		s := string(b)
		snapJSON = &s
	}
	var id int
	err := s.db.QueryRow(
		`INSERT INTO runs (project_name, changed, include_manual, pipeline_def, started_by) VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		projectName, changedJSON, includeManual, snapJSON, startedBy,
	).Scan(&id)
	return id, err
}

// DefaultRunHistoryLimit is the number of terminal runs retained when no
// limit is configured. It mirrors config.DefaultConfig's runner.run_history_limit;
// the store keeps its own copy so it stays usable without the config package.
const DefaultRunHistoryLimit = 1000

// FinishRun updates a run with its result and, once the write lands a
// terminal status, prunes history to the configured retention limit in the
// same transaction - see PruneRuns and design.md D3. A run already recorded
// as canceled keeps that status: a late non-canceled write (for example a
// single-task handler racing Cancel, or a panic recovery landing after
// CancelRun) must not overwrite the operator's cancel. Canceled writes stay
// allowed so recording a cancellation itself is idempotent.
func (s *PGStore) FinishRun(id int, status, output, errMsg string, taskResults []TaskResult) error {
	taskResultsJSON := "[]"
	if len(taskResults) > 0 {
		b, _ := json.Marshal(taskResults)
		taskResultsJSON = string(b)
	}
	finished := status == "passed" || status == "failed" || status == "canceled"

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin finish run: %w", err)
	}
	defer tx.Rollback()

	if finished {
		_, err = tx.Exec(
			`UPDATE runs SET status = $1, output = $2, error = $3, task_results = $4, finished_at = now() WHERE id = $5 AND (status <> 'canceled' OR $1 = 'canceled')`,
			status, output, errMsg, taskResultsJSON, id,
		)
	} else {
		_, err = tx.Exec(
			`UPDATE runs SET status = $1, output = $2, error = $3, task_results = $4, finished_at = NULL WHERE id = $5 AND (status <> 'canceled' OR $1 = 'canceled')`,
			status, output, errMsg, taskResultsJSON, id,
		)
	}
	if err != nil {
		return err
	}

	if finished {
		if err := s.pruneRuns(tx); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// PruneRuns deletes the oldest terminal (passed/failed/canceled) runs beyond
// the configured retention limit, never touching a running, queued, or
// manual-pending run - see design.md D3. It runs in its own transaction; call
// it directly only outside of FinishRun's own transactional call.
func (s *PGStore) PruneRuns() error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin prune runs: %w", err)
	}
	defer tx.Rollback()
	if err := s.pruneRuns(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// pruneRuns is PruneRuns' body, shared with FinishRun so both run the
// deletion inside whichever transaction the caller already holds.
func (s *PGStore) pruneRuns(tx *sql.Tx) error {
	limit := s.runHistoryLimit
	if limit <= 0 {
		limit = DefaultRunHistoryLimit
	}
	_, err := tx.Exec(
		`DELETE FROM runs
		 WHERE status IN ('passed', 'failed', 'canceled')
		 AND id NOT IN (
		   SELECT id FROM runs WHERE status IN ('passed', 'failed', 'canceled')
		   ORDER BY started_at DESC, id DESC LIMIT $1
		 )`,
		limit,
	)
	if err != nil {
		return fmt.Errorf("prune run history: %w", err)
	}
	return nil
}

// SetRunQueued marks a non-terminal run "queued" while it waits for a
// concurrency slot; see design.md D1/D2. A run already terminal is left
// untouched.
func (s *PGStore) SetRunQueued(id int) error {
	_, err := s.db.Exec(
		`UPDATE runs SET status = 'queued' WHERE id = $1 AND status NOT IN ('passed', 'failed', 'canceled')`,
		id,
	)
	return err
}

// StartQueuedRun transitions a "queued" run to "running". ok is false when
// the row was not "queued" (for example, already canceled), telling the
// caller not to execute it.
func (s *PGStore) StartQueuedRun(id int) (bool, error) {
	res, err := s.db.Exec(`UPDATE runs SET status = 'running' WHERE id = $1 AND status = 'queued'`, id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// AppendTaskResult atomically adds a single task result to the run's task_results array.
func (s *PGStore) AppendTaskResult(runID int, tr TaskResult) error {
	b, _ := json.Marshal(tr)
	_, err := s.db.Exec(
		`UPDATE runs SET task_results = task_results::jsonb || $1::jsonb WHERE id = $2`,
		string(b), runID,
	)
	return err
}

// UpdateRunSources stores only non-secret source declarations and resolved
// revisions. Credential values are never part of AcquiredSource.
func (s *PGStore) UpdateRunSources(runID int, sources []domain.AcquiredSource) error {
	b, _ := json.Marshal(sources)
	_, err := s.db.Exec(`UPDATE runs SET sources = $1 WHERE id = $2`, string(b), runID)
	return err
}

// FlushRunOutput overwrites the run's aggregate output (called periodically during execution).
func (s *PGStore) FlushRunOutput(runID int, output string) error {
	_, err := s.db.Exec(`UPDATE runs SET output = $1 WHERE id = $2`, output, runID)
	return err
}

// dedupLatestTaskResults reduces an append-only task_results history to one
// entry per task name - the latest - preserving the order each name was
// first seen in. Shared by CancelRun and FailInterruptedRuns, the two
// writers that need "current status per task" rather than the raw history.
func dedupLatestTaskResults(results []TaskResult) (order []string, latest map[string]TaskResult) {
	latest = make(map[string]TaskResult)
	order = make([]string, 0, len(results))
	seen := make(map[string]bool)
	for _, tr := range results {
		if !seen[tr.Name] {
			order = append(order, tr.Name)
			seen[tr.Name] = true
		}
		latest[tr.Name] = tr
	}
	return order, latest
}

// CancelRun marks a running or queued run as "canceled":
// - running or queued → canceled
// - pending (no incomplete deps) → canceled
// - pending (depends on incomplete/failed) → skipped
// - passed left unchanged
func (s *PGStore) CancelRun(id int, canceledBy string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin cancel run: %w", err)
	}
	defer tx.Rollback()

	var taskResultsJSON string
	var status string
	err = tx.QueryRow(
		`SELECT status, task_results FROM runs WHERE id = $1`, id,
	).Scan(&status, &taskResultsJSON)
	if err != nil {
		return err
	}
	if status != "running" && status != "queued" {
		return nil
	}

	var results []TaskResult
	json.Unmarshal([]byte(taskResultsJSON), &results)

	order, latest := dedupLatestTaskResults(results)

	incomplete := make(map[string]bool)
	for name, tr := range latest {
		if tr.Status == "pending" || tr.Status == "running" {
			incomplete[name] = true
		}
	}

	depMap := make(map[string][]string)
	var snapJSON sql.NullString
	_ = tx.QueryRow(`SELECT pipeline_def FROM runs WHERE id = $1`, id).Scan(&snapJSON)
	if snapJSON.Valid {
		var snap model.Project
		if json.Unmarshal([]byte(snapJSON.String), &snap) == nil {
			for _, t := range snap.Tasks {
				depMap[t.Name] = t.DependsOn
			}
		}
	}

	var final []TaskResult
	for _, name := range order {
		tr := latest[name]
		switch tr.Status {
		case "running":
			tr.Status = "canceled"
			tr.Error = "canceled"
		case "pending":
			blocked := false
			for _, dep := range depMap[name] {
				if incomplete[dep] {
					blocked = true
					break
				}
				if d, ok := latest[dep]; ok && (d.Status == "failed" || d.Status == "canceled" || d.Status == "skipped") {
					blocked = true
					break
				}
			}
			if blocked {
				tr.Status = "skipped"
				tr.Error = "dependency canceled"
			} else {
				tr.Status = "canceled"
				tr.Error = "canceled"
			}
		}
		final = append(final, tr)
	}

	b, _ := json.Marshal(final)
	if _, err := tx.Exec(
		`UPDATE runs SET status = 'canceled', task_results = $1, error = 'canceled', finished_at = now(), canceled_by = $2 WHERE id = $3`,
		string(b), canceledBy, id,
	); err != nil {
		return err
	}

	// A canceled run is terminal, so it must count against run_history_limit
	// the same as a passed/failed run finishing via FinishRun - see
	// REVIEW.md R3.
	if err := s.pruneRuns(tx); err != nil {
		return err
	}

	return tx.Commit()
}

// FailInterruptedRuns marks every run left "running" or "queued" by a
// previous process as failed, in one transaction, before any request can
// observe the old state. A "queued" run never persisted its place in an
// in-memory queue across a restart, so it is reconciled exactly like an
// interrupted "running" run - see design.md Migration Plan. A run is left
// alone only when it has at least one "manual" task result and nothing else
// non-terminal: ApproveManualTask still works on it after the restart. Every
// other case - a running or pending task, or no task result at all (a crash
// before the first one was appended, or a zero-task project) - is
// reconciled. Idempotent: a run already reconciled has no "running" or
// "queued" status left to select.
func (s *PGStore) FailInterruptedRuns(reason string) (int, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()

	rows, err := tx.Query(`SELECT id, task_results, output FROM runs WHERE status IN ('running', 'queued') FOR UPDATE`)
	if err != nil {
		return 0, fmt.Errorf("select running runs: %w", err)
	}
	type runRow struct {
		id              int
		taskResultsJSON string
		output          string
	}
	var runRows []runRow
	for rows.Next() {
		var r runRow
		if err := rows.Scan(&r.id, &r.taskResultsJSON, &r.output); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scan running run: %w", err)
		}
		runRows = append(runRows, r)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("iterate running runs: %w", err)
	}
	rows.Close()

	count := 0
	for _, r := range runRows {
		var results []TaskResult
		if err := json.Unmarshal([]byte(r.taskResultsJSON), &results); err != nil {
			log.Printf("FailInterruptedRuns: run %d has malformed task_results, proceeding with a partial decode: %v", r.id, err)
		}

		order, latest := dedupLatestTaskResults(results)

		// Leave the run running only when it has an actual manual gate
		// waiting and nothing else non-terminal - that run is genuinely
		// resumable via ApproveManualTask. Empty task_results (a crash
		// between CreateRun and its first AppendTaskResult, or a
		// zero-task project) has no manual entry to wait on, so it must
		// not fall into this branch: it needs to be reconciled below.
		hasManual := false
		hasRunningOrPending := false
		for _, tr := range latest {
			switch tr.Status {
			case "manual":
				hasManual = true
			case "running", "pending":
				hasRunningOrPending = true
			}
		}
		if hasManual && !hasRunningOrPending {
			continue
		}

		final := make([]TaskResult, 0, len(order))
		for _, name := range order {
			tr := latest[name]
			switch tr.Status {
			case "running":
				tr.Status = "failed"
				tr.Error = reason
			case "pending":
				tr.Status = "skipped"
				tr.Error = reason
			}
			final = append(final, tr)
		}

		out := r.output
		if out != "" && !strings.HasSuffix(out, "\n") {
			out += "\n"
		}
		out += reason

		b, _ := json.Marshal(final)
		if _, err := tx.Exec(
			`UPDATE runs SET status = 'failed', task_results = $1, output = $2, error = $3, finished_at = now() WHERE id = $4`,
			string(b), out, reason, r.id,
		); err != nil {
			return 0, fmt.Errorf("fail interrupted run %d: %w", r.id, err)
		}
		count++
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit: %w", err)
	}
	return count, nil
}

// ApplyTaskResult appends a task result and recomputes overall run status.
func (s *PGStore) ApplyTaskResult(id int, tr TaskResult, outputExtra string) error {
	run, err := s.GetRun(id)
	if err != nil || run == nil {
		return fmt.Errorf("run not found")
	}
	results := append(run.TaskResults, tr)
	// Latest status per task name.
	latest := make(map[string]TaskResult)
	for _, r := range results {
		latest[r.Name] = r
	}
	status := "passed"
	errMsg := ""
	canExecute := false
	failed := false
	canceled := false
	for _, result := range latest {
		switch result.Status {
		case "manual", "pending", "running":
			canExecute = true
		case "failed":
			failed = true
			if errMsg == "" {
				errMsg = result.Error
			}
		case "canceled":
			// A task terminated by our own cancellation is not a pipeline
			// failure; the run is canceled rather than failed, mirroring
			// runStatus in internal/service.
			canceled = true
		}
	}
	if canExecute {
		status = "running"
	} else if canceled {
		status = "canceled"
		errMsg = "canceled"
	} else if failed {
		status = "failed"
	}
	if failed && errMsg == "" {
		errMsg = "task failed"
	}
	out := run.Output
	if outputExtra != "" {
		if out != "" && !strings.HasSuffix(out, "\n") {
			out += "\n"
		}
		out += outputExtra
	}
	b, _ := json.Marshal(results)
	finished := status == "passed" || status == "failed" || status == "canceled"
	if finished {
		_, err = s.db.Exec(
			`UPDATE runs SET status = $1, task_results = $2, output = $3, error = $4, finished_at = COALESCE(finished_at, now()) WHERE id = $5`,
			status, string(b), out, errMsg, id,
		)
	} else {
		_, err = s.db.Exec(
			`UPDATE runs SET status = $1, task_results = $2, output = $3, error = $4, finished_at = NULL WHERE id = $5`,
			status, string(b), out, errMsg, id,
		)
	}
	return err
}

// GetRun returns a run by ID.
func (s *PGStore) GetRun(id int) (*Run, error) {
	var r Run
	var changedJSON, taskResultsJSON, sourcesJSON string
	var pipelineDefJSON sql.NullString
	var finishedAt sql.NullTime
	err := s.db.QueryRow(
		`SELECT id, project_name, status, changed, include_manual, pipeline_def, output, error, task_results, sources, started_at, finished_at, started_by, canceled_by FROM runs WHERE id = $1`, id,
	).Scan(&r.ID, &r.ProjectName, &r.Status, &changedJSON, &r.IncludeManual, &pipelineDefJSON, &r.Output, &r.Error, &taskResultsJSON, &sourcesJSON, &r.StartedAt, &finishedAt, &r.StartedBy, &r.CanceledBy)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	json.Unmarshal([]byte(changedJSON), &r.Changed)
	json.Unmarshal([]byte(taskResultsJSON), &r.TaskResults)
	json.Unmarshal([]byte(sourcesJSON), &r.Sources)
	if pipelineDefJSON.Valid {
		var snap model.Project
		if err := json.Unmarshal([]byte(pipelineDefJSON.String), &snap); err == nil {
			r.PipelineDef = &snap
		}
	}
	if finishedAt.Valid {
		r.FinishedAt = &finishedAt.Time
	}
	return &r, nil
}

// CountRuns returns the total number of runs for a project.
func (s *PGStore) CountRuns(projectName string) (int, error) {
	var count int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM runs WHERE project_name = $1`,
		projectName,
	).Scan(&count)
	return count, err
}

// ListRuns returns runs for a project, most recent first, with pagination.
func (s *PGStore) ListRuns(projectName string, limit int, offset int) ([]Run, error) {
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := s.db.Query(
		`SELECT id, project_name, status, changed, include_manual, pipeline_def, output, error, task_results, sources, started_at, finished_at, started_by, canceled_by FROM runs WHERE project_name = $1 ORDER BY started_at DESC LIMIT $2 OFFSET $3`,
		projectName, limit, offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var runs []Run
	for rows.Next() {
		var r Run
		var changedJSON, taskResultsJSON, sourcesJSON string
		var pipelineDefJSON sql.NullString
		var finishedAt sql.NullTime
		if err := rows.Scan(&r.ID, &r.ProjectName, &r.Status, &changedJSON, &r.IncludeManual, &pipelineDefJSON, &r.Output, &r.Error, &taskResultsJSON, &sourcesJSON, &r.StartedAt, &finishedAt, &r.StartedBy, &r.CanceledBy); err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(changedJSON), &r.Changed)
		json.Unmarshal([]byte(taskResultsJSON), &r.TaskResults)
		json.Unmarshal([]byte(sourcesJSON), &r.Sources)
		if pipelineDefJSON.Valid {
			var snap model.Project
			if err := json.Unmarshal([]byte(pipelineDefJSON.String), &snap); err == nil {
				r.PipelineDef = &snap
			}
		}
		if finishedAt.Valid {
			r.FinishedAt = &finishedAt.Time
		}
		runs = append(runs, r)
	}
	return runs, rows.Err()
}

// ListAllRuns returns all runs across projects, most recent first.
func (s *PGStore) ListAllRuns(limit int) ([]Run, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.Query(
		`SELECT id, project_name, status, changed, include_manual, pipeline_def, output, error, task_results, sources, started_at, finished_at, started_by, canceled_by FROM runs ORDER BY started_at DESC LIMIT $1`,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var runs []Run
	for rows.Next() {
		var r Run
		var changedJSON, taskResultsJSON, sourcesJSON string
		var pipelineDefJSON sql.NullString
		var finishedAt sql.NullTime
		if err := rows.Scan(&r.ID, &r.ProjectName, &r.Status, &changedJSON, &r.IncludeManual, &pipelineDefJSON, &r.Output, &r.Error, &taskResultsJSON, &sourcesJSON, &r.StartedAt, &finishedAt, &r.StartedBy, &r.CanceledBy); err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(changedJSON), &r.Changed)
		json.Unmarshal([]byte(taskResultsJSON), &r.TaskResults)
		json.Unmarshal([]byte(sourcesJSON), &r.Sources)
		if pipelineDefJSON.Valid {
			var snap model.Project
			if err := json.Unmarshal([]byte(pipelineDefJSON.String), &snap); err == nil {
				r.PipelineDef = &snap
			}
		}
		if finishedAt.Valid {
			r.FinishedAt = &finishedAt.Time
		}
		runs = append(runs, r)
	}
	return runs, rows.Err()
}

// CountRunsByStatus returns a map of status -> count across all projects.
func (s *PGStore) CountRunsByStatus() (map[string]int, error) {
	rows, err := s.db.Query(`SELECT status, COUNT(*) as cnt FROM runs GROUP BY status`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := map[string]int{}
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return nil, err
		}
		result[status] = count
	}
	return result, rows.Err()
}

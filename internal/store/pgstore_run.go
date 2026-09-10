package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
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

// FinishRun updates a run with its result. A run already recorded as
// canceled keeps that status: a late non-canceled write (for example a
// single-task handler racing Cancel, or a panic recovery landing after
// CancelRun) must not overwrite the operator's cancel. Canceled writes
// stay allowed so recording a cancellation itself is idempotent.
func (s *PGStore) FinishRun(id int, status, output, errMsg string, taskResults []TaskResult) error {
	taskResultsJSON := "[]"
	if len(taskResults) > 0 {
		b, _ := json.Marshal(taskResults)
		taskResultsJSON = string(b)
	}
	finished := status == "passed" || status == "failed" || status == "canceled"
	var err error
	if finished {
		_, err = s.db.Exec(
			`UPDATE runs SET status = $1, output = $2, error = $3, task_results = $4, finished_at = now() WHERE id = $5 AND (status <> 'canceled' OR $1 = 'canceled')`,
			status, output, errMsg, taskResultsJSON, id,
		)
	} else {
		_, err = s.db.Exec(
			`UPDATE runs SET status = $1, output = $2, error = $3, task_results = $4, finished_at = NULL WHERE id = $5 AND (status <> 'canceled' OR $1 = 'canceled')`,
			status, output, errMsg, taskResultsJSON, id,
		)
	}
	return err
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

// CancelRun marks a running run as "canceled":
// - running → canceled
// - pending (no incomplete deps) → canceled
// - pending (depends on incomplete/failed) → skipped
// - passed left unchanged
func (s *PGStore) CancelRun(id int, canceledBy string) error {
	var taskResultsJSON string
	var status string
	err := s.db.QueryRow(
		`SELECT status, task_results FROM runs WHERE id = $1`, id,
	).Scan(&status, &taskResultsJSON)
	if err != nil {
		return err
	}
	if status != "running" {
		return nil
	}

	var results []TaskResult
	json.Unmarshal([]byte(taskResultsJSON), &results)

	// Deduplicate by name keeping last status (append model may have running+passed pairs).
	latest := make(map[string]TaskResult)
	order := make([]string, 0)
	seen := make(map[string]bool)
	for _, tr := range results {
		if !seen[tr.Name] {
			order = append(order, tr.Name)
			seen[tr.Name] = true
		}
		latest[tr.Name] = tr
	}

	incomplete := make(map[string]bool)
	for name, tr := range latest {
		if tr.Status == "pending" || tr.Status == "running" {
			incomplete[name] = true
		}
	}

	depMap := make(map[string][]string)
	var snapJSON sql.NullString
	_ = s.db.QueryRow(`SELECT pipeline_def FROM runs WHERE id = $1`, id).Scan(&snapJSON)
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
	_, err = s.db.Exec(
		`UPDATE runs SET status = 'canceled', task_results = $1, error = 'canceled', finished_at = now(), canceled_by = $2 WHERE id = $3`,
		string(b), canceledBy, id,
	)
	return err
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
	for _, result := range latest {
		switch result.Status {
		case "manual", "pending", "running":
			canExecute = true
		case "failed":
			failed = true
			if errMsg == "" {
				errMsg = result.Error
			}
		}
	}
	if canExecute {
		status = "running"
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

package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// DefaultWebhookHistoryLimit is the number of webhook events retained when no
// limit is configured. It mirrors config.DefaultWebhookHistoryLimit; the store
// keeps its own copy so it stays usable without the config package.
const DefaultWebhookHistoryLimit = 1000

// webhookRetentionLockKey serializes the insert-and-trim pair. Under READ
// COMMITTED each transaction evaluates "newest N" against its own snapshot, so
// concurrent writers cannot see each other's insert and every one of them
// leaves an extra row behind, pushing history past the limit. The lock is
// transaction-scoped and released on commit or rollback.
const webhookRetentionLockKey = 4711001

// dispatchPayload is the JSONB shape of the dispatch column.
type dispatchPayload struct {
	PipelineID  int    `json:"pipeline_id,omitempty"`
	PipelineURL string `json:"pipeline_url,omitempty"`
}

// RecordWebhookEvent stores one processed webhook delivery and trims history
// to the configured retention limit in the same transaction, so the table
// never grows past the limit even under concurrent webhook traffic.
func (s *PGStore) RecordWebhookEvent(event WebhookEvent) error {
	receivedAt := event.ReceivedAt
	if receivedAt.IsZero() {
		receivedAt = time.Now().UTC()
	}
	tasks := event.TriggeredTasks
	if tasks == nil {
		tasks = []string{}
	}
	tasksJSON, err := json.Marshal(tasks)
	if err != nil {
		return fmt.Errorf("encode triggered tasks: %w", err)
	}
	dispatchJSON, err := json.Marshal(dispatchPayload{
		PipelineID:  event.DispatchedID,
		PipelineURL: event.DispatchedURL,
	})
	if err != nil {
		return fmt.Errorf("encode dispatch result: %w", err)
	}
	var projectName sql.NullString
	if event.ProjectName != "" {
		projectName = sql.NullString{String: event.ProjectName, Valid: true}
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin webhook event: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`SELECT pg_advisory_xact_lock($1)`, webhookRetentionLockKey); err != nil {
		return fmt.Errorf("lock webhook history: %w", err)
	}

	if _, err := tx.Exec(
		`INSERT INTO webhook_events (received_at, project_name, source_name, repo, branch, revision, commits, triggered_tasks, status, error, dispatch)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		receivedAt, projectName, event.SourceName, event.Repo, event.Branch, event.Revision,
		event.Commits, string(tasksJSON), event.Status, event.Error, string(dispatchJSON),
	); err != nil {
		return fmt.Errorf("insert webhook event: %w", err)
	}

	limit := s.webhookHistoryLimit
	if limit <= 0 {
		limit = DefaultWebhookHistoryLimit
	}
	if _, err := tx.Exec(
		`DELETE FROM webhook_events
		 WHERE id NOT IN (
		   SELECT id FROM webhook_events ORDER BY received_at DESC, id DESC LIMIT $1
		 )`,
		limit,
	); err != nil {
		return fmt.Errorf("trim webhook history: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit webhook event: %w", err)
	}
	return nil
}

// webhookFilterClauses builds the parameterized WHERE clauses for a filter.
// It never interpolates caller-supplied values into the statement text.
func webhookFilterClauses(filter WebhookEventFilter, firstArg int) (string, []interface{}) {
	var clauses []string
	var args []interface{}
	next := firstArg
	add := func(clause string, value interface{}) {
		clauses = append(clauses, clause+"$"+strconv.Itoa(next))
		args = append(args, value)
		next++
	}
	if filter.ProjectName != "" {
		add("project_name = ", filter.ProjectName)
	}
	if filter.Status != "" {
		add("status = ", filter.Status)
	}
	if filter.Since != nil {
		add("received_at >= ", *filter.Since)
	}
	if filter.Until != nil {
		add("received_at <= ", *filter.Until)
	}
	if len(clauses) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}

// ListWebhookEvents returns matching events newest first, with pagination.
func (s *PGStore) ListWebhookEvents(filter WebhookEventFilter, limit, offset int) ([]WebhookEvent, error) {
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	where, args := webhookFilterClauses(filter, 1)
	query := `SELECT id, received_at, project_name, source_name, repo, branch, revision, commits, triggered_tasks, status, error, dispatch FROM webhook_events` +
		where +
		` ORDER BY received_at DESC, id DESC LIMIT $` + strconv.Itoa(len(args)+1) +
		` OFFSET $` + strconv.Itoa(len(args)+2)
	args = append(args, limit, offset)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []WebhookEvent
	for rows.Next() {
		var e WebhookEvent
		var projectName sql.NullString
		var tasksJSON, dispatchJSON []byte
		if err := rows.Scan(&e.ID, &e.ReceivedAt, &projectName, &e.SourceName, &e.Repo, &e.Branch,
			&e.Revision, &e.Commits, &tasksJSON, &e.Status, &e.Error, &dispatchJSON); err != nil {
			return nil, err
		}
		if projectName.Valid {
			e.ProjectName = projectName.String
		}
		e.TriggeredTasks = []string{}
		if len(tasksJSON) > 0 {
			if err := json.Unmarshal(tasksJSON, &e.TriggeredTasks); err != nil {
				return nil, fmt.Errorf("decode triggered tasks: %w", err)
			}
		}
		if len(dispatchJSON) > 0 {
			var d dispatchPayload
			if err := json.Unmarshal(dispatchJSON, &d); err != nil {
				return nil, fmt.Errorf("decode dispatch result: %w", err)
			}
			e.DispatchedID = d.PipelineID
			e.DispatchedURL = d.PipelineURL
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

// CountWebhookEvents returns how many stored events match the filter.
func (s *PGStore) CountWebhookEvents(filter WebhookEventFilter) (int, error) {
	where, args := webhookFilterClauses(filter, 1)
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM webhook_events`+where, args...).Scan(&count)
	return count, err
}

// Record implements domain.WebhookLogSink.
func (s *PGStore) Record(event WebhookEvent) error {
	return s.RecordWebhookEvent(event)
}
